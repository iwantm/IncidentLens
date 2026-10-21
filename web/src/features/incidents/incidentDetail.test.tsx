import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { IncidentDetailPage } from "./incidentDetail";

const incident = {
  id: "incident-1",
  namespace: "incident-lens",
  service: "demo-app",
  summary: "Demo app errors",
  status: "closed" as const,
  started_at: "2026-10-21T09:55:00Z",
  resolved_at: "2026-10-21T10:00:00Z",
  created_at: "2026-10-21T09:55:00Z",
  updated_at: "2026-10-21T10:00:00Z",
};

const timeline = [{
  id: "deployment-event-123456",
  incident_id: "incident-1",
  timestamp: "2026-10-21T09:59:43Z",
  observed_at: "2026-10-21T09:59:44Z",
  type: "deployment_changed",
  namespace: "incident-lens",
  service: "demo-app",
  summary: "Deployment pod template changed",
  source: "kubernetes_watch",
  object: { api_version: "apps/v1", kind: "Deployment", name: "demo-app", uid: "deployment-1" },
  evidence: { generation: 3 },
}];

const analysis = {
  findings: [{
    rule_id: "deployment_change_during_incident",
    title: "Deployment changed while the incident was active",
    summary: "Review this event alongside the timeline.",
    evidence_event_ids: ["deployment-event-123456"],
  }],
};

function jsonResponse(payload: unknown, status = 200) {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function renderPage() {
  return render(
    <MemoryRouter>
      <IncidentDetailPage incidentId="incident-1" />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
    configurable: true,
    value: vi.fn(),
  });
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("IncidentDetailPage investigation signals", () => {
  it("shows findings and links them to timeline evidence", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input);
      if (path.endsWith("/analysis")) return jsonResponse(analysis);
      if (path.endsWith("/events")) return jsonResponse(timeline);
      return jsonResponse(incident);
    });
    vi.stubGlobal("fetch", fetchMock);

    renderPage();

    expect(await screen.findByRole("heading", { name: analysis.findings[0].title })).toBeInTheDocument();
    expect(screen.getByText("Deployment pod template changed")).toBeInTheDocument();
    const evidenceButton = screen.getByRole("button", { name: "View evidence deployme" });
    fireEvent.click(evidenceButton);

    expect(HTMLElement.prototype.scrollIntoView).toHaveBeenCalledWith({ behavior: "smooth", block: "center" });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/incidents/incident-1/analysis",
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
  });

  it("keeps the timeline visible when analysis cannot be loaded", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input);
      if (path.endsWith("/analysis")) return jsonResponse({ error: "unavailable" }, 503);
      if (path.endsWith("/events")) return jsonResponse(timeline);
      return jsonResponse(incident);
    });
    vi.stubGlobal("fetch", fetchMock);

    renderPage();

    expect(await screen.findByText("Analysis is temporarily unavailable. The incident timeline is still available below.")).toBeInTheDocument();
    expect(screen.getByText("Deployment pod template changed")).toBeInTheDocument();
  });

  it("shows explicit empty states for an open incident without events or findings", async () => {
    const openIncident = {
      ...incident,
      summary: "",
      service: "",
      status: "open" as const,
      resolved_at: undefined,
    };
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input);
      if (path.endsWith("/analysis")) return jsonResponse({ findings: [] });
      if (path.endsWith("/events")) return jsonResponse([]);
      return jsonResponse(openIncident);
    });
    vi.stubGlobal("fetch", fetchMock);

    renderPage();

    expect(await screen.findByText("No matching signals were found in the captured timeline.")).toBeInTheDocument();
    expect(screen.getByText("No timeline events yet")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Untitled incident" })).toBeInTheDocument();
    expect(screen.getByText("Still open")).toBeInTheDocument();
    expect(screen.getByText("Ongoing")).toBeInTheDocument();
    expect(screen.getByText((_, element) => element?.textContent === "incident-lens / Service not specified")).toBeInTheDocument();
  });

  it("shows a not-found message and retries the incident load", async () => {
    let incidentRequests = 0;
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input);
      if (path.endsWith("/analysis")) return jsonResponse({ findings: [] });
      if (path.endsWith("/events")) return jsonResponse([]);
      incidentRequests += 1;
      return incidentRequests === 1
        ? jsonResponse({ error: "incident not found" }, 404)
        : jsonResponse({ ...incident, summary: "Incident loaded after retry" });
    });
    vi.stubGlobal("fetch", fetchMock);

    renderPage();

    expect(await screen.findByRole("alert")).toHaveTextContent("This incident could not be found.");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByRole("heading", { name: "Incident loaded after retry" })).toBeInTheDocument();
    expect(incidentRequests).toBe(2);
  });
});
