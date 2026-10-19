package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/iwantm/IncidentLens/internal/events"
)

type EventStore interface {
	Create(ctx context.Context, event *events.Event) error
	ListByIncident(ctx context.Context, incidentID string) ([]events.Event, error)
	OpenIncident(
		ctx context.Context,
		incident *events.Incident,
		alert *events.Event,
		backfillWindow time.Duration,
	) error
	ResolveIncident(
		ctx context.Context,
		incidentID string,
		resolution *events.Event,
	) error
	GetIncident(ctx context.Context, incidentID string) (events.Incident, error)
}

type Server struct {
	store EventStore
	mux   *http.ServeMux
}

func NewServer(store EventStore) *Server {
	serv := &Server{
		store: store,
		mux:   http.NewServeMux(),
	}

	serv.mux.HandleFunc("POST /api/v1/events", serv.createEvent)
	serv.mux.HandleFunc("GET /api/v1/incidents/{incidentID}", serv.getIncident)
	serv.mux.HandleFunc("GET /api/v1/incidents/{incidentID}/events", serv.listEvents)

	return serv
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) createEvent(w http.ResponseWriter, r *http.Request) {
	var event events.Event

	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&event); err != nil {
		writeJSON(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if err := validateEvent(&event); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": err.Error(),
		})
		return
	}

	if event.ObservedAt.IsZero() {
		event.ObservedAt = time.Now().UTC()
	}

	switch event.Type {
	case "alert_fired":
		incident := &events.Incident{
			ID:        event.IncidentID,
			Namespace: event.Namespace,
			Service:   event.Service,
			Summary:   event.Summary,
			Status:    events.IncidentStatusOpen,
			StartedAt: event.Timestamp,
		}
		if err := s.store.OpenIncident(r.Context(), incident, &event, 5*time.Minute); err != nil {
			if errors.Is(err, events.ErrIncidentAlreadyClosed) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "incident is already closed"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "could not open incident",
			})
			return
		}
	case "alert_resolved":
		if err := s.store.ResolveIncident(r.Context(), event.IncidentID, &event); err != nil {
			log.Printf("resolve incident %q failed: %v", event.IncidentID, err)
			if errors.Is(err, events.ErrIncidentNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "incident not found"})
				return
			}
			if errors.Is(err, events.ErrIncidentAlreadyClosed) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "incident is already closed"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "could not resolve incident",
			})
			return
		}
	default:
		if err := s.store.Create(r.Context(), &event); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "could not save event",
			})
			return
		}
	}

	writeJSON(w, http.StatusCreated, event)
}

func (s *Server) getIncident(w http.ResponseWriter, r *http.Request) {
	incidentID := r.PathValue("incidentID")
	if strings.TrimSpace(incidentID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "incident ID is required",
		})
		return
	}

	incident, err := s.store.GetIncident(r.Context(), incidentID)
	if err != nil {
		if errors.Is(err, events.ErrIncidentNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "incident not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not load incident"})
		return
	}

	writeJSON(w, http.StatusOK, incident)
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	incidentID := r.PathValue("incidentID")
	if strings.TrimSpace(incidentID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "incident ID is required",
		})
		return
	}

	events, err := s.store.ListByIncident(r.Context(), incidentID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "could not load incident events",
		})
		return
	}

	writeJSON(w, http.StatusOK, events)
}

func validateEvent(event *events.Event) error {
	switch {
	case strings.TrimSpace(event.ID) == "":
		return errors.New("id is required")
	case strings.TrimSpace(event.IncidentID) == "":
		return errors.New("incident_id is required")
	case event.Timestamp.IsZero():
		return errors.New("timestamp is required")
	case strings.TrimSpace(event.Type) == "":
		return errors.New("type is required")
	case strings.TrimSpace(event.Summary) == "":
		return errors.New("summary is required")
	case strings.TrimSpace(event.Source) == "":
		return errors.New("source is required")
	default:
		return nil
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
