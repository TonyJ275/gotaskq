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

	"github.com/TonyJ275/gotaskq/internal/api"
	"github.com/TonyJ275/gotaskq/internal/db"
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

	// Set up worker pool with 5 concurrent workers
	wp := worker.NewWorkerPool(pool, 5)

	// Register job handlers
	wp.RegisterHandler("send_email", func(ctx context.Context, payload []byte) error {
		var data map[string]any
		if err := json.Unmarshal(payload, &data); err != nil {
			return err
		}
		log.Printf("Sending email to: %v", data["to"])
		// Simulate work
		return nil
	})

	wp.RegisterHandler("process_payment", func(ctx context.Context, payload []byte) error {
		log.Printf("Processing payment...")
		// Simulate work
		return nil
	})

	// Start watchdog
	watchdog := worker.NewWatchdog(pool)
	go watchdog.Start(ctx)

	// Start worker pool in background
	go wp.Start(ctx)

	// Set up API
	jobRepo := db.NewJobRepository(pool)
	handler := api.NewHandler(jobRepo)
	router := api.NewRouter(handler, pool)

	fmt.Println("Server starting on :8080")

	// Start HTTP server in a goroutine so it doesn't block
	go func() {
		if err := http.ListenAndServe(":8080", router); err != nil {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	// Block until context is cancelled (Ctrl+C or SIGTERM)
	<-ctx.Done()
	fmt.Println("Shutting down gracefully...")
}
