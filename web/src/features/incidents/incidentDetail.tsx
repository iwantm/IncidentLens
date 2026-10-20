import { useEffect, useState } from "react";
import { getIncident, type Incident } from "../../api/incident";

export function IncidentDetailPage({ incidentId }: { incidentId: string }) {
  const [incident, setIncident] = useState<Incident | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    getIncident(incidentId)
      .then(setIncident)
      .catch((err: unknown) => {
        setError(
          err instanceof Error ? err.message : "Could not load incident",
        );
      });
  }, [incidentId]);

  if (error) return <p role="alert">{error}</p>;
  if (!incident) return <p>Loading incident…</p>;

  return (
    <div>
      <table>
        <thead>
          <tr>
            <th>Field</th>
            <th>Value</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>ID</td>
            <td>{incident.id}</td>
          </tr>
          <tr>
            <td>Namespace</td>
            <td>{incident.namespace}</td>
          </tr>
          <tr>
            <td>Service</td>
            <td>{incident.service}</td>
          </tr>
          <tr>
            <td>Status</td>
            <td>{incident.status}</td>
          </tr>
          <tr>
            <td>Start Time</td>
            <td>{incident.started_at}</td>
          </tr>
          <tr>
            <td>Resolution Time</td>
            <td>{incident.resolved_at}</td>
          </tr>
        </tbody>
      </table>
    </div>
  );
}
