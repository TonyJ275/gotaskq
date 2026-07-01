CREATE TYPE job_status AS ENUM (
    'pending',
    'running',
    'completed',
    'failed',
    'dead'
);

CREATE TABLE jobs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type            VARCHAR(100) NOT NULL,
    payload         JSONB NOT NULL,
    status          job_status NOT NULL DEFAULT 'pending',
    priority        INT NOT NULL DEFAULT 0,
    max_retries     INT NOT NULL DEFAULT 3,
    retry_count     INT NOT NULL DEFAULT 0,
    error_message   TEXT,
    scheduled_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_jobs_pending 
ON jobs(priority DESC, scheduled_at ASC) 
WHERE status = 'pending';