import { SpanStatusCode } from "@opentelemetry/api";
import {
  injectTraceContext,
  logApiFailure,
  recordApiRequest,
  setSpanError,
  startApiSpan,
} from "../telemetry";

export class ApiError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

export async function getJSON<T>(
  path: string,
  signal?: AbortSignal,
): Promise<T> {
  const method = "GET";
  const route = normalizeRoute(path);
  const span = startApiSpan(method, route);
  const headers: Record<string, string> = { Accept: "application/json" };
  injectTraceContext(span, headers);
  const startedAt = performance.now();
  let statusCode = 0;

  try {
    const response = await fetch(path, { headers, signal });
    statusCode = response.status;
    span.setAttribute("http.response.status_code", statusCode);

    if (!response.ok) {
      const text = await response.text();
      let message = `Request failed with status ${response.status}`;
      if (text) {
        try {
          const body = JSON.parse(text) as { error?: string };
          message = body.error ?? text;
        } catch {
          message = text;
        }
      }
      throw new ApiError(message, response.status);
    }

    span.setStatus({ code: SpanStatusCode.OK });
    return (await response.json()) as T;
  } catch (error) {
    setSpanError(span, error);
    logApiFailure(route, method, statusCode, error);
    throw error;
  } finally {
    recordApiRequest(route, method, statusCode, performance.now() - startedAt);
    span.end();
  }
}

function normalizeRoute(path: string) {
  const cleanPath = path.split("?", 1)[0];
  return cleanPath
    .replace(/(\/api\/v1\/incidents)\/[^/]+\/events$/, "$1/{incidentID}/events")
    .replace(/(\/api\/v1\/incidents)\/[^/]+$/, "$1/{incidentID}");
}
