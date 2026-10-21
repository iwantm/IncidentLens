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
	pingErr         error
	resolveErr      error
	openErr         error
	createErr       error
	getErr          error
	listErr         error
	incidentListErr error
	incident        events.Incident
	event           events.Event
	events          []events.Event
	incidents       []events.Incident
	listQuery       struct {
		namespace string
		service   string
		status    string
		from      time.Time
		to        time.Time
		limit     int
		offset    int
	}
}

func (f *fakeEventStore) Ping(context.Context) error { return f.pingErr }

func (f *fakeEventStore) Create(_ context.Context, event *events.Event) error {
	f.event = *event
	return f.createErr
}

func (f *fakeEventStore) ListByIncident(context.Context, string) ([]events.Event, error) {
	return f.events, f.listErr
}

func (f *fakeEventStore) ListIncidents(_ context.Context, namespace, service, status string, from, to time.Time, limit, offset int) ([]events.Incident, error) {
	f.listQuery.namespace = namespace
	f.listQuery.service = service
	f.listQuery.status = status
	f.listQuery.from = from
	f.listQuery.to = to
	f.listQuery.limit = limit
	f.listQuery.offset = offset
	return f.incidents, f.incidentListErr
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

func TestHealthAndReadinessEndpoints(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		pingErr    error
		wantStatus int
	}{
		{name: "liveness is healthy", path: "/health", wantStatus: http.StatusOK},
		{name: "readiness when database responds", path: "/ready", wantStatus: http.StatusOK},
		{name: "readiness when database fails", path: "/ready", pingErr: errors.New("database unavailable"), wantStatus: http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeEventStore{pingErr: tt.pingErr}
			response := httptest.NewRecorder()
			NewServer(store).Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, tt.wantStatus, response.Body.String())
			}
		})
	}
}

func TestReadinessDependencyFailure(t *testing.T) {
	readinessErr := errors.New("Kubernetes watcher caches are not synchronized")
	handler := NewServer(&fakeEventStore{}, func(context.Context) error { return readinessErr }).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
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

func TestAlertmanagerWebhookOpensIncident(t *testing.T) {
	store := &fakeEventStore{}
	body := `{"version":"4","groupKey":"demo","status":"firing","receiver":"incident-lens","alerts":[{"status":"firing","labels":{"alertname":"DemoAppErrors","namespace":"incident-lens","service":"demo-app"},"annotations":{"summary":"Demo errors"},"startsAt":"2026-10-20T09:58:00Z","fingerprint":"abc123"}]}`
	response := sendJSONRequest(t, NewServer(store).Handler(), http.MethodPost, "/api/v1/ingest/alertmanager", body)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusAccepted, response.Body.String())
	}
	if store.incident.ID == "" || store.incident.Status != events.IncidentStatusOpen {
		t.Errorf("opened incident = %#v", store.incident)
	}
	if store.event.Type != "alert_fired" || store.event.Source != "alertmanager" || store.event.Service != "demo-app" {
		t.Errorf("stored event = %#v", store.event)
	}
}

func TestAlertmanagerWebhookRejectsInvalidPayload(t *testing.T) {
	response := sendJSONRequest(t, NewServer(&fakeEventStore{}).Handler(), http.MethodPost, "/api/v1/ingest/alertmanager", `{"version":"3","alerts":[]}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusBadRequest, response.Body.String())
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

func TestListIncidentsDefaultsAndFilters(t *testing.T) {
	store := &fakeEventStore{incidents: []events.Incident{{ID: "incident-1", Status: events.IncidentStatusOpen}}}
	handler := NewServer(store).Handler()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/incidents", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "incident-1") {
		t.Fatalf("default list response = %d %s", response.Code, response.Body.String())
	}
	if store.listQuery.limit != 50 || store.listQuery.offset != 0 {
		t.Fatalf("default pagination = limit %d offset %d, want 50 and 0", store.listQuery.limit, store.listQuery.offset)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/incidents?namespace=incident-lens&service=demo-app&status=open&from=2026-10-20T09:00:00Z&to=2026-10-20T10:00:00Z&limit=25&offset=50", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("filtered list status = %d, body: %s", response.Code, response.Body.String())
	}
	query := store.listQuery
	if query.namespace != "incident-lens" || query.service != "demo-app" || query.status != "open" || query.limit != 25 || query.offset != 50 {
		t.Fatalf("store query = %#v", query)
	}
	if query.from.Format(time.RFC3339) != "2026-10-20T09:00:00Z" || query.to.Format(time.RFC3339) != "2026-10-20T10:00:00Z" {
		t.Fatalf("time filters = %s through %s", query.from, query.to)
	}
}

func TestListIncidentsRejectsInvalidQuery(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{name: "bad from timestamp", query: "?from=yesterday"},
		{name: "bad to timestamp", query: "?to=tomorrow"},
		{name: "reversed range", query: "?from=2026-10-20T10:00:00Z&to=2026-10-20T09:00:00Z"},
		{name: "unsupported status", query: "?status=active"},
		{name: "too-large limit", query: "?limit=201"},
		{name: "negative offset", query: "?offset=-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			NewServer(&fakeEventStore{}).Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/incidents"+tt.query, nil))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusBadRequest, response.Body.String())
			}
		})
	}
}

func TestAnalyzeIncidentEndpoint(t *testing.T) {
	startedAt := time.Now().UTC().Add(-time.Minute)
	deployment := events.Event{
		ID: "deployment-1", Type: "deployment_changed", Namespace: "incident-lens",
		Service: "demo-app", Timestamp: startedAt.Add(-time.Minute),
	}
	store := &fakeEventStore{
		incident: events.Incident{
			ID: "incident-1", Namespace: "incident-lens", Service: "demo-app",
			Status: events.IncidentStatusOpen, StartedAt: startedAt,
		},
		events: []events.Event{deployment},
	}
	response := httptest.NewRecorder()
	NewServer(store).Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/incidents/incident-1/analysis", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body.String())
	}
	for _, expected := range []string{"deployment_change_before_alert", "deployment-1"} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Errorf("analysis response %q does not contain %q", response.Body.String(), expected)
		}
	}
}

func TestAnalyzeIncidentEndpointErrors(t *testing.T) {
	tests := []struct {
		name       string
		store      *fakeEventStore
		wantStatus int
	}{
		{name: "missing incident", store: &fakeEventStore{getErr: events.ErrIncidentNotFound}, wantStatus: http.StatusNotFound},
		{name: "incident read error", store: &fakeEventStore{getErr: errors.New("database unavailable")}, wantStatus: http.StatusInternalServerError},
		{name: "timeline read error", store: &fakeEventStore{incident: events.Incident{ID: "incident-1"}, listErr: errors.New("database unavailable")}, wantStatus: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			NewServer(tt.store).Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/incidents/incident-1/analysis", nil))
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, tt.wantStatus, response.Body.String())
			}
		})
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
