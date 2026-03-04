// Copyright 2026 Steffen Busch

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

// 	http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package metricinjector

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

var metricNameRegex = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)

// MetricInjector is a Caddy HTTP middleware module that defines and
// increments custom Prometheus counters.
//
// The module allows defining one or more counters that are incremented when
// incoming requests match configured Caddy HTTP request matchers.
//
// Each counter may define optional matcher conditions. If the matchers
// evaluate to true for a request, the corresponding counter is incremented.
// Counters without matchers act as match-all counters and increment for every
// request passing through the handler.
//
// Counter evaluation occurs after the remaining handler chain has executed,
// ensuring that metric collection does not interfere with request processing.
//
// All counters are registered in Caddy's metrics registry and become available
// through the standard Prometheus metrics endpoint when the global `metrics`
// option is enabled.
type MetricInjector struct {
	// Counters is the list of metrics that should be tracked.
	Counters []*CounterMetric `json:"counters,omitempty"`

	// logger provides structured logging for the module.
	// It's initialized in the Provision method and used throughout the module for debug information.
	logger *zap.Logger
}

type CounterMetric struct {
	// Name is the Prometheus metric name. It must be unique within the
	// module configuration.
	Name string `json:"name,omitempty"`

	// Help is the help/description string for the metric. A sensible default
	// is generated if this is left empty.
	Help string `json:"help,omitempty"`

	// MatcherSetsRaw holds the raw matcher configuration parsed from Caddyfile /
	// JSON. It is exercise only during Provision; the concrete matcher sets are
	// produced from it and stored in matcherSets.
	MatcherSetsRaw caddyhttp.RawMatcherSets `json:"match,omitempty" caddy:"namespace=http.matchers"`

	// matcherSets contains the compiled matcher sets that are evaluated for each
	// request. It remains nil when no matchers were configured.
	matcherSets caddyhttp.MatcherSets

	// counter is the Prometheus Counter instance used at runtime.
	counter prometheus.Counter
}

// CaddyModule returns the Caddy module information.
func (MetricInjector) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.metric_counter",
		New: func() caddy.Module { return new(MetricInjector) },
	}
}

// Provision sets up the metrics and prepares any matchers.
//
// It initializes the logger, verifies the configuration (unique names, etc.),
// creates or reuses Prometheus counters via Caddy’s registry, and converts the
// raw matcher configuration into matcherSets for runtime use.
func (m *MetricInjector) Provision(ctx caddy.Context) error {
	m.logger = ctx.Logger(m)

	if len(m.Counters) == 0 {
		m.logger.Error("no counters configured")
		return fmt.Errorf("at least one counter must be defined")
	}

	m.logger.Info("Provisioning MetricInjector", zap.Int("configured_counters", len(m.Counters)))

	nameSet := make(map[string]struct{})

	for _, cm := range m.Counters {
		if cm.Name == "" {
			m.logger.Error("counter name missing", zap.Any("counter_def", cm))
			return errors.New("counter: name is required")
		}

		if _, exists := nameSet[cm.Name]; exists {
			m.logger.Error("duplicate counter name detected", zap.String("name", cm.Name))
			return fmt.Errorf("duplicate counter name: %s", cm.Name)
		}

		if !isValidMetricName(cm.Name) {
			m.logger.Error("invalid prometheus metric name", zap.String("name", cm.Name))
			return fmt.Errorf("invalid Prometheus metric name: %s", cm.Name)
		}

		nameSet[cm.Name] = struct{}{}

		help := cm.Help
		if help == "" {
			help = fmt.Sprintf("Counter for %s", cm.Name)
		}

		m.logger.Debug("creating prometheus counter", zap.String("name", cm.Name), zap.String("help", help))
		counter := prometheus.NewCounter(prometheus.CounterOpts{
			Name: cm.Name,
			Help: help,
		})

		if err := ctx.GetMetricsRegistry().Register(counter); err != nil {
			if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
				// reuse existing collector
				counter = are.ExistingCollector.(prometheus.Counter)
				m.logger.Info("reusing already registered counter", zap.String("name", cm.Name))
			} else {
				m.logger.Error("failed to register counter", zap.String("name", cm.Name), zap.Error(err))
				return err
			}
		} else {
			m.logger.Info("registered counter with Caddy metrics registry", zap.String("name", cm.Name))
		}

		cm.counter = counter

		if len(cm.MatcherSetsRaw) > 0 {
			m.logger.Debug("loading matcher sets for counter", zap.String("name", cm.Name))
			matcherSets, err := ctx.LoadModule(cm, "MatcherSetsRaw")
			if err != nil {
				m.logger.Error("failed to load matcher sets", zap.String("name", cm.Name), zap.Error(err))
				return err
			}
			err = cm.matcherSets.FromInterface(matcherSets)
			if err != nil {
				m.logger.Error("failed to parse matcher sets", zap.String("name", cm.Name), zap.Error(err))
				return err
			}
			m.logger.Debug("matcher sets loaded", zap.String("name", cm.Name), zap.Int("matchers", len(cm.matcherSets)))
		} else {
			m.logger.Debug("no matcher sets configured for counter", zap.String("name", cm.Name))
		}
	}

	m.logger.Info("MetricInjector provisioned", zap.Int("active_counters", len(m.Counters)))
	return nil
}

func isValidMetricName(name string) bool {
	return metricNameRegex.MatchString(name)
}

// ServeHTTP evaluates each configured counter and increments those whose
// matchers (if any) are satisfied by the current request. The next handler
// in the chain is always invoked first, so metric failures do not block the
// request.
func (m MetricInjector) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	err := next.ServeHTTP(w, r)

	for _, cm := range m.Counters {
		// If no matchers configured, treat as match-all
		var matches bool
		var matchErr error

		if len(cm.matcherSets) == 0 {
			matches = true
		} else {
			matches, matchErr = cm.matcherSets.AnyMatchWithError(r)
			if matchErr != nil {
				m.logger.Warn("matcher evaluation error", zap.String("counter", cm.Name), zap.Error(matchErr))
				continue
			}
		}

		if !matches {
			m.logger.Debug("request did not match counter's matchers", zap.String("counter", cm.Name))
			continue
		}

		if cm.counter == nil {
			m.logger.Warn("counter instance is nil, skipping increment", zap.String("counter", cm.Name))
			continue
		}

		cm.counter.Inc()
		m.logger.Debug("incremented counter", zap.String("counter", cm.Name))
	}

	return err
}

// Interface guards to ensure MetricInjector implements the necessary interfaces.
var (
	_ caddy.Module                = (*MetricInjector)(nil)
	_ caddy.Provisioner           = (*MetricInjector)(nil)
	_ caddyhttp.MiddlewareHandler = (*MetricInjector)(nil)
)
