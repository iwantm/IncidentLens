package investigation

import (
	"reflect"
	"testing"
	"time"

	"github.com/iwantm/IncidentLens/internal/events"
)

func TestAnalyzeMatchesDeploymentChangesInFiveMinuteLookback(t *testing.T) {
	startedAt := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	incident := events.Incident{ID: "incident-1", Namespace: "incident-lens", Service: "demo-app", StartedAt: startedAt}
	timeline := []events.Event{
		analysisEvent("deployment-z", "deployment_changed", startedAt, "incident-lens", "demo-app", nil),
		analysisEvent("deployment-a", "deployment_changed", startedAt.Add(-5*time.Minute), "incident-lens", "", nil),
		analysisEvent("too-old", "deployment_changed", startedAt.Add(-5*time.Minute-time.Second), "incident-lens", "demo-app", nil),
		analysisEvent("after-start", "deployment_changed", startedAt.Add(time.Second), "incident-lens", "demo-app", nil),
		analysisEvent("other-service", "deployment_changed", startedAt.Add(-time.Minute), "incident-lens", "other-app", nil),
		analysisEvent("other-namespace", "deployment_changed", startedAt.Add(-time.Minute), "other", "demo-app", nil),
		analysisEvent("wrong-type", "kubernetes_event", startedAt.Add(-time.Minute), "incident-lens", "demo-app", nil),
		analysisEvent("", "deployment_changed", startedAt.Add(-time.Minute), "incident-lens", "demo-app", nil),
	}

	got := Analyze(incident, timeline, startedAt.Add(time.Hour))
	if len(got.Findings) != 1 {
		t.Fatalf("findings = %#v, want one deployment finding", got.Findings)
	}
	finding := got.Findings[0]
	if finding.RuleID != "deployment_change_before_alert" {
		t.Errorf("rule ID = %q", finding.RuleID)
	}
	if !reflect.DeepEqual(finding.EvidenceEventIDs, []string{"deployment-a", "deployment-z"}) {
		t.Errorf("evidence IDs = %#v, want stable sorted IDs", finding.EvidenceEventIDs)
	}
}

func TestAnalyzeMatchesWarningEventsDuringResolvedIncident(t *testing.T) {
	startedAt := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	resolvedAt := startedAt.Add(10 * time.Minute)
	incident := events.Incident{
		ID: "incident-1", Namespace: "incident-lens", Service: "demo-app",
		StartedAt: startedAt, ResolvedAt: &resolvedAt,
	}
	timeline := []events.Event{
		analysisEvent("warning-z", "kubernetes_event", resolvedAt, "incident-lens", "other-app", map[string]any{"event_type": "Warning", "reason": "NewReason"}),
		analysisEvent("warning-a", "kubernetes_event", startedAt, "incident-lens", "demo-app", map[string]any{"reason": "BackOff"}),
		analysisEvent("explicit-normal", "kubernetes_event", startedAt.Add(time.Minute), "incident-lens", "demo-app", map[string]any{"event_type": "Normal", "reason": "BackOff"}),
		analysisEvent("not-allowlisted", "kubernetes_event", startedAt.Add(time.Minute), "incident-lens", "demo-app", map[string]any{"reason": "Scheduled"}),
		analysisEvent("before-incident", "kubernetes_event", startedAt.Add(-time.Second), "incident-lens", "demo-app", map[string]any{"event_type": "Warning"}),
		analysisEvent("after-resolution", "kubernetes_event", resolvedAt.Add(time.Second), "incident-lens", "demo-app", map[string]any{"event_type": "Warning"}),
		analysisEvent("wrong-namespace", "kubernetes_event", startedAt.Add(time.Minute), "other", "demo-app", map[string]any{"event_type": "Warning"}),
		analysisEvent("wrong-type", "deployment_changed", startedAt.Add(time.Minute), "incident-lens", "demo-app", map[string]any{"event_type": "Warning"}),
		analysisEvent("", "kubernetes_event", startedAt.Add(time.Minute), "incident-lens", "demo-app", map[string]any{"event_type": "Warning"}),
		analysisEvent("no-evidence", "kubernetes_event", startedAt.Add(time.Minute), "incident-lens", "demo-app", nil),
	}

	got := Analyze(incident, timeline, startedAt.Add(24*time.Hour))
	if len(got.Findings) != 1 {
		t.Fatalf("findings = %#v, want one Kubernetes warning finding", got.Findings)
	}
	finding := got.Findings[0]
	if finding.RuleID != "kubernetes_warning_during_incident" {
		t.Errorf("rule ID = %q", finding.RuleID)
	}
	if !reflect.DeepEqual(finding.EvidenceEventIDs, []string{"warning-a", "warning-z"}) {
		t.Errorf("evidence IDs = %#v, want stable sorted IDs", finding.EvidenceEventIDs)
	}
}

func TestAnalyzeUsesCurrentTimeForOpenIncident(t *testing.T) {
	startedAt := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	now := startedAt.Add(5 * time.Minute)
	incident := events.Incident{ID: "incident-1", Namespace: "incident-lens", StartedAt: startedAt}
	timeline := []events.Event{
		analysisEvent("at-now", "kubernetes_event", now, "incident-lens", "", map[string]any{"event_type": "Warning"}),
		analysisEvent("after-now", "kubernetes_event", now.Add(time.Nanosecond), "incident-lens", "", map[string]any{"event_type": "Warning"}),
	}

	got := Analyze(incident, timeline, now)
	if len(got.Findings) != 1 || !reflect.DeepEqual(got.Findings[0].EvidenceEventIDs, []string{"at-now"}) {
		t.Fatalf("analysis = %#v, want only warning at now", got)
	}
}

func TestAnalyzeReturnsNoFindingsForUnmatchedTimeline(t *testing.T) {
	startedAt := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	incident := events.Incident{ID: "incident-1", Namespace: "incident-lens", Service: "demo-app", StartedAt: startedAt}

	got := Analyze(incident, nil, startedAt)
	if len(got.Findings) != 0 {
		t.Fatalf("findings = %#v, want none", got.Findings)
	}
}

func analysisEvent(id, eventType string, timestamp time.Time, namespace, service string, evidence map[string]any) events.Event {
	return events.Event{
		ID: id, Type: eventType, Timestamp: timestamp,
		Namespace: namespace, Service: service, Evidence: evidence,
	}
}
