package telemetry

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
)

func TestSetupExportsOTelSignalsOverHTTP(t *testing.T) {
	var mu sync.Mutex
	paths := map[string]int{}
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths[r.URL.Path]++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()

	t.Setenv("OTEL_TRACES_EXPORTER", "otlp")
	t.Setenv("OTEL_METRICS_EXPORTER", "otlp")
	t.Setenv("OTEL_LOGS_EXPORTER", "otlp")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", collector.URL)
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")

	shutdown, err := Setup(context.Background(), "incident-lens-test")
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	ctx, span := StartSpan(context.Background(), "test.operation")
	span.End()
	counter, err := otel.Meter("incident-lens-test").Int64Counter("test.operations")
	if err != nil {
		t.Fatalf("create test counter: %v", err)
	}
	counter.Add(ctx, 1)
	slog.InfoContext(ctx, "test log export")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown telemetry providers: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, path := range []string{"/v1/traces", "/v1/metrics", "/v1/logs"} {
		if paths[path] == 0 {
			t.Errorf("collector did not receive signal at %s; received paths: %#v", path, paths)
		}
	}
}

func TestExporterEnabled(t *testing.T) {
	tests := []struct {
		name           string
		signal         string
		selector       string
		endpoint       string
		signalEndpoint string
		want           bool
		wantErr        bool
	}{
		{name: "disabled by default", signal: "TRACES"},
		{name: "shared endpoint enables signal", signal: "TRACES", endpoint: "http://collector:4318", want: true},
		{name: "signal endpoint enables signal", signal: "METRICS", signalEndpoint: "http://collector:4318/v1/metrics", want: true},
		{name: "explicit otlp selector", signal: "LOGS", selector: "otlp", want: true},
		{name: "explicit none overrides endpoint", signal: "TRACES", selector: "none", endpoint: "http://collector:4318", want: false},
		{name: "unsupported selector rejected", signal: "TRACES", selector: "console", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OTEL_"+tt.signal+"_EXPORTER", tt.selector)
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", tt.endpoint)
			t.Setenv("OTEL_EXPORTER_OTLP_"+tt.signal+"_ENDPOINT", tt.signalEndpoint)
			got, err := exporterEnabled(tt.signal)
			if (err != nil) != tt.wantErr {
				t.Fatalf("exporterEnabled() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("exporterEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
