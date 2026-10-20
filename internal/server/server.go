package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/iwantm/IncidentLens/internal/alertmanager"
	"github.com/iwantm/IncidentLens/internal/events"
	"github.com/iwantm/IncidentLens/internal/telemetry"
	"go.opentelemetry.io/otel/trace"
)

type EventStore interface {
	Ping(ctx context.Context) error
	Create(ctx context.Context, event *events.Event) error
	ListByIncident(ctx context.Context, incidentID string) ([]events.Event, error)
	ListIncidents(ctx context.Context, namespace string, service string, status string, from time.Time, to time.Time, limit int, offset int) ([]events.Incident, error)
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
	store     EventStore
	readiness []func(context.Context) error
	mux       *http.ServeMux
}

func NewServer(store EventStore, readiness ...func(context.Context) error) *Server {
	serv := &Server{
		store:     store,
		readiness: readiness,
		mux:       http.NewServeMux(),
	}

	serv.mux.HandleFunc("GET /health", serv.health)
	serv.mux.HandleFunc("GET /ready", serv.ready)
	serv.mux.HandleFunc("POST /api/v1/events", serv.createEvent)
	serv.mux.HandleFunc("POST /api/v1/ingest/alertmanager", serv.ingestAlertmanager)
	serv.mux.HandleFunc("GET /api/v1/incidents", serv.listIncidents)
	serv.mux.HandleFunc("GET /api/v1/incidents/{incidentID}", serv.getIncident)
	serv.mux.HandleFunc("GET /api/v1/incidents/{incidentID}/events", serv.listEvents)

	return serv
}

func (s *Server) Handler() http.Handler {
	return requestLogger(s.mux)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		slog.ErrorContext(r.Context(), "readiness check failed", "error", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
		return
	}
	for _, check := range s.readiness {
		if err := check(r.Context()); err != nil {
			slog.ErrorContext(r.Context(), "readiness check failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) createEvent(w http.ResponseWriter, r *http.Request) {
	var event events.Event

	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&event); err != nil {
		slog.WarnContext(r.Context(), "rejected invalid event payload", "error", err)
		writeJSON(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if err := validateEvent(&event); err != nil {
		slog.WarnContext(r.Context(), "rejected invalid event", "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": err.Error(),
		})
		return
	}

	if event.ObservedAt.IsZero() {
		event.ObservedAt = time.Now().UTC()
	}
	if status, err := s.processEvent(r.Context(), &event); err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, event)
}

func (s *Server) ingestAlertmanager(w http.ResponseWriter, r *http.Request) {
	var notification alertmanager.Webhook
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&notification); err != nil {
		slog.WarnContext(r.Context(), "rejected invalid Alertmanager payload", "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid Alertmanager payload"})
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request must contain one JSON value"})
		return
	}

	converted, err := notification.Events(time.Now().UTC())
	if err != nil {
		slog.WarnContext(r.Context(), "rejected invalid Alertmanager notification", "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	for _, event := range converted {
		if status, err := s.processEvent(r.Context(), &event); err != nil {
			slog.ErrorContext(r.Context(), "failed to ingest Alertmanager alert", "incident_id", event.IncidentID, "event_id", event.ID, "error", err)
			writeJSON(w, status, map[string]string{"error": "could not ingest Alertmanager alert"})
			return
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "accepted", "alerts_received": len(converted)})
}

func (s *Server) processEvent(ctx context.Context, event *events.Event) (int, error) {
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
		if err := s.store.OpenIncident(ctx, incident, event, 5*time.Minute); err != nil {
			if errors.Is(err, events.ErrIncidentAlreadyClosed) {
				telemetry.RecordIncidentAction(ctx, "open", "conflict")
				return http.StatusConflict, errors.New("incident is already closed")
			}
			telemetry.RecordIncidentAction(ctx, "open", "error")
			slog.ErrorContext(ctx, "failed to open incident", "incident_id", event.IncidentID, "event_id", event.ID, "error", err)
			return http.StatusInternalServerError, errors.New("could not open incident")
		}
		telemetry.RecordIncidentAction(ctx, "open", "success")
	case "alert_resolved":
		if err := s.store.ResolveIncident(ctx, event.IncidentID, event); err != nil {
			if errors.Is(err, events.ErrIncidentNotFound) {
				telemetry.RecordIncidentAction(ctx, "resolve", "not_found")
				return http.StatusNotFound, errors.New("incident not found")
			}
			if errors.Is(err, events.ErrIncidentAlreadyClosed) {
				telemetry.RecordIncidentAction(ctx, "resolve", "conflict")
				return http.StatusConflict, errors.New("incident is already closed")
			}
			telemetry.RecordIncidentAction(ctx, "resolve", "error")
			slog.ErrorContext(ctx, "failed to resolve incident", "incident_id", event.IncidentID, "event_id", event.ID, "error", err)
			return http.StatusInternalServerError, errors.New("could not resolve incident")
		}
		telemetry.RecordIncidentAction(ctx, "resolve", "success")
	default:
		if err := s.store.Create(ctx, event); err != nil {
			slog.ErrorContext(ctx, "failed to save event", "incident_id", event.IncidentID, "event_id", event.ID, "error", err)
			return http.StatusInternalServerError, errors.New("could not save event")
		}
	}
	return http.StatusCreated, nil
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
		slog.ErrorContext(r.Context(), "failed to load incident", "incident_id", incidentID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not load incident"})
		return
	}

	writeJSON(w, http.StatusOK, incident)
}

func (s *Server) listIncidents(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	namespace := strings.TrimSpace(query.Get("namespace"))
	service := strings.TrimSpace(query.Get("service"))
	status := strings.TrimSpace(query.Get("status"))
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")

	var fromTime, toTime time.Time
	var err error
	if from != "" {
		fromTime, err = time.Parse(time.RFC3339Nano, from)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid 'from' timestamp; expected RFC3339"})
			return
		}
	}
	if to != "" {
		toTime, err = time.Parse(time.RFC3339Nano, to)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid 'to' timestamp; expected RFC3339"})
			return
		}
	}
	if !fromTime.IsZero() && !toTime.IsZero() && fromTime.After(toTime) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "'from' must not be after 'to'"})
		return
	}
	if status != "" && status != events.IncidentStatusOpen && status != events.IncidentStatusClosed {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status must be 'open' or 'closed'"})
		return
	}
	limit := 50
	if rawLimit := query.Get("limit"); rawLimit != "" {
		limit, err = strconv.Atoi(rawLimit)
		if err != nil || limit < 1 || limit > 200 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be an integer between 1 and 200"})
			return
		}
	}
	offset := 0
	if rawOffset := query.Get("offset"); rawOffset != "" {
		offset, err = strconv.Atoi(rawOffset)
		if err != nil || offset < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "offset must be a non-negative integer"})
			return
		}
	}

	incidents, err := s.store.ListIncidents(r.Context(), namespace, service, status, fromTime, toTime, limit, offset)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to list incidents", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not list incidents"})
		return
	}

	writeJSON(w, http.StatusOK, incidents)
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
		slog.ErrorContext(r.Context(), "failed to load incident timeline", "incident_id", incidentID, "error", err)
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

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		level := slog.LevelInfo
		if status >= http.StatusInternalServerError {
			level = slog.LevelError
		} else if status >= http.StatusBadRequest {
			level = slog.LevelWarn
		}
		attrs := []any{"method", r.Method, "path", r.URL.Path, "status", status, "duration", time.Since(startedAt)}
		spanContext := trace.SpanContextFromContext(r.Context())
		if spanContext.IsValid() {
			attrs = append(attrs, "trace_id", spanContext.TraceID().String(), "span_id", spanContext.SpanID().String())
		}
		slog.Log(r.Context(), level, "http request", attrs...)
	})
}
