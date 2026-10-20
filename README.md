# IncidentLens

IncidentLens is a personal Kubernetes incident recorder. It connects alerts with nearby Kubernetes observations and keeps adding matching observations while an incident is open, helping reconstruct what happened.

## Current status

The project includes a demo app, a Go recorder API, SQLite persistence, and a namespace-scoped Kubernetes watcher for Events and Deployment pod-template changes. The recorder can open and resolve incidents, backfill the five minutes of observations before an alert, and attach new matching observations while the incident is open.

## Incident lifecycle API

Submit an event to `POST /api/v1/events` to open or resolve an incident. An `alert_fired` event opens the incident using its `incident_id`, `namespace`, `service`, `timestamp`, and `summary`. IncidentLens backfills matching observations from the previous five minutes. New matching Kubernetes observations are attached while the incident remains open.

Submit an event with `type: "alert_resolved"` and the same `incident_id` to close it. Other event types are saved to the specified incident timeline.

### Alertmanager webhook

Configure an Alertmanager webhook receiver to send notifications to `POST /api/v1/ingest/alertmanager`. The endpoint accepts Alertmanager webhook version 4, including grouped notifications. Each alert is mapped independently; its fingerprint and `startsAt` identify one incident lifecycle, so Alertmanager retries do not create duplicate timeline events and a later firing creates a new incident. The alert's labels and annotations are retained in event evidence. Include a `namespace` label and preferably a `service` label in your alert rules so Kubernetes observations can be matched to the incident.

```yaml
receivers:
  - name: incident-lens
    webhook_configs:
      - url: http://recorder.incident-lens.svc/api/v1/ingest/alertmanager
        send_resolved: true
```

Alertmanager should be able to reach the recorder service over the cluster network. The endpoint returns a non-2xx response when validation or storage fails, allowing Alertmanager to retry delivery.

Retrieve lifecycle state and the ordered timeline with:

```text
GET /api/v1/incidents?namespace=incident-lens&status=open&limit=50&offset=0
GET /api/v1/incidents/{incidentID}
GET /api/v1/incidents/{incidentID}/events
```

The incident list accepts optional `namespace`, `service`, `status` (`open` or `closed`), `from`, and `to` filters. `from` and `to` are RFC3339 timestamps and filter by incident start time. Results are ordered newest first with a stable ID tie-breaker. Pagination uses `limit` (default 50, maximum 200) and `offset` (default 0).

## Health and logs

The recorder exposes `GET /health` for process liveness and `GET /ready` for readiness. Readiness checks that both the configured database responds and the Kubernetes watcher has synchronized its caches; Kubernetes removes the recorder from service endpoints when either check fails. Set `WATCH_NAMESPACE` to choose the namespace monitored by the recorder; it defaults to `incident-lens`. The recorder writes structured request, startup, watcher, and storage failure logs to standard output, so they are available through the container logs. Kubernetes client-go logs are routed through the same logger.

The recorder is instrumented with OpenTelemetry for HTTP server traces and metrics, storage spans, and Kubernetes watcher spans. It also emits counters named `incidentlens.incident.actions` and `incidentlens.watcher.observations`, with low-cardinality action/outcome attributes. Structured `slog` records stay on standard output and can also be exported as OpenTelemetry logs. Telemetry export is opt-in; configure an OTLP/HTTP collector endpoint to enable all three signals:

```sh
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
```

Use `OTEL_TRACES_EXPORTER=none`, `OTEL_METRICS_EXPORTER=none`, or `OTEL_LOGS_EXPORTER=none` to disable an individual signal. You can also set a signal-specific endpoint such as `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`. The service resource name defaults to `incident-lens-recorder`; standard resource environment variables such as `OTEL_SERVICE_NAME` and `OTEL_RESOURCE_ATTRIBUTES` are read at startup. Without an endpoint or `OTEL_*_EXPORTER=otlp`, the recorder continues to run normally and emits JSON logs to stdout.

Events and observations are stored separately: observations preserve what the watcher saw, while incident events provide the incident-specific timeline. A single observation may be associated with more than one matching open incident.

## Web console

The React and TypeScript console lives in `web/` and is deployed independently from the recorder. It provides a filterable, paginated incident list and an incident detail view with its chronological event timeline and captured evidence.

### Run locally

Start the recorder on port 8080, then in another terminal:

```sh
cd web
bun install
bun run dev
```

Open the Vite URL (normally `http://localhost:5173`). Vite forwards `/api` requests to the local recorder at `http://localhost:8080`.

### Build and deploy in Kubernetes

Build the frontend image from the repository root so the Dockerfile can access the `web/` source and shared Nginx config:

```sh
docker build -f build/docker/frontend.Dockerfile -t incident-lens/frontend:dev .
```

Load or push that image using the workflow for your cluster, then apply the frontend resources:

```sh
kubectl apply -f deployments/k8s/frontend.yaml
```

The frontend runs in the separate `incident-lens-ui` namespace. Its Nginx server serves the single-page app and proxies `/api/` requests to `recorder.incident-lens.svc.cluster.local`, so the browser does not need direct access to the recorder service. The frontend Service is internal (`ClusterIP`); for local access, forward a local port to it:

```sh
kubectl port-forward -n incident-lens-ui service/frontend 8081:80
```

Then open `http://localhost:8081`. For access beyond a local cluster, expose the frontend Service through an Ingress or Gateway configured for your cluster; the recorder can remain private.
