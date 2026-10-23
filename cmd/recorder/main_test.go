package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRejectsInvalidTelemetryConfiguration(t *testing.T) {
	t.Setenv("OTEL_TRACES_EXPORTER", "unsupported")
	t.Setenv("OTEL_METRICS_EXPORTER", "none")
	t.Setenv("OTEL_LOGS_EXPORTER", "none")

	err := run()
	if err == nil || !strings.Contains(err.Error(), "unsupported OTEL_TRACES_EXPORTER") {
		t.Fatalf("run() error = %v, want invalid trace exporter configuration", err)
	}
}

func TestRunReturnsDatabaseInitializationError(t *testing.T) {
	t.Setenv("OTEL_TRACES_EXPORTER", "none")
	t.Setenv("OTEL_METRICS_EXPORTER", "none")
	t.Setenv("OTEL_LOGS_EXPORTER", "none")
	t.Setenv("DATABASE_PATH", filepath.Join(t.TempDir(), "missing", "incidentlens.db"))

	if err := run(); err == nil {
		t.Fatal("run() error = nil for an invalid database path")
	}
}

func TestRunReturnsKubernetesConfigurationError(t *testing.T) {
	t.Setenv("OTEL_TRACES_EXPORTER", "none")
	t.Setenv("OTEL_METRICS_EXPORTER", "none")
	t.Setenv("OTEL_LOGS_EXPORTER", "none")
	t.Setenv("DATABASE_PATH", filepath.Join(t.TempDir(), "incidents.db"))
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")

	err := run()
	if err == nil || !strings.Contains(err.Error(), "KUBERNETES_SERVICE_HOST") {
		t.Fatalf("run() error = %v, want in-cluster client configuration error", err)
	}
}
