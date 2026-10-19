package gorm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/iwantm/IncidentLens/internal/events"
	"github.com/iwantm/IncidentLens/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type EventStore struct {
	db *gorm.DB
}

func NewEventStore(db *gorm.DB) *EventStore {
	return &EventStore{db: db}
}

func (s *EventStore) Ping(ctx context.Context) (err error) {
	ctx, span := telemetry.StartSpan(ctx, "storage.database.ping")
	defer func() { telemetry.EndSpan(span, err) }()
	db, err := s.db.DB()
	if err != nil {
		return err
	}
	err = db.PingContext(ctx)
	return err
}

func (s *EventStore) Create(
	ctx context.Context,
	event *events.Event,
) (err error) {
	ctx, span := telemetry.StartSpan(ctx, "storage.event.create", attribute.String("incident.id", event.IncidentID), attribute.String("event.id", event.ID))
	defer func() { telemetry.EndSpan(span, err) }()
	return s.db.WithContext(ctx).Create(event).Error
}

func (s *EventStore) CreateEvents(
	ctx context.Context,
	items []events.Event,
) (err error) {
	ctx, span := telemetry.StartSpan(ctx, "storage.events.create_batch", attribute.Int("events.count", len(items)))
	defer func() { telemetry.EndSpan(span, err) }()
	if len(items) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&items).Error
	})
}

func (s *EventStore) OpenIncident(
	ctx context.Context,
	incident *events.Incident,
	alert *events.Event,
	backfillWindow time.Duration,
) (err error) {
	ctx, span := telemetry.StartSpan(ctx, "storage.incident.open", attribute.String("incident.id", incident.ID), attribute.String("incident.namespace", incident.Namespace))
	defer func() { telemetry.EndSpan(span, err) }()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(incident).Error; err != nil {
			return err
		}

		var stored events.Incident
		if err := tx.First(&stored, "id = ?", incident.ID).Error; err != nil {
			return err
		}
		if stored.Status != events.IncidentStatusOpen {
			return fmt.Errorf("%w: %s", events.ErrIncidentAlreadyClosed, stored.ID)
		}

		from := incident.StartedAt.Add(-backfillWindow)
		to := incident.StartedAt
		query := tx.Model(&events.Observation{}).
			Where("namespace = ? AND timestamp >= ? AND timestamp <= ?", incident.Namespace, from, to)
		if incident.Service != "" {
			query = query.Where("(service = ? OR service = '')", incident.Service)
		}

		observations := make([]events.Observation, 0)
		if err := query.Order("timestamp ASC").Find(&observations).Error; err != nil {
			return err
		}

		timeline := make([]events.Event, 0, len(observations)+1)
		timeline = append(timeline, *alert)
		for _, observation := range observations {
			timeline = append(timeline, events.EventFromObservation(incident.ID, observation))
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&timeline).Error
	})
}

func (s *EventStore) ResolveIncident(
	ctx context.Context,
	incidentID string,
	resolution *events.Event,
) (err error) {
	ctx, span := telemetry.StartSpan(ctx, "storage.incident.resolve", attribute.String("incident.id", incidentID), attribute.String("event.id", resolution.ID))
	defer func() { telemetry.EndSpan(span, err) }()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		resolvedAt := resolution.Timestamp
		result := tx.Model(&events.Incident{}).
			Where("id = ? AND status = ?", incidentID, events.IncidentStatusOpen).
			Updates(map[string]any{
				"status":      events.IncidentStatusClosed,
				"resolved_at": resolvedAt,
			})
		if result.Error != nil {
			return result.Error
		}

		if result.RowsAffected == 0 {
			var incident events.Incident
			if err := tx.Select("id").First(&incident, "id = ?", incidentID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return fmt.Errorf("%w: %s", events.ErrIncidentNotFound, incidentID)
				}
				return err
			}
			return fmt.Errorf("%w: %s", events.ErrIncidentAlreadyClosed, incidentID)
		}

		return tx.Create(resolution).Error
	})
}

func (s *EventStore) GetIncident(
	ctx context.Context,
	incidentID string,
) (incident events.Incident, err error) {
	ctx, span := telemetry.StartSpan(ctx, "storage.incident.get", attribute.String("incident.id", incidentID))
	defer func() { telemetry.EndSpan(span, err) }()
	err = s.db.WithContext(ctx).First(&incident, "id = ?", incidentID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return incident, fmt.Errorf("%w: %s", events.ErrIncidentNotFound, incidentID)
	}
	return incident, err
}

func (s *EventStore) ListByIncident(
	ctx context.Context,
	incidentID string,
) (listed []events.Event, err error) {
	ctx, span := telemetry.StartSpan(ctx, "storage.incident.events.list", attribute.String("incident.id", incidentID))
	defer func() { telemetry.EndSpan(span, err) }()
	listed = make([]events.Event, 0)

	err = s.db.WithContext(ctx).
		Where("incident_id = ?", incidentID).
		Order("timestamp ASC").
		Find(&listed).Error

	return listed, err
}

func (s *EventStore) UpsertObservation(
	ctx context.Context,
	observation *events.Observation,
) (err error) {
	ctx, span := telemetry.StartSpan(ctx, "storage.observation.upsert", attribute.String("observation.source", observation.Source), attribute.String("observation.source_id", observation.SourceID), attribute.String("observation.namespace", observation.Namespace))
	defer func() { telemetry.EndSpan(span, err) }()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "source"},
				{Name: "source_id"},
				{Name: "source_version"},
			},
			DoUpdates: clause.AssignmentColumns([]string{
				"timestamp",
				"observed_at",
				"type",
				"namespace",
				"service",
				"summary",
				"object_api_version",
				"object_kind",
				"object_name",
				"object_uid",
				"evidence",
			}),
		}).Create(observation).Error; err != nil {
			return err
		}

		var openIncidents []events.Incident
		if err := tx.Where(
			"namespace = ? AND status = ? AND started_at <= ?",
			observation.Namespace,
			events.IncidentStatusOpen,
			observation.Timestamp,
		).Find(&openIncidents).Error; err != nil {
			return err
		}

		for _, incident := range openIncidents {
			if incident.Service != "" && observation.Service != "" && incident.Service != observation.Service {
				continue
			}
			timelineEvent := events.EventFromObservation(incident.ID, *observation)
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&timelineEvent).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *EventStore) ListObservations(
	ctx context.Context,
	from time.Time,
	to time.Time,
) (observations []events.Observation, err error) {
	ctx, span := telemetry.StartSpan(ctx, "storage.observations.list")
	defer func() { telemetry.EndSpan(span, err) }()
	observations = make([]events.Observation, 0)

	err = s.db.WithContext(ctx).
		Where("timestamp >= ? AND timestamp <= ?", from, to).
		Order("timestamp ASC").
		Find(&observations).Error

	return observations, err
}

func (s *EventStore) ListObservationsBetween(
	ctx context.Context,
	from time.Time,
	to time.Time,
	namespace string,
	service string,
) (observations []events.Observation, err error) {
	ctx, span := telemetry.StartSpan(ctx, "storage.observations.list_between", attribute.String("observation.namespace", namespace), attribute.String("observation.service", service))
	defer func() { telemetry.EndSpan(span, err) }()
	observations = make([]events.Observation, 0)

	query := s.db.WithContext(ctx).
		Where("timestamp >= ? AND timestamp <= ?", from, to)

	if namespace != "" {
		query = query.Where("namespace = ?", namespace)
	}
	if service != "" {
		query = query.Where("service = ?", service)
	}

	err = query.
		Order("timestamp ASC").
		Find(&observations).Error

	return observations, err
}
