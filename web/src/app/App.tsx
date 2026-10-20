import { Link, Navigate, Route, Routes, useParams } from "react-router";
import { IncidentListPage } from "../features/incidents/IncidentList";
import { IncidentDetailPage } from "../features/incidents/incidentDetail";

function IncidentDetailRoute() {
  const { incidentID } = useParams();
  if (!incidentID) return <Navigate to="/incidents" replace />;
  return <IncidentDetailPage incidentId={incidentID} />;
}

function NotFoundPage() {
  return (
    <section className="mx-auto w-full max-w-6xl px-4 py-12 sm:px-7">
      <div className="rounded-xl border border-dashed border-slate-300 bg-white px-6 py-16 text-center">
        <p className="mb-3 font-mono text-xs tracking-[0.14em] text-slate-500">404</p>
        <h1 className="mb-2 text-3xl font-semibold tracking-tight">Page not found</h1>
        <p className="mb-6 text-slate-500">The page you requested does not exist.</p>
        <Link className="inline-flex rounded-md bg-teal px-4 py-2.5 font-semibold text-white hover:bg-teal-dark" to="/incidents">Go to incidents</Link>
      </div>
    </section>
  );
}

function App() {
  return (
    <div className="flex min-h-screen flex-col">
      <header className="border-b border-slate-200 bg-white">
        <div className="mx-auto flex h-16 w-full max-w-6xl items-center gap-10 px-4 sm:px-7">
          <Link className="flex items-center gap-2.5 text-base font-bold tracking-tight no-underline" to="/incidents">
            <span className="grid size-8 place-items-center rounded-lg bg-teal font-mono text-xs text-white">IL</span>
            <span>Incident<span className="font-normal text-slate-500">Lens</span></span>
          </Link>
          <nav aria-label="Main navigation" className="flex h-full items-center">
            <Link className="flex h-full items-center border-b-2 border-teal pt-0.5 font-semibold text-teal-dark no-underline" to="/incidents">Incidents</Link>
          </nav>
          <span className="ml-auto font-mono text-[.65rem] tracking-[.11em] text-slate-400 max-sm:hidden">PERSONAL CONSOLE</span>
        </div>
      </header>
      <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-8 sm:px-7 sm:py-11">
        <Routes>
          <Route path="/" element={<Navigate to="/incidents" replace />} />
          <Route path="/incidents" element={<IncidentListPage />} />
          <Route path="/incidents/:incidentID" element={<IncidentDetailRoute />} />
          <Route path="*" element={<NotFoundPage />} />
        </Routes>
      </main>
      <footer className="flex justify-between gap-3 border-t border-slate-200 bg-white px-4 py-4 text-xs text-slate-400 sm:px-7">
        <div className="mx-auto flex w-full max-w-6xl justify-between">
          <span className="font-bold text-slate-500">IncidentLens</span>
          <span>Incident evidence, in one timeline.</span>
        </div>
      </footer>
    </div>
  );
}

export default App;
