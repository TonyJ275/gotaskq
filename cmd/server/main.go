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
	router.(*http.ServeMux).Handle("/metrics", promhttp.Handler())

	fmt.Println("Server starting on :8080")

	go func() {
		if err := http.ListenAndServe(":8080", router); err != nil {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	<-ctx.Done()
	fmt.Println("Shutting down gracefully...")
}
