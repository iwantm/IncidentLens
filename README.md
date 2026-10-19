# IncidentLens

IncidentLens is a personal Kubernetes incident recorder. It connects alerts with nearby Kubernetes observations and keeps adding matching observations while an incident is open, helping reconstruct what happened.

## Current status

The project includes a demo app, a Go recorder API, SQLite persistence, and a namespace-scoped Kubernetes watcher for Events and Deployment pod-template changes. The recorder can open and resolve incidents, backfill the five minutes of observations before an alert, and attach new matching observations while the incident is open.

## Incident lifecycle API

Submit an event to `POST /api/v1/events` to open or resolve an incident. An `alert_fired` event opens the incident using its `incident_id`, `namespace`, `service`, `timestamp`, and `summary`. IncidentLens backfills matching observations from the previous five minutes. New matching Kubernetes observations are attached while the incident remains open.

Submit an event with `type: "alert_resolved"` and the same `incident_id` to close it. Other event types are saved to the specified incident timeline.

Retrieve lifecycle state and the ordered timeline with:

```text
GET /api/v1/incidents/{incidentID}
GET /api/v1/incidents/{incidentID}/events
```

Events and observations are stored separately: observations preserve what the watcher saw, while incident events provide the incident-specific timeline. A single observation may be associated with more than one matching open incident.
