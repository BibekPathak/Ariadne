CREATE TABLE eval_runs (
    id           TEXT PRIMARY KEY,
    suite        TEXT NOT NULL,
    suite_version INT NOT NULL DEFAULT 1,
    arm          TEXT NOT NULL DEFAULT '',
    git_sha      TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'running',
    summary      JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);
CREATE INDEX idx_eval_runs_suite ON eval_runs(suite, arm, created_at);

CREATE TABLE eval_results (
    id          BIGSERIAL PRIMARY KEY,
    run_id      TEXT NOT NULL REFERENCES eval_runs(id) ON DELETE CASCADE,
    task        TEXT NOT NULL,
    agent_id    TEXT NOT NULL DEFAULT '',
    success     BOOLEAN NOT NULL DEFAULT false,
    pass        BOOLEAN NOT NULL DEFAULT false,
    latency_ms  BIGINT NOT NULL DEFAULT 0,
    cost        DOUBLE PRECISION NOT NULL DEFAULT 0,
    tokens      INT NOT NULL DEFAULT 0,
    tool_errors INT NOT NULL DEFAULT 0,
    checks      JSONB NOT NULL DEFAULT '{}'::jsonb,
    error       TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_eval_results_run ON eval_results(run_id);
