package events

import "time"

type Observation struct {
	ID uint64 `json:"-" gorm:"primaryKey;autoIncrement"`

	Source        string `json:"source" gorm:"not null;size:64;uniqueIndex:idx_observation_source_version,priority:1"`
	SourceID      string `json:"source_id" gorm:"not null;size:128;uniqueIndex:idx_observation_source_version,priority:2"`
	SourceVersion string `json:"source_version" gorm:"not null;size:128;uniqueIndex:idx_observation_source_version,priority:3"`

	Timestamp  time.Time `json:"timestamp" gorm:"not null;index"`
	ObservedAt time.Time `json:"observed_at" gorm:"not null"`

	Type      string `json:"type" gorm:"not null;size:64"`
	Namespace string `json:"namespace" gorm:"index;size:255"`
	Service   string `json:"service,omitempty" gorm:"index;size:255"`
	Summary   string `json:"summary" gorm:"not null;type:text"`

	Object   ObjectReference `json:"object" gorm:"embedded;embeddedPrefix:object_"`
	Evidence map[string]any  `json:"evidence,omitempty" gorm:"serializer:json;type:text"`
}
