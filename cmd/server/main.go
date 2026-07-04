package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/TonyJ275/gotaskq/internal/api"
	"github.com/TonyJ275/gotaskq/internal/db"
	"github.com/TonyJ275/gotaskq/internal/metrics"
	"github.com/TonyJ275/gotaskq/internal/worker"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer cancel()

	connString := os.Getenv("DATABASE_URL")
	if connString == "" {
		connString = "postgres://gotaskq_user:gotaskq_pass@localhost:5432/gotaskq?sslmode=disable"
	}

	pool, err := db.NewPool(ctx, connString)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	fmt.Println("Successfully connected to database")

	// Run migrations automatically on startup
	if err := runMigrations(connString); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Initialize metrics
	m := metrics.New()

	// Set up repository
	jobRepo := db.NewJobRepository(pool)

	// Start queue depth tracking
	m.StartQueueDepthTracking(ctx, jobRepo)

	// Set up worker pool
	wp := worker.NewWorkerPool(pool, 5, m)

	wp.RegisterHandler("send_email", func(ctx context.Context, payload []byte) error {
		var data map[string]any
		if err := json.Unmarshal(payload, &data); err != nil {
			return err
		}
		log.Printf("Sending email to: %v", data["to"])
		return nil
	})

	wp.RegisterHandler("process_payment", func(ctx context.Context, payload []byte) error {
		log.Printf("Processing payment...")
		return nil
	})

	// Start watchdog
	watchdog := worker.NewWatchdog(pool)
	go watchdog.Start(ctx)

	// Start worker pool
	go wp.Start(ctx)

	// Set up API
	handler := api.NewHandler(jobRepo, m)
	router := api.NewRouter(handler, pool)

	// Add metrics endpoint to router
	router.Handle("/metrics", promhttp.Handler())

	fmt.Println("Server starting on :8080")

	go func() {
		if err := http.ListenAndServe(":8080", router); err != nil {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	<-ctx.Done()
	fmt.Println("Shutting down gracefully...")
}

func runMigrations(databaseURL string) error {
	m, err := migrate.New(
		"file://migrations",
		databaseURL,
	)
	if err != nil {
		return fmt.Errorf("failed to create migrator: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	fmt.Println("Migrations applied successfully")
	return nil
}
