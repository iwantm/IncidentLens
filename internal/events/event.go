package events

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

type Event struct {
	ID         string    `json:"id" gorm:"primaryKey;size:64"`
	IncidentID string    `json:"incident_id" gorm:"not null;index:idx_incident_time,priority:1;size:64"`
	Timestamp  time.Time `json:"timestamp" gorm:"not null;index:idx_incident_time,priority:2"`
	ObservedAt time.Time `json:"observed_at" gorm:"not null"`

	Type      string `json:"type" gorm:"not null;size:64"`
	Namespace string `json:"namespace" gorm:"index;size:255"`
	Service   string `json:"service" gorm:"index;size:255"`
	Summary   string `json:"summary" gorm:"not null;type:text"`
	Source    string `json:"source" gorm:"not null;size:64"`

	Object   ObjectReference `json:"object" gorm:"embedded;embeddedPrefix:object_"`
	Evidence map[string]any  `json:"evidence,omitempty" gorm:"serializer:json;type:text"`
}

type ObjectReference struct {
	APIVersion string `json:"api_version" gorm:"size:128"`
	Kind       string `json:"kind" gorm:"size:64"`
	Name       string `json:"name" gorm:"size:255"`
	UID        string `json:"uid" gorm:"size:128;index"`
}

func EventFromObservation(
	incidentID string,
	observation Observation,
) Event {
	idInput := incidentID + "\x00" +
		observation.Source + "\x00" +
		observation.SourceID + "\x00" +
		observation.SourceVersion

	idHash := sha256.Sum256([]byte(idInput))

	evidence := make(map[string]any, len(observation.Evidence)+2)
	for key, value := range observation.Evidence {
		evidence[key] = value
	}
	evidence["observation_source_id"] = observation.SourceID
	evidence["observation_source_version"] = observation.SourceVersion

	return Event{
		ID:         hex.EncodeToString(idHash[:]),
		IncidentID: incidentID,
		Timestamp:  observation.Timestamp,
		ObservedAt: observation.ObservedAt,
		Type:       observation.Type,
		Namespace:  observation.Namespace,
		Service:    observation.Service,
		Summary:    observation.Summary,
		Source:     observation.Source,
		Object:     observation.Object,
		Evidence:   evidence,
	}
}
