package events

import (
	"reflect"
	"testing"
	"time"
)

func TestEventFromObservation(t *testing.T) {
	observation := Observation{
		Source:        "kubernetes_watch",
		SourceID:      "deployment-uid-1",
		SourceVersion: "7",
		Timestamp:     time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC),
		ObservedAt:    time.Date(2026, 10, 19, 12, 0, 1, 0, time.UTC),
		Type:          "deployment_changed",
		Namespace:     "incident-lens",
		Service:       "demo-app",
		Summary:       "Deployment changed",
		Object:        ObjectReference{APIVersion: "apps/v1", Kind: "Deployment", Name: "demo-app", UID: "deployment-uid-1"},
		Evidence:      map[string]any{"revision": "7"},
	}

	got := EventFromObservation("incident-1", observation)
	if got.IncidentID != "incident-1" || got.Type != observation.Type || got.Timestamp != observation.Timestamp {
		t.Fatalf("converted event has incorrect identity or timestamps: %#v", got)
	}
	if got.Object != observation.Object {
		t.Errorf("converted object = %#v, want %#v", got.Object, observation.Object)
	}
	wantEvidence := map[string]any{
		"revision":                   "7",
		"observation_source_id":      observation.SourceID,
		"observation_source_version": observation.SourceVersion,
	}
	if !reflect.DeepEqual(got.Evidence, wantEvidence) {
		t.Errorf("converted evidence = %#v, want %#v", got.Evidence, wantEvidence)
	}

	if again := EventFromObservation("incident-1", observation); again.ID != got.ID {
		t.Errorf("same observation produced unstable event IDs: %q and %q", got.ID, again.ID)
	}
	if otherIncident := EventFromObservation("incident-2", observation); otherIncident.ID == got.ID {
		t.Errorf("same observation attached to different incidents produced same ID %q", got.ID)
	}
	if observation.Evidence["observation_source_id"] != nil {
		t.Errorf("conversion mutated source observation evidence: %#v", observation.Evidence)
	}
}
