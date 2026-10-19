package events

import (
	"errors"
	"time"
)

var (
	ErrIncidentNotFound      = errors.New("incident not found")
	ErrIncidentAlreadyClosed = errors.New("incident is already closed")
)

const (
	IncidentStatusOpen   = "open"
	IncidentStatusClosed = "closed"
)

type Incident struct {
	ID         string     `json:"id" gorm:"primaryKey;size:64"`
	Namespace  string     `json:"namespace" gorm:"not null;size:255;index:idx_incident_status_namespace,priority:1"`
	Service    string     `json:"service,omitempty" gorm:"size:255"`
	Summary    string     `json:"summary" gorm:"not null;type:text"`
	Status     string     `json:"status" gorm:"not null;size:16;index:idx_incident_status_namespace,priority:2"`
	StartedAt  time.Time  `json:"started_at" gorm:"not null;index"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}
