import { getJSON } from "./client";

export type IncidentStatus = "open" | "closed";

export interface Incident {
  id: string;
  namespace: string;
  service?: string;
  summary: string;
  status: IncidentStatus;
  started_at: string;
  resolved_at?: string;
  created_at: string;
  updated_at: string;
}

export interface IncidentEvent {
  id: string;
  incident_id: string;
  timestamp: string;
  observed_at: string;
  type: string;
  namespace: string;
  service: string;
  summary: string;
  source: string;
  object: {
    api_version: string;
    kind: string;
    name: string;
    uid: string;
  };
  evidence?: Record<string, unknown>;
}

export interface ListIncidentsOptions {
  namespace?: string;
  service?: string;
  status?: IncidentStatus;
  from?: string;
  to?: string;
  limit?: number;
  offset?: number;
}

export function listIncidents(
  options: ListIncidentsOptions = {},
): Promise<Incident[]> {
  const query = new URLSearchParams();

  for (const [key, value] of Object.entries(options)) {
    if (value !== undefined && value !== "") {
      query.set(key, String(value));
    }
  }

  const suffix = query.size > 0 ? `?${query.toString()}` : "";
  return getJSON<Incident[]>(`/api/v1/incidents${suffix}`);
}

export function getIncident(id: string): Promise<Incident> {
  return getJSON<Incident>(`/api/v1/incidents/${encodeURIComponent(id)}`);
}

export function listIncidentEvents(id: string): Promise<IncidentEvent[]> {
  return getJSON<IncidentEvent[]>(
    `/api/v1/incidents/${encodeURIComponent(id)}/events`,
  );
}
