import { Link, Navigate, Route, Routes, useParams } from "react-router";
import { IncidentListPage } from "../features/incidents/IncidentList";

import { IncidentDetailPage } from "../features/incidents/incidentDetail";

function IncidentDetailRoute() {
  const { incidentID } = useParams();

  if (!incidentID) {
    return <Navigate to="/incidents" replace />;
  }

  return <IncidentDetailPage incidentId={incidentID} />;
}

function App() {
  return (
    <main>
      <header>
        <Link to="/incidents">IncidentLens</Link>
      </header>

      <Routes>
        <Route path="/" element={<Navigate to="/incidents" replace />} />
        <Route path="/incidents" element={<IncidentListPage />} />
        <Route
          path="/incidents/:incidentID"
          element={<IncidentDetailRoute />}
        />
      </Routes>
    </main>
  );
}

export default App;
