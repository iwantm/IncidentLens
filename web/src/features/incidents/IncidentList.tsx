import { useEffect, useState, type FormEvent } from "react";
import { Link } from "react-router";
import {
  listIncidents,
  type Incident,
  type IncidentStatus,
  type ListIncidentsOptions,
} from "../../api/incident";

const PAGE_SIZE = 50;
const inputClass = "mt-1.5 h-10 w-full rounded-md border border-slate-200 bg-white px-2.5 text-sm text-ink placeholder:text-slate-400 focus:border-teal";
const buttonClass = "inline-flex min-h-9 items-center justify-center rounded-md border border-slate-200 bg-white px-3.5 text-sm font-semibold text-slate-700 transition hover:border-slate-300 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50";

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
  return `${hours}h ${minutes % 60}m`;
}

function toRFC3339(value: string) {
  return value ? new Date(value).toISOString() : "";
}

export function IncidentListPage() {
  const [incidents, setIncidents] = useState<Incident[]>([]);
  const [filters, setFilters] = useState({ namespace: "", service: "", status: "", from: "", to: "" });
  const [draft, setDraft] = useState(filters);
  const [offset, setOffset] = useState(0);
  const [refresh, setRefresh] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const controller = new AbortController();
    const options: ListIncidentsOptions = {
      namespace: filters.namespace || undefined,
      service: filters.service || undefined,
      status: (filters.status || undefined) as IncidentStatus | undefined,
      from: filters.from || undefined,
      to: filters.to || undefined,
      limit: PAGE_SIZE,
      offset,
    };
    listIncidents(options, controller.signal)
      .then(setIncidents)
      .catch((err: unknown) => {
        if (!controller.signal.aborted) setError(err instanceof Error ? err.message : "Could not load incidents");
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [filters, offset, refresh]);

  function applyFilters(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setLoading(true);
    setError(null);
    setOffset(0);
    setFilters({
      namespace: draft.namespace.trim(),
      service: draft.service.trim(),
      status: draft.status,
      from: toRFC3339(draft.from),
      to: toRFC3339(draft.to),
    });
  }

  function clearFilters() {
    setLoading(true);
    setError(null);
    const empty = { namespace: "", service: "", status: "", from: "", to: "" };
    setDraft(empty);
    setFilters(empty);
    setOffset(0);
  }

  function reload() {
    setLoading(true);
    setError(null);
    setRefresh((value) => value + 1);
  }

  function changePage(nextOffset: number) {
    setLoading(true);
    setError(null);
    setOffset(nextOffset);
  }

  return (
    <section>
      <div className="mb-7 flex items-center justify-between gap-6">
        <div>
          <p className="mb-2 font-mono text-[.68rem] tracking-[.14em] text-slate-500">OPERATIONS</p>
          <h1 className="mb-1 text-3xl font-semibold tracking-tight sm:text-4xl">Incidents</h1>
          <p className="text-sm text-slate-500">Browse and investigate captured incidents.</p>
        </div>
        <button className={buttonClass} type="button" onClick={reload} disabled={loading}>Refresh</button>
      </div>

      <form className="mb-5 grid grid-cols-1 items-end gap-3.5 rounded-xl border border-slate-200 bg-white p-4 shadow-sm sm:grid-cols-2 lg:grid-cols-5" onSubmit={applyFilters}>
        <label className="text-xs font-semibold text-slate-600">Namespace<input className={inputClass} value={draft.namespace} onChange={(event) => setDraft({ ...draft, namespace: event.target.value })} placeholder="incident-lens" /></label>
        <label className="text-xs font-semibold text-slate-600">Service<input className={inputClass} value={draft.service} onChange={(event) => setDraft({ ...draft, service: event.target.value })} placeholder="demo-app" /></label>
        <label className="text-xs font-semibold text-slate-600">Status<select className={inputClass} value={draft.status} onChange={(event) => setDraft({ ...draft, status: event.target.value })}><option value="">All statuses</option><option value="open">Open</option><option value="closed">Closed</option></select></label>
        <label className="text-xs font-semibold text-slate-600">Started after<input className={inputClass} type="datetime-local" value={draft.from} onChange={(event) => setDraft({ ...draft, from: event.target.value })} /></label>
        <label className="text-xs font-semibold text-slate-600">Started before<input className={inputClass} type="datetime-local" value={draft.to} onChange={(event) => setDraft({ ...draft, to: event.target.value })} /></label>
        <div className="flex gap-2 sm:col-span-2 lg:col-span-5">
          <button className="inline-flex min-h-9 items-center justify-center rounded-md bg-teal px-3.5 text-sm font-semibold text-white transition hover:bg-teal-dark" type="submit">Apply filters</button>
          <button className="inline-flex min-h-9 items-center justify-center rounded-md px-3.5 text-sm font-semibold text-slate-600 hover:bg-slate-100" type="button" onClick={clearFilters}>Clear</button>
        </div>
      </form>

      {error && <div className="my-4 flex items-center justify-between gap-4 rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-800" role="alert"><span>{error}</span><button className={buttonClass} type="button" onClick={reload}>Retry</button></div>}
      {loading && <p className="py-12 text-center text-sm text-slate-500" role="status">Loading incidents…</p>}
      {!loading && !error && incidents.length === 0 && <div className="rounded-xl border border-dashed border-slate-300 bg-white px-6 py-12 text-center"><h2 className="mb-2 text-lg font-semibold">{offset > 0 ? "No more incidents" : "No incidents found"}</h2><p className="mb-4 text-sm text-slate-500">{offset > 0 ? "You have reached the end of this result set." : "Try changing the filters, or submit an alert to create an incident."}</p>{offset > 0 && <button className={buttonClass} type="button" onClick={() => changePage(Math.max(0, offset - PAGE_SIZE))}>Previous page</button>}</div>}

      {!loading && incidents.length > 0 && <>
        <div className="overflow-x-auto rounded-xl border border-slate-200 bg-white shadow-sm">
          <table className="w-full min-w-[760px] border-collapse text-left text-sm">
            <thead className="bg-slate-50 font-mono text-[.67rem] uppercase tracking-wider text-slate-500"><tr><th className="border-b border-slate-200 px-4 py-3">Incident</th><th className="border-b border-slate-200 px-4 py-3">Scope</th><th className="border-b border-slate-200 px-4 py-3">Status</th><th className="border-b border-slate-200 px-4 py-3">Started</th><th className="border-b border-slate-200 px-4 py-3">Duration</th><th className="border-b border-slate-200 px-4 py-3"><span className="sr-only">Actions</span></th></tr></thead>
            <tbody>{incidents.map((incident) => <tr className="last:[&>td]:border-b-0 hover:bg-slate-50/70" key={incident.id}>
              <td className="border-b border-slate-100 px-4 py-3.5"><Link className="block max-w-[350px] font-semibold text-slate-800 no-underline hover:text-teal hover:underline" to={`/incidents/${encodeURIComponent(incident.id)}`}>{incident.summary || "Untitled incident"}</Link><span className="mt-1 block text-xs text-slate-400">{incident.id}</span></td>
              <td className="border-b border-slate-100 px-4 py-3.5 text-slate-700">{incident.namespace}<span className="mt-1 block text-xs text-slate-400">{incident.service || "Service not specified"}</span></td>
              <td className="border-b border-slate-100 px-4 py-3.5"><span className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-bold capitalize ${incident.status === "open" ? "bg-orange-50 text-orange-800" : "bg-emerald-50 text-emerald-800"}`}><span aria-hidden="true" className="size-1.5 rounded-full bg-current" />{incident.status}</span></td>
              <td className="border-b border-slate-100 px-4 py-3.5 text-slate-700"><time dateTime={incident.started_at}>{formatDate(incident.started_at)}</time></td>
              <td className="border-b border-slate-100 px-4 py-3.5 text-slate-700">{incident.resolved_at ? formatDuration(new Date(incident.started_at), new Date(incident.resolved_at)) : "Ongoing"}</td>
              <td className="border-b border-slate-100 px-4 py-3.5"><Link className="whitespace-nowrap font-semibold text-teal no-underline hover:underline" to={`/incidents/${encodeURIComponent(incident.id)}`}>Open →</Link></td>
            </tr>)}</tbody>
          </table>
        </div>
        <nav className="flex items-center justify-between py-4" aria-label="Incident pages">
          <span className="text-sm text-slate-500">Showing {offset + 1}–{offset + incidents.length}</span>
          <div className="flex gap-2"><button className={buttonClass} type="button" onClick={() => changePage(Math.max(0, offset - PAGE_SIZE))} disabled={offset === 0 || loading}>Previous</button><button className={buttonClass} type="button" onClick={() => changePage(offset + PAGE_SIZE)} disabled={incidents.length < PAGE_SIZE || loading}>Next</button></div>
        </nav>
      </>}
    </section>
  );
}
