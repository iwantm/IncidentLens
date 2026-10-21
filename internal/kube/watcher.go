// internal/kube/watcher.go
package kube

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/iwantm/IncidentLens/internal/events"
	"github.com/iwantm/IncidentLens/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// ObservationStore is implemented by storage.EventStore.
type ObservationStore interface {
	UpsertObservation(
		ctx context.Context,
		observation *events.Observation,
	) error
}

type Watcher struct {
	client    kubernetes.Interface
	namespace string
	store     ObservationStore
	ready     atomic.Bool
}

func (w *Watcher) Ready() bool {
	return w.ready.Load()
}

func NewWatcher(
	client kubernetes.Interface,
	namespace string,
	store ObservationStore,
) *Watcher {
	return &Watcher{
		client:    client,
		namespace: namespace,
		store:     store,
	}
}

// Run watches Kubernetes Events and Deployment pod-template changes
// until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) error {
	factory := informers.NewSharedInformerFactoryWithOptions(
		w.client,
		0,
		informers.WithNamespace(w.namespace),
	)

	eventInformer := factory.Core().V1().Events().Informer()
	deploymentInformer := factory.Apps().V1().Deployments().Informer()

	_, err := eventInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			event, ok := obj.(*corev1.Event)
			if !ok {
				return
			}
			w.recordEvent(ctx, event)
		},
		UpdateFunc: func(_, newObj any) {
			event, ok := newObj.(*corev1.Event)
			if !ok {
				return
			}
			// Kubernetes may update an Event as repeated occurrences are aggregated.
			w.recordEvent(ctx, event)
		},
	})
	if err != nil {
		return fmt.Errorf("register Kubernetes Event handler: %w", err)
	}

	_, err = deploymentInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		// No AddFunc: the initial Deployment list is not a change.
		UpdateFunc: func(oldObj, newObj any) {
			oldDeployment, oldOK := oldObj.(*appsv1.Deployment)
			newDeployment, newOK := newObj.(*appsv1.Deployment)
			if !oldOK || !newOK {
				return
			}

			// Ignore status-only changes; keep changes that alter the pods.
			if apiequality.Semantic.DeepEqual(
				oldDeployment.Spec.Template,
				newDeployment.Spec.Template,
			) {
				return
			}

			w.recordDeploymentChange(ctx, oldDeployment, newDeployment)
		},
	})
	if err != nil {
		return fmt.Errorf("register Deployment handler: %w", err)
	}

	factory.Start(ctx.Done())

	if !cache.WaitForCacheSync(
		ctx.Done(),
		eventInformer.HasSynced,
		deploymentInformer.HasSynced,
	) {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("Kubernetes informer caches failed to sync")
	}
	w.ready.Store(true)
	defer w.ready.Store(false)
	slog.Info("Kubernetes watcher caches synchronized", "namespace", w.namespace)

	<-ctx.Done()
	return nil
}

func (w *Watcher) recordEvent(ctx context.Context, event *corev1.Event) {
	if event == nil {
		return
	}
	ctx, span := telemetry.StartSpan(ctx, "kubernetes.event.observe",
		attribute.String("k8s.namespace.name", event.Namespace),
		attribute.String("k8s.event.name", event.Name),
	)
	var saveErr error
	defer func() { telemetry.EndSpan(span, saveErr) }()

	observedAt := time.Now().UTC()
	sourceVersion := event.ResourceVersion
	if sourceVersion == "" {
		sourceVersion = observedAt.Format(time.RFC3339Nano)
	}

	summary := strings.TrimSpace(strings.Join(
		[]string{event.Reason, event.Message},
		": ",
	))
	if summary == "" {
		summary = "Kubernetes Event observed"
	}

	observation := &events.Observation{
		Source:        "kubernetes_watch",
		SourceID:      string(event.UID),
		SourceVersion: sourceVersion,
		Timestamp:     eventTimestamp(event),
		ObservedAt:    observedAt,
		Type:          "kubernetes_event",
		Namespace:     event.Namespace,
		Summary:       summary,
		Object: events.ObjectReference{
			APIVersion: event.InvolvedObject.APIVersion,
			Kind:       event.InvolvedObject.Kind,
			Name:       event.InvolvedObject.Name,
			UID:        string(event.InvolvedObject.UID),
		},
		Evidence: map[string]any{
			"event_type":             event.Type,
			"reason":                 event.Reason,
			"action":                 event.Action,
			"count":                  event.Count,
			"reporting_controller":   event.ReportingController,
			"event_resource_version": event.ResourceVersion,
		},
	}

	if saveErr = w.store.UpsertObservation(ctx, observation); saveErr != nil {
		telemetry.RecordObservation(ctx, "kubernetes_event", "error")
		slog.ErrorContext(ctx, "failed to save Kubernetes Event observation", "namespace", event.Namespace, "event_name", event.Name, "error", saveErr)
	} else {
		telemetry.RecordObservation(ctx, "kubernetes_event", "success")
	}
}

func (w *Watcher) recordDeploymentChange(
	ctx context.Context,
	oldDeployment *appsv1.Deployment,
	newDeployment *appsv1.Deployment,
) {
	if newDeployment == nil {
		return
	}
	ctx, span := telemetry.StartSpan(ctx, "kubernetes.deployment.change.observe",
		attribute.String("k8s.namespace.name", newDeployment.Namespace),
		attribute.String("k8s.deployment.name", newDeployment.Name),
		attribute.Int64("k8s.deployment.generation", newDeployment.Generation),
	)
	var saveErr error
	defer func() { telemetry.EndSpan(span, saveErr) }()

	observedAt := time.Now().UTC()
	sourceID := string(newDeployment.UID)
	if sourceID == "" {
		sourceID = newDeployment.Namespace + "/" + newDeployment.Name
	}

	observation := &events.Observation{
		Source:        "kubernetes_watch",
		SourceID:      sourceID,
		SourceVersion: strconv.FormatInt(newDeployment.Generation, 10),
		Timestamp:     observedAt,
		ObservedAt:    observedAt,
		Type:          "deployment_changed",
		Namespace:     newDeployment.Namespace,
		Service:       newDeployment.Name,
		Summary:       "Deployment pod template changed",
		Object: events.ObjectReference{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Name:       newDeployment.Name,
			UID:        string(newDeployment.UID),
		},
		Evidence: map[string]any{
			"generation":     newDeployment.Generation,
			"changed_fields": []string{"spec.template"},
			"old_generation": oldDeployment.Generation,
		},
	}

	if saveErr = w.store.UpsertObservation(ctx, observation); saveErr != nil {
		telemetry.RecordObservation(ctx, "deployment_change", "error")
		slog.ErrorContext(ctx, "failed to save Deployment observation", "namespace", newDeployment.Namespace, "deployment", newDeployment.Name, "error", saveErr)
	} else {
		telemetry.RecordObservation(ctx, "deployment_change", "success")
	}
}

func eventTimestamp(event *corev1.Event) time.Time {
	switch {
	case !event.EventTime.Time.IsZero():
		return event.EventTime.Time.UTC()
	case !event.LastTimestamp.IsZero():
		return event.LastTimestamp.Time.UTC()
	case !event.FirstTimestamp.IsZero():
		return event.FirstTimestamp.Time.UTC()
	default:
		return time.Now().UTC()
	}
}
