package kube

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/iwantm/IncidentLens/internal/events"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestNewClientRequiresInClusterConfiguration(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")
	client, err := NewClient()
	if err == nil {
		t.Fatal("NewClient() error = nil outside a cluster, want configuration error")
	}
	if client != nil {
		t.Fatalf("NewClient() client = %#v on configuration error, want nil", client)
	}
}

func TestIsNotFound(t *testing.T) {
	if !IsNotFound(apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "missing")) {
		t.Fatal("IsNotFound() = false for Kubernetes NotFound error")
	}
	if IsNotFound(errors.New("other error")) {
		t.Fatal("IsNotFound() = true for a generic error")
	}
}

type captureObservationStore struct {
	mu           sync.Mutex
	observations []events.Observation
	received     chan events.Observation
	err          error
}

func (s *captureObservationStore) UpsertObservation(_ context.Context, observation *events.Observation) error {
	s.mu.Lock()
	s.observations = append(s.observations, *observation)
	s.mu.Unlock()
	if s.received != nil {
		s.received <- *observation
	}
	return s.err
}

func TestRecordEventConvertsKubernetesEvent(t *testing.T) {
	store := &captureObservationStore{}
	watcher := NewWatcher(nil, "incident-lens", store)
	eventTime := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	kubeEvent := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Namespace: "incident-lens", Name: "demo-app-warning", UID: types.UID("event-uid"), ResourceVersion: "12"},
		InvolvedObject: corev1.ObjectReference{APIVersion: "apps/v1", Kind: "Deployment", Name: "demo-app", UID: types.UID("deployment-uid")},
		Reason:         "Failed", Message: "Container failed", Action: "StartContainer", Count: 3,
		EventTime: metav1.MicroTime{Time: eventTime}, LastTimestamp: metav1.NewTime(eventTime.Add(time.Minute)),
	}

	watcher.recordEvent(context.Background(), kubeEvent)
	if len(store.observations) != 1 {
		t.Fatalf("stored observations = %d, want 1", len(store.observations))
	}
	got := store.observations[0]
	if got.Timestamp != eventTime || got.SourceID != "event-uid" || got.SourceVersion != "12" {
		t.Errorf("event identity/timestamp conversion incorrect: %#v", got)
	}
	if got.Type != "kubernetes_event" || got.Namespace != "incident-lens" || got.Summary != "Failed: Container failed" {
		t.Errorf("event fields conversion incorrect: %#v", got)
	}
	if got.Object.Name != "demo-app" || got.Object.UID != "deployment-uid" {
		t.Errorf("involved object conversion incorrect: %#v", got.Object)
	}
	if got.Evidence["count"] != int32(3) || got.Evidence["reason"] != "Failed" {
		t.Errorf("event evidence conversion incorrect: %#v", got.Evidence)
	}
}

func TestRecordEventHandlesNilAndStorageFailure(t *testing.T) {
	storeErr := errors.New("database unavailable")
	store := &captureObservationStore{err: storeErr}
	watcher := NewWatcher(nil, "incident-lens", store)
	watcher.recordEvent(context.Background(), nil)
	if len(store.observations) != 0 {
		t.Fatalf("nil event stored %d observations, want none", len(store.observations))
	}
	watcher.recordEvent(context.Background(), &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Namespace: "incident-lens"},
	})
	if len(store.observations) != 1 {
		t.Fatalf("stored observations = %d, want 1 despite storage error", len(store.observations))
	}
	if store.observations[0].Summary != "Kubernetes Event observed" {
		t.Errorf("empty event summary = %q, want fallback summary", store.observations[0].Summary)
	}
	if store.observations[0].SourceVersion == "" {
		t.Error("empty resource version should receive an observed-time version")
	}
}

func TestEventTimestampFallbackOrder(t *testing.T) {
	first := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	last := first.Add(time.Minute)
	eventTime := last.Add(time.Minute)
	tests := []struct {
		name  string
		event corev1.Event
		want  time.Time
	}{
		{name: "event time wins", event: corev1.Event{EventTime: metav1.MicroTime{Time: eventTime}, LastTimestamp: metav1.NewTime(last), FirstTimestamp: metav1.NewTime(first)}, want: eventTime},
		{name: "last timestamp fallback", event: corev1.Event{LastTimestamp: metav1.NewTime(last), FirstTimestamp: metav1.NewTime(first)}, want: last},
		{name: "first timestamp fallback", event: corev1.Event{FirstTimestamp: metav1.NewTime(first)}, want: first},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eventTimestamp(&tt.event); !got.Equal(tt.want) {
				t.Errorf("eventTimestamp() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRecordDeploymentChangeConvertsObservation(t *testing.T) {
	store := &captureObservationStore{}
	watcher := NewWatcher(nil, "incident-lens", store)
	oldDeployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Generation: 4}}
	newDeployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Namespace: "incident-lens", Name: "demo-app", UID: types.UID("deployment-uid"), Generation: 5,
	}}

	watcher.recordDeploymentChange(context.Background(), oldDeployment, newDeployment)
	if len(store.observations) != 1 {
		t.Fatalf("stored observations = %d, want 1", len(store.observations))
	}
	got := store.observations[0]
	if got.Source != "kubernetes_watch" || got.SourceID != "deployment-uid" || got.SourceVersion != "5" {
		t.Errorf("deployment identity conversion incorrect: %#v", got)
	}
	if got.Type != "deployment_changed" || got.Service != "demo-app" || got.Object.Kind != "Deployment" {
		t.Errorf("deployment fields conversion incorrect: %#v", got)
	}
	if got.Evidence["old_generation"] != int64(4) || got.Evidence["generation"] != int64(5) {
		t.Errorf("deployment evidence conversion incorrect: %#v", got.Evidence)
	}
}

func TestRecordDeploymentChangeHandlesNilAndStorageFailure(t *testing.T) {
	store := &captureObservationStore{err: errors.New("database unavailable")}
	watcher := NewWatcher(nil, "incident-lens", store)
	watcher.recordDeploymentChange(context.Background(), nil, nil)
	if len(store.observations) != 0 {
		t.Fatalf("nil deployment stored %d observations, want none", len(store.observations))
	}
	watcher.recordDeploymentChange(context.Background(), &appsv1.Deployment{}, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "incident-lens", Name: "demo-app", Generation: 2},
	})
	if len(store.observations) != 1 {
		t.Fatalf("stored observations = %d, want 1 despite storage error", len(store.observations))
	}
	if store.observations[0].SourceID != "incident-lens/demo-app" {
		t.Errorf("deployment source ID = %q, want namespace/name fallback", store.observations[0].SourceID)
	}
}

func TestRunWatchesInitialEventsAndDeploymentChanges(t *testing.T) {
	namespace := "incident-lens"
	initialEvent := &corev1.Event{ObjectMeta: metav1.ObjectMeta{
		Namespace: namespace, Name: "started", UID: types.UID("event-uid"), ResourceVersion: "1",
	}, Reason: "Scheduled", Message: "Pod scheduled"}
	initialDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "demo-app", UID: types.UID("deployment-uid"), Generation: 1},
		Spec:       appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"version": "one"}}}},
	}
	client := fake.NewSimpleClientset(initialEvent, initialDeployment)
	store := &captureObservationStore{received: make(chan events.Observation, 10)}
	watcher := NewWatcher(client, namespace, store)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- watcher.Run(ctx) }()

	initial := receiveObservation(t, store.received)
	if initial.Type != "kubernetes_event" {
		t.Fatalf("initial observation type = %q, want kubernetes_event", initial.Type)
	}
	waitForReady(t, watcher)

	updated := initialDeployment.DeepCopy()
	updated.Generation = 2
	updated.Spec.Template.Labels["version"] = "two"
	if _, err := client.AppsV1().Deployments(namespace).Update(ctx, updated, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update deployment: %v", err)
	}
	changed := receiveObservation(t, store.received)
	if changed.Type != "deployment_changed" || changed.SourceVersion != "2" {
		t.Fatalf("deployment change observation = %#v", changed)
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("watcher.Run() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watcher.Run() did not stop after context cancellation")
	}
	if watcher.Ready() {
		t.Error("watcher remained ready after Run stopped")
	}
}

func receiveObservation(t *testing.T, observations <-chan events.Observation) events.Observation {
	t.Helper()
	select {
	case observation := <-observations:
		return observation
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for watcher observation")
		return events.Observation{}
	}
}

func waitForReady(t *testing.T, watcher *Watcher) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if watcher.Ready() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("watcher did not become ready after caches synced")
}
