import {
  context,
  propagation,
  SpanStatusCode,
  trace,
  type Span,
  type Counter,
  type Histogram,
} from "@opentelemetry/api";
import { logs, SeverityNumber } from "@opentelemetry/api-logs";
import { W3CTraceContextPropagator } from "@opentelemetry/core";
import { OTLPLogExporter } from "@opentelemetry/exporter-logs-otlp-proto";
import { OTLPMetricExporter } from "@opentelemetry/exporter-metrics-otlp-proto";
import { OTLPTraceExporter } from "@opentelemetry/exporter-trace-otlp-proto";
import { resourceFromAttributes } from "@opentelemetry/resources";
import { LoggerProvider, BatchLogRecordProcessor } from "@opentelemetry/sdk-logs";
import { MeterProvider, PeriodicExportingMetricReader } from "@opentelemetry/sdk-metrics";
import { BatchSpanProcessor, TracerProvider } from "@opentelemetry/sdk-trace";
import { metrics } from "@opentelemetry/api";

const instrumentationName = "incident-lens-web";
const sharedEndpoint = import.meta.env.VITE_OTEL_EXPORTER_OTLP_ENDPOINT?.trim();
const traceEndpoint = signalEndpoint(import.meta.env.VITE_OTEL_EXPORTER_OTLP_TRACES_ENDPOINT, "traces");
const metricEndpoint = signalEndpoint(import.meta.env.VITE_OTEL_EXPORTER_OTLP_METRICS_ENDPOINT, "metrics");
const logEndpoint = signalEndpoint(import.meta.env.VITE_OTEL_EXPORTER_OTLP_LOGS_ENDPOINT, "logs");

let requestCounter: Counter | undefined;
let requestDuration: Histogram | undefined;
let navigationCounter: Counter | undefined;
let logger = logs.getLogger(instrumentationName);

export function initializeTelemetry() {
  if (!traceEndpoint && !metricEndpoint && !logEndpoint) return;

  const resource = resourceFromAttributes({
    "service.name": import.meta.env.VITE_OTEL_SERVICE_NAME || "incident-lens-web",
    "service.version": import.meta.env.VITE_APP_VERSION || "dev",
    "deployment.environment.name": import.meta.env.MODE,
  });

  if (traceEndpoint) {
    const provider = new TracerProvider({
      resource,
      spanProcessors: [new BatchSpanProcessor({ exporter: new OTLPTraceExporter({ url: traceEndpoint }) })],
    });
    trace.setGlobalTracerProvider(provider);
    propagation.setGlobalPropagator(new W3CTraceContextPropagator());
  }

  if (metricEndpoint) {
    const reader = new PeriodicExportingMetricReader({
      exporter: new OTLPMetricExporter({ url: metricEndpoint }),
      exportIntervalMillis: 30_000,
      exportTimeoutMillis: 10_000,
    });
    const provider = new MeterProvider({ resource, readers: [reader] });
    metrics.setGlobalMeterProvider(provider);
    const meter = provider.getMeter(instrumentationName);
    requestCounter = meter.createCounter("incidentlens.ui.api.requests", {
      description: "Frontend API requests grouped by route and outcome",
    });
    requestDuration = meter.createHistogram("incidentlens.ui.api.request.duration", {
      unit: "ms",
      description: "Frontend API request duration",
    });
    navigationCounter = meter.createCounter("incidentlens.ui.navigation", {
      description: "Frontend route changes",
    });
    window.addEventListener("pagehide", () => void provider.forceFlush());
    document.addEventListener("visibilitychange", () => {
      if (document.visibilityState === "hidden") void provider.forceFlush();
    });
  }

  if (logEndpoint) {
    const provider = new LoggerProvider({
      resource,
      processors: [new BatchLogRecordProcessor({ exporter: new OTLPLogExporter({ url: logEndpoint }) })],
    });
    logs.setGlobalLoggerProvider(provider);
    logger = provider.getLogger(instrumentationName);
    window.addEventListener("pagehide", () => void provider.forceFlush());
    document.addEventListener("visibilitychange", () => {
      if (document.visibilityState === "hidden") void provider.forceFlush();
    });
  }
}

export function startApiSpan(method: string, route: string): Span {
  return trace.getTracer(instrumentationName).startSpan(`HTTP ${method}`, {
    attributes: {
      "http.request.method": method,
      "http.route": route,
      "server.address": window.location.host,
    },
  });
}

export function injectTraceContext(span: Span, headers: Record<string, string>) {
  propagation.inject(trace.setSpan(context.active(), span), headers);
}

export function recordApiRequest(
  route: string,
  method: string,
  statusCode: number,
  durationMs: number,
) {
  const attributes = {
    "http.route": route,
    "http.request.method": method,
    "http.response.status_code": statusCode,
  };
  requestCounter?.add(1, attributes);
  requestDuration?.record(durationMs, attributes);
}

export function logApiFailure(
  route: string,
  method: string,
  statusCode: number,
  error: unknown,
) {
  logger.emit({
    severityNumber: SeverityNumber.ERROR,
    severityText: "ERROR",
    body: "Frontend API request failed",
    attributes: {
      "http.route": route,
      "http.request.method": method,
      "http.response.status_code": statusCode,
      "error.type": error instanceof Error ? error.name : "UnknownError",
    },
  });
}

export function recordNavigation(route: string) {
  const span = trace.getTracer(instrumentationName).startSpan("ui.navigation", {
    attributes: { "http.route": route },
  });
  span.end();
  navigationCounter?.add(1, { "http.route": route });
}

function signalEndpoint(signalEndpointValue: string | undefined, path: string) {
  const configured = signalEndpointValue?.trim() || sharedEndpoint;
  if (!configured) return undefined;
  if (configured.endsWith(`/v1/${path}`)) return configured;
  return `${configured.replace(/\/$/, "")}/v1/${path}`;
}

export function setSpanError(span: Span, error: unknown) {
  span.setAttribute("error.type", error instanceof Error ? error.name : "UnknownError");
  span.setStatus({ code: SpanStatusCode.ERROR, message: "API request failed" });
}
