# Caddy Metric Injector Plugin

The **Metric Injector** plugin for [Caddy](https://caddyserver.com) provides a minimalistic way to define and increment custom Prometheus counters during HTTP request handling.

It allows you to declaratively configure one or more counters and optionally bind them to Caddy request matchers. Each matching request increments the corresponding metric.

Counters are evaluated after the remaining handler chain has executed, ensuring that metric collection does not interfere with request processing.

This plugin is intended as a complement to Caddy’s built-in metrics.
Metric Injector enables domain-specific counters tied to routing logic or matcher conditions that are not covered by the built-in metrics.

[![Go Report Card](https://goreportcard.com/badge/github.com/steffenbusch/caddy-metric-injector)](https://goreportcard.com/report/github.com/steffenbusch/caddy-metric-injector)

## Features

This plugin introduces a middleware that:

- Registers custom Prometheus `CounterVec` metrics via Caddy’s metrics registry.
- Supports optional per-counter request matchers using Caddy’s native HTTP matchers.
- Supports optional per-counter labels with dynamic values from request placeholders.
- Increments counters only when matcher conditions are satisfied.
- Treats counters without a `match` block as match-all (incremented for every request).
- Counter evaluation occurs after the remaining handler chain has completed.
- Validates configuration at provisioning time (e.g. duplicate counter name detection).

## Installation

Build Caddy with this module using `xcaddy`:

```bash
xcaddy build --with github.com/steffenbusch/caddy-metric-injector
```

## Caddyfile

Use one or more `counter` blocks inside `metric_injector`.

### Enabling Metrics

This plugin registers counters in Caddy’s metrics registry.
You must enable the global `metrics` option for Prometheus scraping:

```caddyfile
{
  metrics
}
```

### Syntax

```caddyfile
metric_injector {
    counter {
      name <prometheus-metric-name>
      help <help-text>
      label <label-name> <placeholder> [<default-value>]
      match {
         <any Caddy HTTP request matcher>
      }
    }
}
```

### Directive Order

The `metric_injector` directive does not define a default order.

You must either:

- Set a global order:

```caddyfile
{
  order metric_injector before handle
}
```

- Or use it inside an explicit `route` block to control execution order.

### Counter block fields

- `name` (required): Prometheus metric name. Must be unique within the configuration and follow Prometheus naming conventions.
- `help` (optional): Help/description string. A default description is generated if omitted.
- `label` (optional): Defines a Prometheus label. You can have multiple `label` lines.
  - `<label-name>`: The name of the label.
  - `<placeholder>`: A Caddy placeholder to resolve the label's value at runtime (e.g., `{http.request.method}`).
  - `<default-value>` (optional): A fallback value if the placeholder is empty. Defaults to `-`.
- `match` (optional): Any Caddy HTTP request matcher (path, method, header, vars, etc.). If omitted, the counter increments for every request.

### Example

```caddyfile
{
  metrics
  order metric_injector before handle
}

reporting.example.com:8080 {

  metric_injector {
    counter {
      name content_security_policy_reports_total
      help "How many CSP reports were received"
      label origin {http.request.header.origin} "unknown"
      match {
        path /csp/*
      }
    }
    counter {
      name network_error_reporting_reports_total
      help "How many NEL reports were received"
      match {
        path /nel/*
      }
    }
  }

  handle /csp/* {
    reverse_proxy localhost:9001
  }

  handle /nel/* {
    reverse_proxy localhost:9002
  }
}
```

> [!Important]
> **A Note on Cardinality**
>
> While labels are a powerful feature, it is important to use them responsibly. Each unique combination of key-value pairs for labels creates a new time series in Prometheus, which can lead to high cardinality.
>
> Avoid using labels with unbounded or high-cardinality values, such as user IDs, session IDs, or full request paths if they are not parameterized. High cardinality can significantly increase memory usage and performance overhead on your Prometheus server.
>
> It is the user's responsibility to ensure that the configured labels do not lead to an explosion of metric series.

## Behavior

- Each request is evaluated against all configured counters.
- Counters without a `match` block increment for every request.
- Counter evaluation occurs after the remaining handler chain has executed.
- Counters are incremented synchronously during request handling.
- The request and response flow are not modified by this middleware.
- Matcher evaluation errors are logged and only affect the respective counter; requests are never blocked.

## Current Limitations

- Only Prometheus `CounterVec` metrics are supported (no `Gauge`, `Histogram`, etc.).
- Counters are incremented solely based on request matchers.
- Response status codes, response headers, and response body data are not inspected.
- Within a single `metric_injector` instance, all configured counters are evaluated for each request handled by that instance.
- Counter values are process-local and reset on Caddy reload or restart.

This module intentionally focuses on simple, declarative counters.

## License

Apache License 2.0 — see the license header in source files for details.

## Acknowledgements

- [Caddy](https://caddyserver.com) for the extensible web server and module APIs.
- [Prometheus](https://prometheus.io) for the metrics ecosystem.
