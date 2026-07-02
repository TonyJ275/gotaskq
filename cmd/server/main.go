package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/TonyJ275/gotaskq/internal/api"
	"github.com/TonyJ275/gotaskq/internal/db"
)

func main() {
	ctx := context.Background()

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

	jobRepo := db.NewJobRepository(pool)
	handler := api.NewHandler(jobRepo)
	router := api.NewRouter(handler, pool)

	fmt.Println("Server starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", router))
}
