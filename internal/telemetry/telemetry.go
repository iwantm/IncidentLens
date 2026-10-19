package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/klog/v2"
)

const instrumentationName = "github.com/iwantm/IncidentLens"

var businessMetrics struct {
	incidentActions metric.Int64Counter
	observations    metric.Int64Counter
}

func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return otel.Tracer(instrumentationName).Start(ctx, name, trace.WithAttributes(attrs...))
}

func EndSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

func RecordIncidentAction(ctx context.Context, action, outcome string) {
	if businessMetrics.incidentActions != nil {
		businessMetrics.incidentActions.Add(ctx, 1, metric.WithAttributes(
			attribute.String("action", action),
			attribute.String("outcome", outcome),
		))
	}
}

func RecordObservation(ctx context.Context, kind, outcome string) {
	if businessMetrics.observations != nil {
		businessMetrics.observations.Add(ctx, 1, metric.WithAttributes(
			attribute.String("kind", kind),
			attribute.String("outcome", outcome),
		))
	}
}

// Setup configures OpenTelemetry exporters from standard OTEL environment
// variables. When no exporter or endpoint is configured, the application keeps
// JSON logs on stdout and uses the OpenTelemetry no-op providers.
func Setup(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	resource, err := resource.New(ctx,
		resource.WithAttributes(attribute.String("service.name", serviceName)),
		resource.WithFromEnv(),
	)
	if err != nil {
		return nil, fmt.Errorf("create telemetry resource: %w", err)
	}

	var shutdowns []func(context.Context) error
	shutdown := func(ctx context.Context) error {
		var errs []error
		for i := len(shutdowns) - 1; i >= 0; i-- {
			if err := shutdowns[i](ctx); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}
	fail := func(err error) (func(context.Context) error, error) {
		_ = shutdown(context.Background())
		return nil, err
	}

	tracesEnabled, err := exporterEnabled("TRACES")
	if err != nil {
		return fail(err)
	}
	if tracesEnabled {
		exporter, err := otlptracehttp.New(ctx)
		if err != nil {
			return fail(fmt.Errorf("create OTLP trace exporter: %w", err))
		}
		provider := sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(exporter),
			sdktrace.WithResource(resource),
		)
		otel.SetTracerProvider(provider)
		shutdowns = append(shutdowns, provider.Shutdown)
	}

	metricsEnabled, err := exporterEnabled("METRICS")
	if err != nil {
		return fail(err)
	}
	if metricsEnabled {
		exporter, err := otlpmetrichttp.New(ctx)
		if err != nil {
			return fail(fmt.Errorf("create OTLP metric exporter: %w", err))
		}
		provider := sdkmetric.NewMeterProvider(
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)),
			sdkmetric.WithResource(resource),
		)
		otel.SetMeterProvider(provider)
		shutdowns = append(shutdowns, provider.Shutdown)
		meter := otel.Meter(instrumentationName)
		businessMetrics.incidentActions, err = meter.Int64Counter(
			"incidentlens.incident.actions",
			metric.WithDescription("Number of incident open and resolve actions by outcome"),
			metric.WithUnit("{action}"),
		)
		if err != nil {
			return fail(fmt.Errorf("create incident action metric: %w", err))
		}
		businessMetrics.observations, err = meter.Int64Counter(
			"incidentlens.watcher.observations",
			metric.WithDescription("Number of Kubernetes observations handled by outcome"),
			metric.WithUnit("{observation}"),
		)
		if err != nil {
			return fail(fmt.Errorf("create watcher observation metric: %w", err))
		}
	}

	logsEnabled, err := exporterEnabled("LOGS")
	if err != nil {
		return fail(err)
	}
	stdoutHandler := slog.NewJSONHandler(os.Stdout, nil)
	if logsEnabled {
		exporter, err := otlploghttp.New(ctx)
		if err != nil {
			return fail(fmt.Errorf("create OTLP log exporter: %w", err))
		}
		provider := sdklog.NewLoggerProvider(
			sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
			sdklog.WithResource(resource),
		)
		global.SetLoggerProvider(provider)
		shutdowns = append(shutdowns, provider.Shutdown)
		otelHandler := otelslog.NewHandler(instrumentationName, otelslog.WithLoggerProvider(provider))
		slog.SetDefault(slog.New(multiHandler{stdoutHandler, otelHandler}))
	} else {
		slog.SetDefault(slog.New(stdoutHandler))
	}
	klog.SetLogger(logr.FromSlogHandler(slog.Default().Handler()))

	if tracesEnabled {
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))
	}
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("OpenTelemetry internal error", "error", err)
	}))
	return shutdown, nil
}

func exporterEnabled(signal string) (bool, error) {
	selectorName := "OTEL_" + signal + "_EXPORTER"
	selector := strings.ToLower(strings.TrimSpace(os.Getenv(selectorName)))
	switch selector {
	case "otlp":
		return true, nil
	case "none":
		return false, nil
	case "":
		return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" || os.Getenv("OTEL_EXPORTER_OTLP_"+signal+"_ENDPOINT") != "", nil
	default:
		return false, fmt.Errorf("unsupported %s value %q; use otlp or none", selectorName, selector)
	}
}

type multiHandler []slog.Handler

func (h multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h multiHandler) Handle(ctx context.Context, record slog.Record) error {
	var errs []error
	for _, handler := range h {
		if handler.Enabled(ctx, record.Level) {
			if err := handler.Handle(ctx, record.Clone()); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (h multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	derived := make(multiHandler, len(h))
	for i, handler := range h {
		derived[i] = handler.WithAttrs(attrs)
	}
	return derived
}

func (h multiHandler) WithGroup(name string) slog.Handler {
	derived := make(multiHandler, len(h))
	for i, handler := range h {
		derived[i] = handler.WithGroup(name)
	}
	return derived
}
