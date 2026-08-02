CREATE TABLE agents (
    id          TEXT PRIMARY KEY,
    template    TEXT NOT NULL,
    goal        TEXT NOT NULL,
    repo_url    TEXT NOT NULL DEFAULT '',
    repo_path   TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'created',
    error       TEXT NOT NULL DEFAULT '',
    meta        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE tasks (
    id          TEXT PRIMARY KEY,
    agent_id    TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    template    TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending',
    depends_on  TEXT[] NOT NULL DEFAULT '{}',
    inputs      JSONB NOT NULL DEFAULT '{}'::jsonb,
    outputs     JSONB NOT NULL DEFAULT '{}'::jsonb,
    attempt     INT NOT NULL DEFAULT 0,
    max_attempt INT NOT NULL DEFAULT 1,
    error       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_tasks_agent ON tasks(agent_id);

CREATE TABLE events (
    seq       BIGSERIAL PRIMARY KEY,
    agent_id  TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    task_id   TEXT NOT NULL DEFAULT '',
    type      TEXT NOT NULL,
    payload   JSONB NOT NULL DEFAULT '{}'::jsonb,
    ts        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_events_agent_seq ON events(agent_id, seq);
CREATE INDEX idx_events_type ON events(type);

CREATE TABLE artifacts (
    id          TEXT PRIMARY KEY,
    agent_id    TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    task_id     TEXT NOT NULL DEFAULT '',
    type        TEXT NOT NULL,
    path        TEXT NOT NULL,
    size        BIGINT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_artifacts_agent ON artifacts(agent_id);
