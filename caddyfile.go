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
	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

// Initialize the module by registering it with Caddy
func init() {
	caddy.RegisterModule(MetricInjector{})
	httpcaddyfile.RegisterHandlerDirective("metric_injector", parseCaddyfile)
}

// parseCaddyfile parses the Caddyfile configuration
func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var m = new(MetricInjector)
	if err := m.UnmarshalCaddyfile(h.Dispenser); err != nil {
		return nil, err
	}
	return m, nil
}

// UnmarshalCaddyfile parses the metric_injector block from the Caddyfile
func (m *MetricInjector) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		for d.NextBlock(0) {
			switch d.Val() {
			case "counter":
				cm := new(CounterMetric)
				for d.NextBlock(1) {
					switch d.Val() {
					case "name":
						if !d.NextArg() {
							return d.ArgErr()
						}
						cm.Name = d.Val()
					case "help":
						if !d.NextArg() {
							return d.ArgErr()
						}
						cm.Help = d.Val()
					case "match":
						matcherSet, err := caddyhttp.ParseCaddyfileNestedMatcherSet(d)
						if err != nil {
							return err
						}
						cm.MatcherSetsRaw = append(cm.MatcherSetsRaw, matcherSet)
					default:
						return d.Errf("unknown directive: %s", d.Val())
					}
				}
				m.Counters = append(m.Counters, cm)
			default:
				return d.Errf("unknown directive: %s", d.Val())
			}
		}
	}
	return nil
}
