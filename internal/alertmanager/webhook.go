// Package alertmanager translates Alertmanager webhook notifications into the
// recorder's provider-independent incident event model.
package alertmanager

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iwantm/IncidentLens/internal/events"
)

const WebhookVersion = "4"

var ErrInvalidPayload = errors.New("invalid Alertmanager webhook payload")

type Webhook struct {
	Version           string            `json:"version"`
	GroupKey          string            `json:"groupKey"`
	TruncatedAlerts   int               `json:"truncatedAlerts"`
	Status            string            `json:"status"`
	Receiver          string            `json:"receiver"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Alerts            []Alert           `json:"alerts"`
}

type Alert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
}

// Events validates and converts each alert in the webhook batch. Each alert
// fingerprint plus startsAt identifies one firing lifecycle, making retries
// stable while allowing a later firing of the same alert to open a new incident.
func (w Webhook) Events(observedAt time.Time) ([]events.Event, error) {
	if w.Version != WebhookVersion {
		return nil, fmt.Errorf("%w: unsupported version %q", ErrInvalidPayload, w.Version)
	}
	if len(w.Alerts) == 0 {
		return nil, fmt.Errorf("%w: alerts must not be empty", ErrInvalidPayload)
	}
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}

	converted := make([]events.Event, 0, len(w.Alerts))
	for i, alert := range w.Alerts {
		event, err := w.event(alert, observedAt)
		if err != nil {
			return nil, fmt.Errorf("%w: alert %d: %v", ErrInvalidPayload, i, err)
		}
		converted = append(converted, event)
	}
	return converted, nil
}

func (w Webhook) event(alert Alert, observedAt time.Time) (events.Event, error) {
	if alert.Status != "firing" && alert.Status != "resolved" {
		return events.Event{}, fmt.Errorf("unsupported status %q", alert.Status)
	}
	if strings.TrimSpace(alert.Fingerprint) == "" {
		return events.Event{}, errors.New("fingerprint is required")
	}
	if alert.StartsAt.IsZero() {
		return events.Event{}, errors.New("startsAt is required")
	}

	alertName := strings.TrimSpace(alert.Labels["alertname"])
	if alertName == "" {
		return events.Event{}, errors.New("labels.alertname is required")
	}

	incidentID := stableID("am-inc-", alert.Fingerprint+"\x00"+alert.StartsAt.UTC().Format(time.RFC3339Nano))
	eventType := "alert_fired"
	timestamp := alert.StartsAt
	summary := strings.TrimSpace(alert.Annotations["summary"])
	if summary == "" {
		summary = alertName
	}
	if alert.Status == "resolved" {
		eventType = "alert_resolved"
		if !alert.EndsAt.IsZero() {
			timestamp = alert.EndsAt
		}
	}

	service := firstNonEmpty(alert.Labels["service"], alert.Labels["app"], alert.Labels["job"])
	namespace := strings.TrimSpace(alert.Labels["namespace"])
	evidence := map[string]any{
		"alertmanager": map[string]any{
			"receiver":         w.Receiver,
			"group_key":        w.GroupKey,
			"group_labels":     w.GroupLabels,
			"external_url":     w.ExternalURL,
			"fingerprint":      alert.Fingerprint,
			"generator_url":    alert.GeneratorURL,
			"labels":           alert.Labels,
			"annotations":      alert.Annotations,
			"truncated_alerts": w.TruncatedAlerts,
		},
	}
	if description := strings.TrimSpace(alert.Annotations["description"]); description != "" {
		evidence["description"] = description
	}

	return events.Event{
		ID:         stableID("am-evt-", alert.Fingerprint+"\x00"+alert.StartsAt.UTC().Format(time.RFC3339Nano)+"\x00"+alert.Status),
		IncidentID: incidentID,
		Timestamp:  timestamp.UTC(),
		ObservedAt: observedAt.UTC(),
		Type:       eventType,
		Namespace:  namespace,
		Service:    service,
		Summary:    summary,
		Source:     "alertmanager",
		Evidence:   evidence,
	}, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func stableID(prefix, value string) string {
	sum := sha256.Sum256([]byte(value))
	hash := hex.EncodeToString(sum[:])
	maxHashLength := 64 - len(prefix) // Keep IDs within the model's MySQL column width.
	if len(hash) > maxHashLength {
		hash = hash[:maxHashLength]
	}
	return prefix + hash
}
