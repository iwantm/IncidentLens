# IncidentLens web console

React and TypeScript frontend for the IncidentLens recorder, styled with Tailwind CSS 4.

Run `bun install` and `bun run dev` from this directory. Vite proxies `/api` to the recorder at `http://localhost:8080`.

For the production container and Kubernetes setup, see the root [README](../README.md#web-console).


## Frontend OpenTelemetry

The React client can export traces, metrics, and logs using OTLP over HTTP. Tracing covers API calls and page navigation; API calls also contribute request count and duration metrics, while failed calls emit structured error logs. The telemetry resource identifies the frontend as `incident-lens-web`. Export is disabled unless an OTLP endpoint is configured, and telemetry contains route templates rather than incident IDs.

For local development, start Jaeger with its OTLP/HTTP receiver on port 4318, copy `.env.example` to `.env.local`, then start Vite. If Jaeger is in Kubernetes, forward ports with `kubectl port-forward -n incident-lens-observability service/jaeger 4318:4318` and `kubectl port-forward -n incident-lens-observability service/jaeger 16686:16686`. The Vite `/otlp` proxy forwards to `localhost:4318`, avoiding browser CORS. Open `http://localhost:16686` to inspect traces. Jaeger shows traces; use an OpenTelemetry Collector for metrics and logs.

For the container image, configure the browser-visible OTLP endpoint at build time:

```sh
docker build \
  --build-arg VITE_OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=/otlp \
  -f build/docker/frontend.Dockerfile \
  -t incident-lens/frontend:dev .
```

The browser must be able to reach that endpoint. If it is a different origin from the UI, configure the Collector's OTLP/HTTP receiver CORS `allowed_origins` for the exact UI origin and allow `Content-Type`. Do not put secrets in `VITE_*` variables; they are embedded in public frontend assets. The OpenTelemetry JavaScript browser instrumentation and Logs SDK are still evolving, so treat browser telemetry as a useful supplement to the recorder's server-side signals.
