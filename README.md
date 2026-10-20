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

### Frontend OpenTelemetry

The React client can export traces, metrics, and logs using OTLP over HTTP. Tracing covers API calls and page navigation; API calls also contribute request-count and duration metrics, while failed calls emit structured error logs. The telemetry resource identifies the frontend as `incident-lens-web`. Export is disabled unless an OTLP endpoint is configured, and telemetry uses route templates rather than incident IDs.

For local development, run Jaeger with an OTLP/HTTP receiver on port 4318, copy `web/.env.example` to `web/.env.local`, then start Vite. Jaeger stores traces in memory, and the Vite `/otlp` proxy forwards to `localhost:4318`, avoiding browser CORS. Jaeger is for tracing here; leave frontend metrics and logs pointed at an OpenTelemetry Collector if you want those signals too.

If Jaeger is running in Kubernetes, forward its OTLP and UI ports in separate terminals:

```sh
kubectl port-forward -n incident-lens-observability service/jaeger 4318:4318
kubectl port-forward -n incident-lens-observability service/jaeger 16686:16686
```

Then start Vite with the copied `.env.local` and open `http://localhost:16686` for Jaeger.

For the container image, set a browser-reachable OTLP endpoint at build time. The local Jaeger setup uses `/otlp`, which Nginx forwards to Jaeger's OTLP/HTTP receiver:

```sh
docker build \
  --build-arg VITE_OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=/otlp \
  -f build/docker/frontend.Dockerfile \
  -t incident-lens/frontend:dev .
```

If the Collector is on a different origin from the UI, configure its OTLP/HTTP receiver CORS `allowed_origins` for the exact UI origin and allow the `Content-Type` header. Do not put secrets in `VITE_*` variables; they are embedded in public frontend assets. Browser instrumentation and the OpenTelemetry JS Logs SDK are still evolving, so use frontend telemetry alongside the recorder's server-side signals.

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

### Test traces with Jaeger in local Kubernetes

The local Jaeger manifest runs the Jaeger 2 all-in-one image with in-memory trace storage. Traces disappear when its pod is restarted. Apply Jaeger first so its Service exists before the frontend Nginx configuration starts:

```sh
kubectl apply -f deployments/k8s/jaeger.yaml
kubectl rollout status deployment/jaeger -n incident-lens-observability
```

Restrict Jaeger's OTLP/HTTP receiver to the frontend and recorder pods:

```sh
kubectl apply -f deployments/k8s/telemetry-networkpolicy.yaml
```

This policy selects the current Jaeger pod (`app: jaeger`) and allows only TCP/4318 from `app: frontend` in `incident-lens-ui` and `app: recorder` in `incident-lens`. It does not allow OTLP/gRPC on 4317. The frontend image's `/otlp` proxy and the recorder exporter both use OTLP/HTTP. NetworkPolicies are additive, so check for other policies that grant broader ingress, and confirm your cluster CNI enforces NetworkPolicies. If Jaeger is replaced by an OpenTelemetry Collector gateway, update the policy's pod selector to the Collector's labels.

The recorder manifest configures its trace exporter to send to Jaeger. Apply it and wait for the rollout:

```sh
kubectl apply -f deployments/k8s/recorder-deployment.yaml
kubectl rollout status deployment/recorder -n incident-lens
```

Build the UI with browser tracing enabled, then make the image available in your local cluster. Use the matching image-load command for your cluster:

```sh
docker build \
  --build-arg VITE_OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=/otlp \
  -f build/docker/frontend.Dockerfile \
  -t incident-lens/frontend:dev .
```

For Kind, run `kind load docker-image incident-lens/frontend:dev`. For Minikube, run `minikube image load incident-lens/frontend:dev`. For k3d, run `k3d image import incident-lens/frontend:dev` (add `-c <cluster-name>` if needed). Then apply or restart the frontend:

```sh
kubectl apply -f deployments/k8s/frontend.yaml
kubectl rollout restart deployment/frontend -n incident-lens-ui
kubectl rollout status deployment/frontend -n incident-lens-ui
```

The frontend sends browser traces to its same-origin `/otlp/v1/traces` path. Nginx forwards them to Jaeger across namespaces, so the browser does not need Jaeger CORS access. The recorder sends its server-side traces directly to Jaeger using its in-cluster Service DNS name.

Open Jaeger and the UI in separate terminals:

```sh
kubectl port-forward -n incident-lens-observability service/jaeger 16686:16686
kubectl port-forward -n incident-lens-ui service/frontend 8081:80
```

Open `http://localhost:8081`, browse the incident list and details, then view `http://localhost:16686`. In Jaeger, choose `incident-lens-web` or `github.com/iwantm/IncidentLens` in the Service selector and click **Find Traces**. For a connected frontend-to-recorder trace, open a detail page that makes an API request; the browser trace context is propagated to the recorder. Jaeger does not receive this app's metrics or logs: use an OpenTelemetry Collector to receive those signals.
