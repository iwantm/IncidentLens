import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, getJSON } from "./client";
import {
  getIncident,
  getIncidentAnalysis,
  listIncidentEvents,
  listIncidents,
} from "./incident";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("getIncidentAnalysis", () => {
  it("requests the incident analysis route and returns its findings", async () => {
    const payload = {
      findings: [{
        rule_id: "deployment_change_during_incident",
        title: "Deployment changed while the incident was active",
        summary: "Review this deployment change.",
        evidence_event_ids: ["deployment-event-1"],
      }],
    };
    const fetchMock = vi.fn(async () => new Response(JSON.stringify(payload), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(getIncidentAnalysis("incident/with space")).resolves.toEqual(payload);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/incidents/incident%2Fwith%20space/analysis",
      expect.objectContaining({ signal: undefined }),
    );
  });
});

describe("incident API routes", () => {
  it("lists incidents with provided filters and omits empty values", async () => {
    const fetchMock = vi.fn(async () => new Response("[]", { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(listIncidents({
      namespace: "incident-lens",
      service: "",
      status: "open",
      from: "2026-10-21T09:00:00Z",
      limit: 20,
      offset: 0,
    })).resolves.toEqual([]);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/incidents?namespace=incident-lens&status=open&from=2026-10-21T09%3A00%3A00Z&limit=20&offset=0",
      expect.any(Object),
    );
  });

  it("uses the unfiltered incident list route when no filters are set", async () => {
    const fetchMock = vi.fn(async () => new Response("[]", { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await listIncidents({ namespace: "", status: undefined });
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/incidents", expect.any(Object));
  });

  it("encodes IDs for incident and event routes", async () => {
    const fetchMock = vi.fn(async (_path: RequestInfo | URL) => new Response("{}", { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await getIncident("incident/1");
    await listIncidentEvents("incident/1");

    expect(fetchMock.mock.calls.map(([path]) => path)).toEqual([
      "/api/v1/incidents/incident%2F1",
      "/api/v1/incidents/incident%2F1/events",
    ]);
  });
});

describe("getJSON error handling", () => {
  it("uses a structured API error message and preserves the HTTP status", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(
      JSON.stringify({ error: "database unavailable" }),
      { status: 503, headers: { "Content-Type": "application/json" } },
    )));

    await expect(getJSON("/api/v1/incidents")).rejects.toMatchObject({
      name: "ApiError",
      message: "database unavailable",
      status: 503,
    } satisfies Partial<ApiError>);
  });

  it("falls back to response text for a non-JSON API error", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("upstream unavailable", { status: 502 })));

    await expect(getJSON("/api/v1/incidents")).rejects.toMatchObject({
      message: "upstream unavailable",
      status: 502,
    });
  });
});
