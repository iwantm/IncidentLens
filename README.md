# IncidentLens

IncidentLens is a personal Kubernetes incident recorder. It connects alerts with nearby Kubernetes observations and keeps adding matching observations while an incident is open, helping reconstruct what happened.

## Current status

The project includes a demo app, a Go recorder API, SQLite persistence, and a namespace-scoped Kubernetes watcher for Events and Deployment pod-template changes. The recorder can open and resolve incidents, backfill the five minutes of observations before an alert, and attach new matching observations while the incident is open.

## Incident lifecycle API

Submit an event to `POST /api/v1/events` to open or resolve an incident. An `alert_fired` event opens the incident using its `incident_id`, `namespace`, `service`, `timestamp`, and `summary`. IncidentLens backfills matching observations from the previous five minutes. New matching Kubernetes observations are attached while the incident remains open.

Submit an event with `type: "alert_resolved"` and the same `incident_id` to close it. Other event types are saved to the specified incident timeline.

Retrieve lifecycle state and the ordered timeline with:

```text
GET /api/v1/incidents/{incidentID}
GET /api/v1/incidents/{incidentID}/events
```

## Health and logs

The recorder exposes `GET /healthz` for process liveness and `GET /readyz` for readiness. Readiness checks that both the configured database responds and the Kubernetes watcher has synchronized its caches; Kubernetes removes the recorder from service endpoints when either check fails. The recorder writes structured request, startup, watcher, and storage failure logs to standard output, so they are available through the container logs. Kubernetes client-go logs are routed through the same logger.

The recorder is instrumented with OpenTelemetry for HTTP server traces and metrics, storage spans, and Kubernetes watcher spans. It also emits counters named `incidentlens.incident.actions` and `incidentlens.watcher.observations`, with low-cardinality action/outcome attributes. Structured `slog` records stay on standard output and can also be exported as OpenTelemetry logs. Telemetry export is opt-in; configure an OTLP/HTTP collector endpoint to enable all three signals:

```sh
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
```

Use `OTEL_TRACES_EXPORTER=none`, `OTEL_METRICS_EXPORTER=none`, or `OTEL_LOGS_EXPORTER=none` to disable an individual signal. You can also set a signal-specific endpoint such as `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`. The service resource name defaults to `incident-lens-recorder`; standard resource environment variables such as `OTEL_SERVICE_NAME` and `OTEL_RESOURCE_ATTRIBUTES` are read at startup. Without an endpoint or `OTEL_*_EXPORTER=otlp`, the recorder continues to run normally and emits JSON logs to stdout.

Events and observations are stored separately: observations preserve what the watcher saw, while incident events provide the incident-specific timeline. A single observation may be associated with more than one matching open incident.
