package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/iwantm/IncidentLens/internal/config"
	"github.com/iwantm/IncidentLens/internal/kube"
	"github.com/iwantm/IncidentLens/internal/server"
	gorm "github.com/iwantm/IncidentLens/internal/storage"
	"github.com/iwantm/IncidentLens/internal/telemetry"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	shutdownTelemetry, err := telemetry.Setup(context.Background(), "incident-lens-recorder")
	if err != nil {
		slog.Error("OpenTelemetry initialization failed", "error", err)
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTelemetry(shutdownCtx); err != nil {
			slog.Error("OpenTelemetry shutdown failed", "error", err)
		}
	}()

	cfg := config.Load()

	db, err := gorm.Open(cfg.DatabasePath)

	if err != nil {
		slog.Error("database initialization failed", "database_path", cfg.DatabasePath, "error", err)
		return err
	}

	eventStore := gorm.NewEventStore(db)

	kubeClient, err := kube.NewClient()
	if err != nil {
		slog.Error("Kubernetes client initialization failed", "error", err)
		return err
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	watcher := kube.NewWatcher(kubeClient, cfg.WatchNamespace, eventStore)

	watcherErr := make(chan error, 1)
	go func() {
		watcherErr <- watcher.Run(ctx)
	}()
	slog.Info("Kubernetes watcher initializing", "namespace", cfg.WatchNamespace)

	server := server.NewServer(eventStore, func(context.Context) error {
		if !watcher.Ready() {
			return errors.New("Kubernetes watcher caches are not synchronized")
		}
		return nil
	})

	httpServer := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: otelhttp.NewHandler(server.Handler(), "incident-lens.http"),
	}

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("HTTP server starting", "address", httpServer.Addr)
		serverErr <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
	case err := <-watcherErr:
		if err != nil {
			slog.Error("Kubernetes watcher stopped unexpectedly", "error", err)
			stop()
			_ = httpServer.Close()
			return err
		}
		stop()
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP server stopped unexpectedly", "error", err)
			stop()
			return err
		}
		stop()
	}

	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("HTTP server shutdown failed", "error", err)
		return err
	}
	return nil

}
