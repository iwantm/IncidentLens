package gorm

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/iwantm/IncidentLens/internal/events"
)

func newTestEventStore(t *testing.T) *EventStore {
	t.Helper()

	db, err := Open(filepath.Join(t.TempDir(), "incidents.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get underlying database: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})

	return NewEventStore(db)
}

func TestResolveIncidentUnknownIncident(t *testing.T) {
	store := newTestEventStore(t)
	resolution := testResolution("missing-incident", "resolution-missing", time.Now().UTC())

	err := store.ResolveIncident(context.Background(), "missing-incident", &resolution)
	if !errors.Is(err, events.ErrIncidentNotFound) {
		t.Fatalf("ResolveIncident() error = %v, want ErrIncidentNotFound", err)
	}
}

func TestResolveIncidentClosesAndRejectsRepeatedResolution(t *testing.T) {
	ctx := context.Background()
	store := newTestEventStore(t)
	startedAt := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	incident := events.Incident{
		ID:        "incident-1",
		Namespace: "incident-lens",
		Service:   "demo-app",
		Summary:   "Demo app errors",
		Status:    events.IncidentStatusOpen,
		StartedAt: startedAt,
	}
	if err := store.db.Create(&incident).Error; err != nil {
		t.Fatalf("create incident: %v", err)
	}

	resolvedAt := startedAt.Add(5 * time.Minute)
	resolution := testResolution(incident.ID, "resolution-1", resolvedAt)
	if err := store.ResolveIncident(ctx, incident.ID, &resolution); err != nil {
		t.Fatalf("ResolveIncident() error = %v, want nil", err)
	}

	stored, err := store.GetIncident(ctx, incident.ID)
	if err != nil {
		t.Fatalf("GetIncident() error = %v", err)
	}
	if stored.Status != events.IncidentStatusClosed {
		t.Errorf("incident status = %q, want %q", stored.Status, events.IncidentStatusClosed)
	}
	if stored.ResolvedAt == nil || !stored.ResolvedAt.Equal(resolvedAt) {
		t.Errorf("incident resolved_at = %v, want %v", stored.ResolvedAt, resolvedAt)
	}

	for _, repeatedAt := range []time.Time{resolvedAt, resolvedAt.Add(time.Minute)} {
		repeated := testResolution(incident.ID, "resolution-repeated-"+repeatedAt.Format(time.RFC3339), repeatedAt)
		err := store.ResolveIncident(ctx, incident.ID, &repeated)
		if !errors.Is(err, events.ErrIncidentAlreadyClosed) {
			t.Errorf("repeated ResolveIncident() error = %v, want ErrIncidentAlreadyClosed", err)
		}
	}

	timeline, err := store.ListByIncident(ctx, incident.ID)
	if err != nil {
		t.Fatalf("ListByIncident() error = %v", err)
	}
	if len(timeline) != 1 {
		t.Fatalf("timeline has %d events, want only the first resolution event", len(timeline))
	}
	if timeline[0].ID != resolution.ID {
		t.Errorf("timeline event ID = %q, want %q", timeline[0].ID, resolution.ID)
	}
}

func TestOpenIncidentBackfillsMatchingObservations(t *testing.T) {
	ctx := context.Background()
	store := newTestEventStore(t)
	startedAt := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	observations := []events.Observation{
		testObservation("in-window", "incident-lens", "demo-app", startedAt.Add(-4*time.Minute)),
		testObservation("at-window-start", "incident-lens", "demo-app", startedAt.Add(-5*time.Minute)),
		testObservation("too-old", "incident-lens", "demo-app", startedAt.Add(-5*time.Minute-time.Second)),
		testObservation("other-service", "incident-lens", "other-app", startedAt.Add(-time.Minute)),
		testObservation("other-namespace", "other", "demo-app", startedAt.Add(-time.Minute)),
	}
	for i := range observations {
		if err := store.UpsertObservation(ctx, &observations[i]); err != nil {
			t.Fatalf("UpsertObservation(%q): %v", observations[i].SourceID, err)
		}
	}

	incident := &events.Incident{
		ID: "incident-backfill", Namespace: "incident-lens", Service: "demo-app",
		Summary: "Demo app errors", Status: events.IncidentStatusOpen, StartedAt: startedAt,
	}
	alert := testResolution(incident.ID, "alert-fired", startedAt)
	alert.Type = "alert_fired"
	alert.Summary = "Demo app errors started"
	if err := store.OpenIncident(ctx, incident, &alert, 5*time.Minute); err != nil {
		t.Fatalf("OpenIncident() error = %v", err)
	}

	timeline, err := store.ListByIncident(ctx, incident.ID)
	if err != nil {
		t.Fatalf("ListByIncident() error = %v", err)
	}
	if len(timeline) != 3 {
		t.Fatalf("timeline has %d events, want alert plus 2 matching observations", len(timeline))
	}
	gotIDs := map[string]bool{}
	for _, event := range timeline {
		gotIDs[event.ID] = true
	}
	if !gotIDs[alert.ID] {
		t.Errorf("timeline is missing alert event %q", alert.ID)
	}
	for _, sourceID := range []string{"in-window", "at-window-start"} {
		converted := events.EventFromObservation(incident.ID, findObservation(t, observations, sourceID))
		if !gotIDs[converted.ID] {
			t.Errorf("timeline is missing backfilled observation %q", sourceID)
		}
	}
}

func TestUpsertObservationTracksOnlyMatchingOpenIncidents(t *testing.T) {
	ctx := context.Background()
	store := newTestEventStore(t)
	observedAt := time.Date(2026, 10, 19, 12, 5, 0, 0, time.UTC)
	incidents := []events.Incident{
		{ID: "open-match", Namespace: "incident-lens", Service: "demo-app", Summary: "open", Status: events.IncidentStatusOpen, StartedAt: observedAt.Add(-time.Minute)},
		{ID: "open-other-service", Namespace: "incident-lens", Service: "other-app", Summary: "open other", Status: events.IncidentStatusOpen, StartedAt: observedAt.Add(-time.Minute)},
		{ID: "open-other-namespace", Namespace: "other", Service: "demo-app", Summary: "open other namespace", Status: events.IncidentStatusOpen, StartedAt: observedAt.Add(-time.Minute)},
		{ID: "open-starts-later", Namespace: "incident-lens", Service: "demo-app", Summary: "future", Status: events.IncidentStatusOpen, StartedAt: observedAt.Add(time.Minute)},
		{ID: "closed-match", Namespace: "incident-lens", Service: "demo-app", Summary: "closed", Status: events.IncidentStatusClosed, StartedAt: observedAt.Add(-time.Minute)},
	}
	if err := store.db.Create(&incidents).Error; err != nil {
		t.Fatalf("create incidents: %v", err)
	}

	observation := testObservation("live-observation", "incident-lens", "demo-app", observedAt)
	if err := store.UpsertObservation(ctx, &observation); err != nil {
		t.Fatalf("UpsertObservation() error = %v", err)
	}
	// Re-delivery of the same source version must not duplicate its timeline event.
	if err := store.UpsertObservation(ctx, &observation); err != nil {
		t.Fatalf("repeat UpsertObservation() error = %v", err)
	}

	for _, incidentID := range []string{"open-match", "open-other-service", "open-other-namespace", "open-starts-later", "closed-match"} {
		timeline, err := store.ListByIncident(ctx, incidentID)
		if err != nil {
			t.Fatalf("ListByIncident(%q): %v", incidentID, err)
		}
		want := 0
		if incidentID == "open-match" {
			want = 1
		}
		if len(timeline) != want {
			t.Errorf("incident %q has %d events, want %d", incidentID, len(timeline), want)
		}
	}
}

func TestCreateEventsIgnoresDuplicateIDs(t *testing.T) {
	store := newTestEventStore(t)
	event := testResolution("incident-1", "same-event-id", time.Now().UTC())
	if err := store.CreateEvents(context.Background(), []events.Event{event, event}); err != nil {
		t.Fatalf("CreateEvents() error = %v", err)
	}

	timeline, err := store.ListByIncident(context.Background(), event.IncidentID)
	if err != nil {
		t.Fatalf("ListByIncident() error = %v", err)
	}
	if len(timeline) != 1 {
		t.Fatalf("timeline has %d events, want 1", len(timeline))
	}
}

func TestListObservationsFiltersByTimeNamespaceAndService(t *testing.T) {
	ctx := context.Background()
	store := newTestEventStore(t)
	baseTime := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	observations := []events.Observation{
		testObservation("match", "incident-lens", "demo-app", baseTime),
		testObservation("other-service", "incident-lens", "other-app", baseTime.Add(time.Minute)),
		testObservation("other-namespace", "other", "demo-app", baseTime.Add(time.Minute)),
		testObservation("too-early", "incident-lens", "demo-app", baseTime.Add(-time.Second)),
		testObservation("too-late", "incident-lens", "demo-app", baseTime.Add(2*time.Minute)),
	}
	for i := range observations {
		if err := store.UpsertObservation(ctx, &observations[i]); err != nil {
			t.Fatalf("UpsertObservation(%q): %v", observations[i].SourceID, err)
		}
	}

	allInWindow, err := store.ListObservations(ctx, baseTime, baseTime.Add(time.Minute))
	if err != nil {
		t.Fatalf("ListObservations() error = %v", err)
	}
	if len(allInWindow) != 3 {
		t.Errorf("ListObservations() returned %d rows, want 3", len(allInWindow))
	}

	filtered, err := store.ListObservationsBetween(ctx, baseTime, baseTime.Add(time.Minute), "incident-lens", "demo-app")
	if err != nil {
		t.Fatalf("ListObservationsBetween() error = %v", err)
	}
	if len(filtered) != 1 || filtered[0].SourceID != "match" {
		t.Errorf("filtered observations = %#v, want only the matching observation", filtered)
	}
}

func testObservation(sourceID, namespace, service string, timestamp time.Time) events.Observation {
	return events.Observation{
		Source: "test", SourceID: sourceID, SourceVersion: "1",
		Timestamp: timestamp, ObservedAt: timestamp.Add(time.Second), Type: "deployment_changed",
		Namespace: namespace, Service: service, Summary: "Test observation",
	}
}

func findObservation(t *testing.T, observations []events.Observation, sourceID string) events.Observation {
	t.Helper()
	for _, observation := range observations {
		if observation.SourceID == sourceID {
			return observation
		}
	}
	t.Fatalf("observation %q not found", sourceID)
	return events.Observation{}
}

func testResolution(incidentID, eventID string, timestamp time.Time) events.Event {
	return events.Event{
		ID:         eventID,
		IncidentID: incidentID,
		Timestamp:  timestamp,
		ObservedAt: timestamp.Add(time.Second),
		Type:       "alert_resolved",
		Namespace:  "incident-lens",
		Service:    "demo-app",
		Summary:    "Demo app errors resolved",
		Source:     "test",
	}
}
