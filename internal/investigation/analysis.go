// Package investigation contains deterministic, evidence-backed incident analysis.
package investigation

import (
	"sort"
	"time"

	"github.com/iwantm/IncidentLens/internal/events"
)

// Analysis is a set of deterministic findings derived from an incident timeline.
type Analysis struct {
	Findings []Finding `json:"findings"`
}

// Finding describes a rule match and links back to the timeline evidence.
type Finding struct {
	RuleID           string   `json:"rule_id"`
	Title            string   `json:"title"`
	Summary          string   `json:"summary"`
	EvidenceEventIDs []string `json:"evidence_event_ids"`
}

const deploymentLookback = 5 * time.Minute

var warningReasons = map[string]struct{}{
	"BackOff":            {},
	"FailedScheduling":   {},
	"Unhealthy":          {},
	"Failed":             {},
	"FailedMount":        {},
	"FailedAttachVolume": {},
}

// Analyze looks for simple operational signals without inferring causality.
// Its output is stable for the same incident and timeline.
func Analyze(incident events.Incident, timeline []events.Event, now time.Time) Analysis {
	findings := make([]Finding, 0, 2)
	deploymentIDs := make([]string, 0)
	warningIDs := make([]string, 0)
	lookbackStart := incident.StartedAt.Add(-deploymentLookback)
	incidentEnd := now
	if incident.ResolvedAt != nil {
		incidentEnd = *incident.ResolvedAt
	}

	for _, event := range timeline {
		if event.ID == "" {
			continue
		}
		if event.Type == "deployment_changed" &&
			!event.Timestamp.Before(lookbackStart) && !event.Timestamp.After(incident.StartedAt) &&
			event.Namespace == incident.Namespace &&
			(incident.Service == "" || event.Service == "" || event.Service == incident.Service) {
			deploymentIDs = append(deploymentIDs, event.ID)
		}

		if event.Type != "kubernetes_event" || event.Namespace != incident.Namespace ||
			event.Timestamp.Before(incident.StartedAt) || event.Timestamp.After(incidentEnd) ||
			!isWarning(event.Evidence) {
			continue
		}
		warningIDs = append(warningIDs, event.ID)
	}

	if len(deploymentIDs) > 0 {
		findings = append(findings, Finding{
			RuleID: "deployment_change_before_alert",
			Title:  "Deployment changed shortly before the alert",
			Summary: "A matching Deployment pod template changed within five minutes before the incident started. " +
				"This timing may be relevant, but does not establish causality.",
			EvidenceEventIDs: stableIDs(deploymentIDs),
		})
	}
	if len(warningIDs) > 0 {
		findings = append(findings, Finding{
			RuleID:           "kubernetes_warning_during_incident",
			Title:            "Kubernetes warning observed during the incident",
			Summary:          "The recorder captured one or more Kubernetes warning events while the incident was active.",
			EvidenceEventIDs: stableIDs(warningIDs),
		})
	}
	return Analysis{Findings: findings}
}

func isWarning(evidence map[string]any) bool {
	if evidence == nil {
		return false
	}
	if eventType, ok := evidence["event_type"].(string); ok && eventType != "" {
		return eventType == "Warning"
	}
	reason, _ := evidence["reason"].(string)
	_, ok := warningReasons[reason]
	return ok
}

func stableIDs(ids []string) []string {
	sort.Strings(ids)
	return ids
}
