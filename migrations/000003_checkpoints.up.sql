CREATE TABLE checkpoints (
    task_id     TEXT PRIMARY KEY,
    agent_id    TEXT NOT NULL,
    iteration   INT NOT NULL DEFAULT 0,
    messages    JSONB NOT NULL DEFAULT '[]'::jsonb,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
