package alertmanager

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFiringFixtureMapsIntoStableIncidentEvent(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "firing.json"))
	if err != nil {
		t.Fatal(err)
	}
	var webhook Webhook
	if err := json.Unmarshal(data, &webhook); err != nil {
		t.Fatal(err)
	}
	observedAt := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	first, err := webhook.Events(observedAt)
	if err != nil {
		t.Fatal(err)
	}
	second, err := webhook.Events(observedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("converted event counts = %d and %d, want 1", len(first), len(second))
	}
	a, b := first[0], second[0]
	if a.ID != b.ID || a.IncidentID != b.IncidentID {
		t.Fatalf("retry identity changed: first (%q, %q), second (%q, %q)", a.ID, a.IncidentID, b.ID, b.IncidentID)
	}
	if a.Type != "alert_fired" || a.Source != "alertmanager" || a.Namespace != "incident-lens" || a.Service != "demo-app" {
		t.Fatalf("unexpected normalized event: %#v", a)
	}
	if a.Timestamp.Format(time.RFC3339) != "2026-10-20T09:58:00Z" || a.Summary != "Demo app is returning errors" {
		t.Fatalf("unexpected alert fields: timestamp=%s summary=%q", a.Timestamp, a.Summary)
	}
}

func TestResolvedNotificationKeepsIncidentIdentity(t *testing.T) {
	startedAt := time.Date(2026, 10, 20, 9, 58, 0, 0, time.UTC)
	endedAt := startedAt.Add(3 * time.Minute)
	webhook := Webhook{Version: WebhookVersion, Alerts: []Alert{{
		Status: "resolved", Fingerprint: "stable-fingerprint", StartsAt: startedAt, EndsAt: endedAt,
		Labels: map[string]string{"alertname": "DemoAppErrors"},
	}}}
	events, err := webhook.Events(endedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if events[0].Type != "alert_resolved" || events[0].Timestamp != endedAt {
		t.Fatalf("resolved event = %#v", events[0])
	}
	firing := webhook
	firing.Alerts[0].Status = "firing"
	firing.Alerts[0].EndsAt = time.Time{}
	firingEvents, err := firing.Events(startedAt)
	if err != nil {
		t.Fatal(err)
	}
	if events[0].IncidentID != firingEvents[0].IncidentID || events[0].ID == firingEvents[0].ID {
		t.Fatalf("firing/resolution identity mismatch: resolved %#v firing %#v", events[0], firingEvents[0])
	}
}

func TestEventsRejectUnsupportedOrIncompletePayload(t *testing.T) {
	tests := []struct {
		name    string
		webhook Webhook
	}{
		{name: "unsupported version", webhook: Webhook{Version: "5", Alerts: []Alert{{}}}},
		{name: "empty batch", webhook: Webhook{Version: WebhookVersion}},
		{name: "missing fingerprint", webhook: Webhook{Version: WebhookVersion, Alerts: []Alert{{Status: "firing", StartsAt: time.Now(), Labels: map[string]string{"alertname": "Alert"}}}}},
		{name: "missing alert name", webhook: Webhook{Version: WebhookVersion, Alerts: []Alert{{Status: "firing", Fingerprint: "fp", StartsAt: time.Now()}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.webhook.Events(time.Now()); err == nil {
				t.Fatal("Events() error = nil, want validation error")
			}
		})
	}
}
