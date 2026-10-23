import { useEffect, useState } from "react";
import { Link } from "react-router";
import { ApiError } from "../../api/client";
import {
  getIncident,
  getIncidentAnalysis,
  listIncidentEvents,
  type Incident,
  type IncidentAnalysis,
  type IncidentEvent,
} from "../../api/incident";

function formatDate(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function formatDuration(start: Date, end: Date) {
  const ms = end.getTime() - start.getTime();
  if (!Number.isFinite(ms) || ms < 0) return "—";
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ${seconds % 60}s`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ${minutes % 60}m`;
  const days = Math.floor(hours / 24);
  return `${days}d ${hours % 24}h`;
}

const buttonClass =
  "inline-flex min-h-9 items-center justify-center rounded-md border border-slate-200 bg-white px-3.5 text-sm font-semibold text-slate-700 transition hover:border-slate-300 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50";

export function IncidentDetailPage({ incidentId }: { incidentId: string }) {
  const [incident, setIncident] = useState<Incident | null>(null);
  const [events, setEvents] = useState<IncidentEvent[]>([]);
  const [analysis, setAnalysis] = useState<IncidentAnalysis | null>(null);
  const [analysisUnavailable, setAnalysisUnavailable] = useState(false);
  const [error, setError] = useState<{
    incidentId: string;
    message: string;
  } | null>(null);
  const [loading, setLoading] = useState(true);
  const [refresh, setRefresh] = useState(0);
  const [loadedIncidentId, setLoadedIncidentId] = useState<string | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    setAnalysis(null);
    setAnalysisUnavailable(false);
    Promise.all([
      getIncident(incidentId, controller.signal),
      listIncidentEvents(incidentId, controller.signal),
    ])
      .then(([loadedIncident, loadedEvents]) => {
        setIncident(loadedIncident);
        setLoadedIncidentId(incidentId);
        setEvents(
          [...loadedEvents].sort(
            (a, b) => Date.parse(a.timestamp) - Date.parse(b.timestamp),
          ),
        );
      })
      .catch((err: unknown) => {
        if (!controller.signal.aborted) {
          setError({
            incidentId,
            message:
              err instanceof ApiError && err.status === 404
                ? "This incident could not be found."
                : err instanceof Error
                  ? err.message
                  : "Could not load incident.",
          });
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    getIncidentAnalysis(incidentId, controller.signal)
      .then((result) => {
        setAnalysis(result);
      })
      .catch(() => {
        if (!controller.signal.aborted) setAnalysisUnavailable(true);
      });
    return () => controller.abort();
  }, [incidentId, refresh]);

  function reload() {
    setLoading(true);
    setError(null);
    setRefresh((value) => value + 1);
  }

  const currentError = error?.incidentId === incidentId ? error.message : null;
  if (loading || (!currentError && loadedIncidentId !== incidentId)) {
    return (
      <p className="py-12 text-center text-sm text-slate-500" role="status">
        Loading incident…
      </p>
    );
  }
  if (currentError || !incident) {
    return (
      <section>
        <Link
          className="mb-6 inline-block font-semibold text-slate-600 no-underline hover:text-teal hover:underline"
          to="/incidents"
        >
          ← All incidents
        </Link>
        <div
          className="flex items-center justify-between gap-4 rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-800"
          role="alert"
        >
          <span>{currentError || "Could not load incident."}</span>
          <button className={buttonClass} type="button" onClick={reload}>
            Retry
          </button>
        </div>
      </section>
    );
  }

  return (
    <section>
      <Link
        className="mb-6 inline-block font-semibold text-slate-600 no-underline hover:text-teal hover:underline"
        to="/incidents"
      >
        ← All incidents
      </Link>
      <div className="mb-7 flex items-start justify-between gap-6">
        <div>
          <p className="mb-2 font-mono text-[.68rem] tracking-[.14em] text-slate-500">
            INCIDENT DETAIL
          </p>
          <h1 className="mb-2 text-3xl font-semibold tracking-tight sm:text-4xl">
            {incident.summary || "Untitled incident"}
          </h1>
          <p className="text-sm text-slate-500">
            {incident.namespace}
            {incident.service
              ? ` / ${incident.service}`
              : " / Service not specified"}
          </p>
        </div>
        <span
          className={`mt-1 inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-bold capitalize ${incident.status === "open" ? "bg-orange-50 text-orange-800" : "bg-emerald-50 text-emerald-800"}`}
        >
          <span
            aria-hidden="true"
            className="size-1.5 rounded-full bg-current"
          />
          {incident.status}
        </span>
      </div>

      <div className="mb-10 grid grid-cols-2 gap-2.5 md:grid-cols-4">
        <SummaryCard label="Started">
          <time dateTime={incident.started_at}>
            {formatDate(incident.started_at)}
          </time>
        </SummaryCard>
        <SummaryCard label="Resolved">
          {incident.resolved_at ? (
            <time dateTime={incident.resolved_at}>
              {formatDate(incident.resolved_at)}
            </time>
          ) : (
            "Still open"
          )}
        </SummaryCard>
        <SummaryCard label="Duration">
          {incident.resolved_at
            ? formatDuration(
                new Date(incident.started_at),
                new Date(incident.resolved_at),
              )
            : "Ongoing"}
        </SummaryCard>
        <SummaryCard label="Timeline events">{events.length}</SummaryCard>
      </div>

      <section className="mb-10" aria-labelledby="signals-heading">
        <p className="mb-2 font-mono text-[.68rem] tracking-[.14em] text-slate-500">
          RULE BASED ANALYSIS
        </p>
        <h2
          id="signals-heading"
          className="mb-3 text-2xl font-semibold tracking-tight"
        >
          Signals found
        </h2>
        <p className="mb-4 text-sm text-slate-500">
          These signals point to timeline evidence for review. They do not prove
          what caused the incident.
        </p>
        {analysisUnavailable ? (
          <p
            className="rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900"
            role="status"
          >
            Analysis is temporarily unavailable. The incident timeline is still
            available below.
          </p>
        ) : analysis && analysis.findings.length === 0 ? (
          <p className="rounded-lg border border-slate-200 bg-white p-4 text-sm text-slate-500">
            No matching signals were found in the captured timeline.
          </p>
        ) : analysis ? (
          <ul className="space-y-3">
            {analysis.findings.map((finding) => (
              <li
                className="rounded-xl border border-teal/20 bg-white p-4 shadow-sm"
                key={finding.rule_id}
              >
                <h3 className="font-semibold">{finding.title}</h3>
                <p className="mb-3 mt-1 text-sm text-slate-600">
                  {finding.summary}
                </p>
                <div className="flex flex-wrap gap-2">
                  {finding.evidence_event_ids.map((eventId) => (
                    <button
                      className="rounded border border-slate-200 bg-slate-50 px-2 py-1 font-mono text-xs text-teal hover:bg-teal/5"
                      key={eventId}
                      type="button"
                      onClick={() =>
                        document
                          .getElementById(`timeline-event-${eventId}`)
                          ?.scrollIntoView({
                            behavior: "smooth",
                            block: "center",
                          })
                      }
                    >
                      View evidence {eventId.slice(0, 8)}
                    </button>
                  ))}
                </div>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-sm text-slate-500" role="status">
            Checking captured evidence…
          </p>
        )}
      </section>

      <div className="mb-5 flex items-center justify-between">
        <div>
          <p className="mb-2 font-mono text-[.68rem] tracking-[.14em] text-slate-500">
            INVESTIGATION
          </p>
          <h2 className="text-2xl font-semibold tracking-tight">Timeline</h2>
        </div>
        <button
          className={buttonClass}
          type="button"
          onClick={reload}
          disabled={loading}
        >
          Refresh
        </button>
      </div>
      {events.length === 0 ? (
        <div className="rounded-xl border border-dashed border-slate-300 bg-white px-6 py-12 text-center">
          <h2 className="mb-2 text-lg font-semibold">No timeline events yet</h2>
          <p className="text-sm text-slate-500">
            Events associated with this incident will appear here.
          </p>
        </div>
      ) : (
        <ol className="relative ml-2 border-l border-slate-200 pl-7">
          {events.map((event) => (
            <li
              className="relative pb-4 last:pb-0"
              id={`timeline-event-${event.id}`}
              key={event.id}
            >
              <span
                className="absolute -left-[2.18rem] top-4 z-10 size-7 rounded-full border-4 border-canvas bg-emerald-100"
                aria-hidden="true"
              />
              <article className="rounded-xl border border-slate-200 bg-white p-4 shadow-sm sm:p-5">
                <div className="flex items-start justify-between gap-5 max-sm:flex-col max-sm:gap-2">
                  <div>
                    <span className="mb-1.5 inline-block font-mono text-[.65rem] font-medium uppercase tracking-wider text-teal">
                      {event.type.replaceAll("_", " ")}
                    </span>
                    <h3 className="font-semibold">
                      {event.summary || "Event"}
                    </h3>
                  </div>
                  <time
                    className="whitespace-nowrap text-xs text-slate-500"
                    dateTime={event.timestamp}
                  >
                    {formatDate(event.timestamp)}
                  </time>
                </div>
                <p className="mb-3 mt-2 text-xs text-slate-500">
                  {event.namespace}
                  {event.service ? ` / ${event.service}` : ""}
                  {event.source
                    ? ` · ${event.source.replaceAll("_", " ")}`
                    : ""}
                </p>
                {event.object?.kind && (
                  <div className="flex w-fit max-w-full flex-wrap items-center gap-2 rounded-md bg-slate-50 px-3 py-2 text-xs text-slate-600">
                    <span className="font-semibold">{event.object.kind}</span>
                    <code className="break-all font-mono text-slate-800">
                      {event.object.name}
                    </code>
                    {event.object.api_version && (
                      <small className="text-slate-400">
                        {event.object.api_version}
                      </small>
                    )}
                  </div>
                )}
                {event.observed_at && event.observed_at !== event.timestamp && (
                  <p className="mb-0 mt-3 text-xs text-slate-400">
                    Observed by recorder{" "}
                    <time dateTime={event.observed_at}>
                      {formatDate(event.observed_at)}
                    </time>
                  </p>
                )}
                {event.evidence && Object.keys(event.evidence).length > 0 && (
                  <details className="mt-3 border-t border-slate-100 pt-3">
                    <summary className="cursor-pointer text-xs font-semibold text-slate-600">
                      Evidence
                    </summary>
                    <pre className="mt-3 max-h-72 overflow-auto whitespace-pre-wrap break-all rounded-md bg-slate-50 p-3 font-mono text-xs leading-relaxed text-slate-600">
                      {JSON.stringify(event.evidence, null, 2)}
                    </pre>
                  </details>
                )}
              </article>
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}

function SummaryCard({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <article className="flex min-h-20 flex-col gap-2 rounded-lg border border-slate-200 bg-white p-3.5 sm:p-4">
      <span className="text-xs text-slate-500">{label}</span>
      <strong className="text-sm font-semibold text-slate-800">
        {children}
      </strong>
    </article>
  );
}
