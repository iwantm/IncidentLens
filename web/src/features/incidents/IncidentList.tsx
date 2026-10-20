import { useEffect, useState } from "react";
import { Link } from "react-router";
import { listIncidents, type Incident } from "../../api/incident";

export function IncidentListPage() {
  const [incidents, setIncidents] = useState<Incident[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    listIncidents({ limit: 50 })
      .then(setIncidents)
      .catch((err: unknown) => {
        setError(
          err instanceof Error ? err.message : "Could not load incidents",
        );
      })
      .finally(() => setLoading(false));
  }, []);

  if (loading) return <p>Loading incidents…</p>;
  if (error) return <p role="alert">{error}</p>;
  if (incidents.length === 0) return <p>No incidents found.</p>;

  return (
    <ul>
      {incidents.map((incident) => (
        <li key={incident.id}>
          <strong>{incident.summary}</strong>
          {" — "}
          {incident.namespace}
          {incident.service && ` / ${incident.service}`}
          {" — "}
          {incident.status}
          {" — "}
          {new Date(incident.started_at).toLocaleString()}
          {" — "}
          <Link to={`/incidents/${incident.id}`}>View Details</Link>
        </li>
      ))}
    </ul>
  );
}
