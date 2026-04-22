package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MyAIOSHub/MyTeam/server/internal/events"
	"github.com/MyAIOSHub/MyTeam/server/internal/logger"
	"github.com/MyAIOSHub/MyTeam/server/internal/realtime"
	"github.com/MyAIOSHub/MyTeam/server/internal/skillsbundle"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

func main() {
	logger.Init()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://myteam:myteam@localhost:5432/myteam?sslmode=disable"
	}

	// Connect to database
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("unable to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		slog.Error("unable to ping database", "error", err)
		os.Exit(1)
	}
	slog.Info("connected to database")

	bus := events.New()
	hub := realtime.NewHub()
	go hub.Run()
	registerListeners(bus, hub)

	queries := db.New(pool)

	// Seed the embedded skills + subagents bundle before the HTTP
	// surface comes up. Failures here are hard — an inconsistent
	// bundle is worse than a delayed startup.
	if err := (&skillsbundle.Loader{Queries: queries}).Run(ctx); err != nil {
		slog.Error("skills bundle seed failed", "error", err)
		os.Exit(1)
	}

	// Order matters: subscriber listeners must register BEFORE notification listeners.
	// The notification listener queries the subscriber table to determine recipients,
	// so subscribers must be written first within the same synchronous event dispatch.
	registerSubscriberListeners(bus, queries)
	registerActivityListeners(bus, queries)
	registerNotificationListeners(bus, queries)
	// Memory lifecycle subscribers — phase M of the memory plan.
	// memory.confirmed gates cloud-sync escalation per scope; today
	// it just logs (no separate cloud DB), but the seam is wired.
	registerMemoryListeners(bus, queries, hub)
	// Phase T: bounded worker pool for outbound MyMemo Hub POSTs.
	// No-op when MEMORY_HUB_URL is unset.
	defaultMemoryHubPoster.Start(ctx)

	r := NewRouter(pool, hub, bus)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	// Start background sweeper to mark stale runtimes as offline.
	sweepCtx, sweepCancel := context.WithCancel(context.Background())
	go runRuntimeSweeper(sweepCtx, queries, bus)

	// Graceful shutdown
	go func() {
		slog.Info("server starting", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server")
	sweepCancel()
	defaultMemoryHubPoster.Stop(5 * time.Second)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}
	slog.Info("server stopped")
}
