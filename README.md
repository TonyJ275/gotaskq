# GoTaskQ

A production-grade distributed task queue built in Go, backed by PostgreSQL. Designed for reliability, observability, and concurrent job processing at scale.

[![CI](https://github.com/TonyJ275/gotaskq/actions/workflows/ci.yml/badge.svg)](https://github.com/TonyJ275/gotaskq/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/TonyJ275/gotaskq)](go.mod)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

---

## What is GoTaskQ?

GoTaskQ is a background job processing system — the same category of infrastructure as Sidekiq, Celery, and BullMQ. Applications submit jobs via a REST API, and a pool of concurrent workers picks them up, processes them, and handles failures automatically.

Built to solve real distributed systems problems:
- **How do you prevent two workers from processing the same job?** → PostgreSQL `SELECT FOR UPDATE SKIP LOCKED`
- **What happens when a job fails?** → Exponential backoff retry with configurable limits
- **What happens when a worker crashes mid-job?** → Watchdog detects stale jobs and requeues them
- **How do you shut down without losing work?** → Graceful shutdown with context cancellation
- **How do you know the system is healthy?** → Prometheus metrics + `/health` endpoint

---

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                      Client                             │
│              POST /jobs  GET /jobs  GET /stats          │
└────────────────────────┬────────────────────────────────┘
                         │ HTTP
                         ▼
┌──────────────────────────────────────────────────────────┐
│                   Go Application                         │
│                                                          │
│   ┌─────────────┐    ┌──────────────────────────────┐    │
│   │  REST API   │    │        Worker Pool           │    │
│   │             │    │                              │    │
│   │ POST /jobs  │    │  ┌────────┐  ┌────────┐      │    │
│   │ GET  /jobs  │    │  │Worker 0│  │Worker 1│ ...  │    │
│   │ GET  /stats │    │  └────────┘  └────────┘      │    │
│   │ DEL  /jobs  │    │                              │    │
│   └──────┬──────┘    └──────────────┬───────────────┘    │
│          │                          │                    │
│          │           ┌──────────────┐                    │
│          │           │   Watchdog   │                    │
│          │           │ (stale jobs) │                    │
│          │           └──────────────┘                    │
└──────────┼───────────────────────────────────────────────┘
           │ pgxpool
           ▼
┌─────────────────────────────────────────────────────────┐
│                    PostgreSQL                           │
│                                                         │
│   jobs table                                            │
│   ┌──────────────────────────────────────────────────┐  │
│   │ id │ type │ payload │ status │ priority │ ...    │  │
│   └──────────────────────────────────────────────────┘  │
│                                                         │
│   Partial Index: WHERE status = 'pending'               │
│   ORDER BY priority DESC, scheduled_at ASC              │
└─────────────────────────────────────────────────────────┘
           │
           ▼
┌─────────────────────────────────────────────────────────┐
│                   Prometheus                            │
│         Scrapes /metrics every 15 seconds               │
└─────────────────────────────────────────────────────────┘
```

### Job Lifecycle

```
Submit
  │
  ▼
pending ──► running ──► completed
              │
              ▼
           failed
              │
         retry_count < max_retries?
                │
                │
                │
                ▼
            Yes   No
             │    │
             ▼    ▼
        pending  dead
        (backoff)
```

### Key Design Decisions

**Why PostgreSQL instead of Redis for the queue?**

PostgreSQL provides ACID guarantees — jobs are never lost even if the system crashes mid-write. Redis is faster but volatile. For a task queue where reliability matters more than raw throughput, PostgreSQL is the correct trade-off. The `SELECT FOR UPDATE SKIP LOCKED` pattern gives atomic job claiming without any application-level locking.

**Why `SELECT FOR UPDATE SKIP LOCKED`?**

When multiple workers poll simultaneously, `SKIP LOCKED` ensures each worker atomically claims a different job — if a row is already locked by another worker, it's skipped entirely. This eliminates duplicate processing without coordination overhead. The same pattern is used in production by Shopify and GitHub.

**Why exponential backoff?**

If a job fails because a downstream service is overloaded, retrying immediately makes the problem worse. Exponential backoff (2^n seconds) gives dependencies time to recover: 1s → 2s → 4s → 8s.

**Why a watchdog?**

If a worker crashes mid-job, the job stays in `running` state forever. The watchdog runs every minute and resets jobs stuck in `running` for more than 5 minutes back to `pending`. This guarantees at-least-once delivery — every job runs at least once, even across crashes.

---

## Tech Stack

| Component | Technology | Purpose |
|-----------|-----------|---------|
| Language | Go 1.26 | Concurrency primitives, performance |
| Database | PostgreSQL 17 | Durable job storage, atomic claiming |
| Driver | pgx/v5 + pgxpool | Connection pooling, performance |
| Migrations | golang-migrate | Schema versioning |
| Metrics | Prometheus client | Observability |
| Containers | Docker + Compose | Local development, deployment |
| CI/CD | GitHub Actions | Automated testing and image publishing |
| Registry | GitHub Container Registry | Docker image hosting |

---

## Getting Started

### Prerequisites

- [Docker Desktop](https://www.docker.com/products/docker-desktop/)
- [Go 1.26+](https://golang.org/dl/) (for local development)

### Run with Docker Compose

```bash
git clone https://github.com/TonyJ275/gotaskq.git
cd gotaskq
docker-compose up --build
```

This starts:
- **GoTaskQ** on `http://localhost:8080`
- **PostgreSQL** on `localhost:5432`
- **Prometheus** on `http://localhost:9090`

Migrations run automatically on startup.

### Run Locally

```bash
# Start PostgreSQL
docker-compose up postgres -d

# Run migrations
migrate -path migrations -database "postgres://gotaskq_user:gotaskq_pass@localhost:5432/gotaskq?sslmode=disable" up

# Start the application
go run cmd/server/main.go
```

### Pull from Registry

```bash
docker pull ghcr.io/TonyJ275/gotaskq:latest
```

---

## API Reference

### Submit a Job

```
POST /jobs
```

**Request:**
```json
{
  "type": "send_email",
  "payload": {
    "to": "user@example.com",
    "subject": "Welcome"
  },
  "priority": 5,
  "max_retries": 3,
  "scheduled_at": "2026-07-10T10:00:00Z"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | ✅ | Job handler to invoke |
| `payload` | object | ✅ | Arbitrary data passed to handler |
| `priority` | int | ❌ | Higher = processed first (default: 0) |
| `max_retries` | int | ❌ | Max retry attempts (default: 3) |
| `scheduled_at` | timestamp | ❌ | Delay execution until this time |

**Response `201 Created`:**
```json
{
  "id": "572c1a8a-7c17-4560-8776-1e70933008a1",
  "type": "send_email",
  "payload": {"to": "user@example.com", "subject": "Welcome"},
  "status": "pending",
  "priority": 5,
  "max_retries": 3,
  "retry_count": 0,
  "error_message": null,
  "scheduled_at": "2026-07-10T10:00:00Z",
  "started_at": null,
  "completed_at": null,
  "created_at": "2026-07-03T10:30:34Z",
  "updated_at": "2026-07-03T10:30:34Z"
}
```

---

### Get a Job

```
GET /jobs/{id}
```

**Response `200 OK`:** Job object as above.

**Response `404 Not Found`:**
```json
{"error": "job not found"}
```

---

### List Jobs

```
GET /jobs?status=pending&limit=10&offset=0
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `status` | `pending` | Filter by status: `pending`, `running`, `completed`, `failed`, `dead` |
| `limit` | `10` | Number of results |
| `offset` | `0` | Pagination offset |

**Response `200 OK`:** Array of job objects.

---

### Cancel a Job

```
DELETE /jobs/{id}
```

Only cancels jobs in `pending` state.

**Response `200 OK`:**
```json
{"message": "job cancelled"}
```

**Response `400 Bad Request`:**
```json
{"error": "job not found or not in pending state"}
```

---

### Get Queue Stats

```
GET /stats
```

**Response `200 OK`:**
```json
{
  "pending": 42,
  "running": 5,
  "completed": 1203,
  "failed": 12,
  "dead": 3
}
```

---

### Health Check

```
GET /health
```

Returns `200 ok` if the application and database are healthy.
Returns `503 Service Unavailable` if the database is unreachable.

---

### Prometheus Metrics

```
GET /metrics
```

| Metric | Type | Description |
|--------|------|-------------|
| `gotaskq_jobs_enqueued_total` | Counter | Total jobs submitted |
| `gotaskq_jobs_completed_total` | Counter | Total jobs completed successfully |
| `gotaskq_jobs_failed_total` | Counter | Total jobs that failed |
| `gotaskq_jobs_dead_total` | Counter | Total jobs moved to dead letter queue |
| `gotaskq_job_duration_seconds` | Histogram | Job processing duration |
| `gotaskq_queue_depth{status}` | Gauge | Current jobs per status |
| `gotaskq_active_workers` | Gauge | Workers currently processing jobs |

---

## Project Structure

```
gotaskq/
├── cmd/
│   └── server/
│       └── main.go              # Entry point, wiring, graceful shutdown
├── internal/
│   ├── api/
│   │   ├── handlers.go          # HTTP request handlers
│   │   ├── handlers_test.go     # Handler unit tests (mock store)
│   │   ├── router.go            # Route registration
│   │   └── router_test.go       # Router tests
│   ├── db/
│   │   ├── postgres.go          # Connection pool setup
│   │   ├── store.go             # JobStore interface
│   │   ├── job_repository.go    # PostgreSQL implementation
│   │   ├── job_repository_test.go # Integration tests (testcontainers)
│   │   └── mocks/
│   │       └── mock_store.go    # Mock implementation for tests
│   ├── metrics/
│   │   └── prometheus.go        # Prometheus metrics (singleton)
│   ├── model/
│   │   ├── job.go               # Job struct, JobResponse, ToResponse()
│   │   └── job_test.go          # Model tests
│   └── worker/
│       ├── pool.go              # Worker pool, SELECT FOR UPDATE SKIP LOCKED
│       ├── pool_test.go         # Worker pool tests
│       ├── watchdog.go          # Stale job recovery
│       └── watchdog_test.go     # Watchdog tests
├── migrations/
│   ├── 000001_create_jobs_table.up.sql
│   └── 000001_create_jobs_table.down.sql
├── .github/
│   └── workflows/
│       └── ci.yml               # CI/CD pipeline
├── docker-compose.yml
├── Dockerfile                   # Multi-stage build
├── prometheus.yml               # Prometheus scrape config
├── go.mod
└── go.sum
```

---

## Testing

```bash
# Run all tests
go test ./... -v

# Run with coverage
go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out

# Run specific package
go test ./internal/api/... -v
go test ./internal/worker/... -v
```

**Coverage:**

| Package | Coverage |
|---------|----------|
| `internal/api` | 97.6% |
| `internal/model` | 100% |
| `internal/worker` | 90.1% |
| `internal/db` | 80.6% |

Integration tests use [testcontainers-go](https://golang.testcontainers.org/) — a real PostgreSQL instance spins up automatically, no manual setup required.

---

## CI/CD Pipeline

Every push and pull request triggers:

```
Push / PR
    │
    ▼
Build & Test
  ├── go mod tidy (dependency cleanliness)
  ├── go build ./...
  ├── go vet ./...
  └── go test ./... (with coverage)
    │
    │ (only if tests pass)
    ▼
Docker
  ├── Build image
  └── Push to GHCR (main branch only)
        ghcr.io/TonyJ275/gotaskq:latest
        ghcr.io/TonyJ275/gotaskq:sha-<commit>
```

---

## Observability

**Health check:**
```bash
curl http://localhost:8080/health
```

**Queue stats:**
```bash
curl http://localhost:8080/stats
```

**Prometheus UI:**
Open `http://localhost:9090` and query:
```
gotaskq_jobs_enqueued_total
gotaskq_queue_depth
rate(gotaskq_jobs_completed_total[5m])
```

---

## Contributing

Contributions are welcome! Feel free to open issues or submit pull requests.

---

## License

MIT License — see [LICENSE](LICENSE) for details.
