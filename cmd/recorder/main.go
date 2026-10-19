package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/iwantm/IncidentLens/internal/config"
	"github.com/iwantm/IncidentLens/internal/kube"
	"github.com/iwantm/IncidentLens/internal/server"
	gorm "github.com/iwantm/IncidentLens/internal/storage"
)

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}

func run() error {
	cfg := config.Load()

	db, err := gorm.Open(cfg.DatabasePath)

	if err != nil {
		return err
	}

	eventStore := gorm.NewEventStore(db)

	kubeClient, err := kube.NewClient()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	watcher := kube.NewWatcher(kubeClient, "incident-lens", eventStore)

	watcherErr := make(chan error, 1)
	go func() {
		watcherErr <- watcher.Run(ctx)
	}()

	server := server.NewServer(eventStore)

	httpServer := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: server.Handler(),
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
	case err := <-watcherErr:
		if err != nil {
			stop()
			_ = httpServer.Close()
			return err
		}
		stop()
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
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

	return httpServer.Shutdown(shutdownCtx)

}
