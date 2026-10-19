package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iwantm/IncidentLens/internal/events"
)

type fakeEventStore struct {
	resolveErr error
	openErr    error
	createErr  error
	getErr     error
	listErr    error
	incident   events.Incident
	event      events.Event
	events     []events.Event
}

func (f *fakeEventStore) Create(_ context.Context, event *events.Event) error {
	f.event = *event
	return f.createErr
}

func (f *fakeEventStore) ListByIncident(context.Context, string) ([]events.Event, error) {
	return f.events, f.listErr
}

func (f *fakeEventStore) OpenIncident(_ context.Context, incident *events.Incident, alert *events.Event, _ time.Duration) error {
	f.incident = *incident
	f.event = *alert
	return f.openErr
}

func (f *fakeEventStore) ResolveIncident(context.Context, string, *events.Event) error {
	return f.resolveErr
}

func (f *fakeEventStore) GetIncident(context.Context, string) (events.Incident, error) {
	return f.incident, f.getErr
}

func TestCreateEventResolutionStatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		storeErr   error
		wantStatus int
	}{
		{name: "resolved", wantStatus: http.StatusCreated},
		{name: "unknown incident", storeErr: events.ErrIncidentNotFound, wantStatus: http.StatusNotFound},
		{name: "already closed", storeErr: events.ErrIncidentAlreadyClosed, wantStatus: http.StatusConflict},
		{name: "unexpected storage error", storeErr: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeEventStore{resolveErr: tt.storeErr}
			handler := NewServer(store).Handler()
			body := `{"id":"resolution-1","incident_id":"incident-1","timestamp":"2026-10-19T12:05:00Z","observed_at":"2026-10-19T12:05:01Z","type":"alert_resolved","namespace":"incident-lens","service":"demo-app","summary":"Errors resolved","source":"test"}`
			request := httptest.NewRequest(http.MethodPost, "/api/v1/events", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, tt.wantStatus, response.Body.String())
			}
		})
	}
}

func TestCreateEventOpensIncident(t *testing.T) {
	store := &fakeEventStore{}
	response := sendJSONRequest(t, NewServer(store).Handler(), http.MethodPost, "/api/v1/events", validEventJSON("alert_fired"))
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusCreated, response.Body.String())
	}
	if store.incident.ID != "incident-1" || store.incident.Status != events.IncidentStatusOpen {
		t.Errorf("opened incident = %#v", store.incident)
	}
	if store.event.Type != "alert_fired" {
		t.Errorf("stored alert type = %q, want alert_fired", store.event.Type)
	}
}

func TestCreateEventValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed JSON", body: "{"},
		{name: "unknown field", body: `{"extra":"not allowed"}`},
		{name: "missing event ID", body: `{"incident_id":"incident-1","timestamp":"2026-10-19T12:00:00Z","type":"custom","summary":"Test","source":"test"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := sendJSONRequest(t, NewServer(&fakeEventStore{}).Handler(), http.MethodPost, "/api/v1/events", tt.body)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusBadRequest, response.Body.String())
			}
		})
	}
}

func TestGetIncidentErrorStatuses(t *testing.T) {
	tests := []struct {
		name       string
		storeErr   error
		wantStatus int
	}{
		{name: "missing incident", storeErr: events.ErrIncidentNotFound, wantStatus: http.StatusNotFound},
		{name: "storage error", storeErr: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeEventStore{getErr: tt.storeErr}
			response := httptest.NewRecorder()
			NewServer(store).Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/incidents/incident-1", nil))
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, tt.wantStatus, response.Body.String())
			}
		})
	}
}

func TestListEventsStorageError(t *testing.T) {
	store := &fakeEventStore{listErr: errors.New("database unavailable")}
	response := httptest.NewRecorder()
	NewServer(store).Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/incidents/incident-1/events", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusInternalServerError, response.Body.String())
	}
}

func TestCreateEventStorageError(t *testing.T) {
	store := &fakeEventStore{createErr: errors.New("database unavailable")}
	response := sendJSONRequest(t, NewServer(store).Handler(), http.MethodPost, "/api/v1/events", validEventJSON("custom"))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusInternalServerError, response.Body.String())
	}
}

func TestGetIncidentAndEvents(t *testing.T) {
	store := &fakeEventStore{
		incident: events.Incident{ID: "incident-1", Status: events.IncidentStatusOpen},
		events:   []events.Event{{ID: "event-1", IncidentID: "incident-1"}},
	}
	handler := NewServer(store).Handler()

	incidentResponse := httptest.NewRecorder()
	handler.ServeHTTP(incidentResponse, httptest.NewRequest(http.MethodGet, "/api/v1/incidents/incident-1", nil))
	if incidentResponse.Code != http.StatusOK || !strings.Contains(incidentResponse.Body.String(), `"status":"open"`) {
		t.Errorf("GET incident response = %d %s", incidentResponse.Code, incidentResponse.Body.String())
	}

	eventsResponse := httptest.NewRecorder()
	handler.ServeHTTP(eventsResponse, httptest.NewRequest(http.MethodGet, "/api/v1/incidents/incident-1/events", nil))
	if eventsResponse.Code != http.StatusOK || !strings.Contains(eventsResponse.Body.String(), `"event-1"`) {
		t.Errorf("GET timeline response = %d %s", eventsResponse.Code, eventsResponse.Body.String())
	}
}

func sendJSONRequest(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func validEventJSON(eventType string) string {
	return `{"id":"event-1","incident_id":"incident-1","timestamp":"2026-10-19T12:00:00Z","observed_at":"2026-10-19T12:00:01Z","type":"` + eventType + `","namespace":"incident-lens","service":"demo-app","summary":"Demo app error","source":"test"}`
}
