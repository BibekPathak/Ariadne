package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"adriane/internal/config"
	"adriane/internal/controlplane"
	"adriane/internal/store"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	ctx := context.Background()

	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		if err := store.Migrate(cfg.DatabaseURL); err != nil {
			logger.Error("migration failed", "err", err)
			os.Exit(1)
		}
		logger.Info("migrations applied")
		return
	}

	cp, err := controlplane.Build(ctx, cfg, logger)
	if err != nil {
		logger.Error("build control plane", "err", err)
		os.Exit(1)
	}
	defer cp.Close()

	api := &api{
		svc:         cp.Agent,
		bus:         cp.Bus,
		logger:      logger,
		runTimeout:  cfg.Timeout,
		metrics:     cp.Stack.Metrics.Handler(),
		auth:        cp.Auth,
		authEnabled: cfg.AuthEnabled,
		limiter:     cp.Limiter,
		leader:      cp.IsLeader,
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           routes(api),
		ReadHeaderTimeout: 10 * time.Second,
	}
	logger.Info("control plane listening", "addr", cfg.HTTPAddr, "worker_mode", cfg.WorkerMode, "auth", cfg.AuthEnabled)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server error", "err", err)
		os.Exit(1)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
