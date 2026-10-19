package gorm

import (
	"github.com/iwantm/IncidentLens/internal/events"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func Open(dbPath string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	if err := db.AutoMigrate(
		&events.Incident{},
		&events.Event{},
		&events.Observation{},
	); err != nil {
		return nil, err
	}

	return db, nil
}
