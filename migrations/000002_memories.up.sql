CREATE TABLE memories (
    id          TEXT PRIMARY KEY,
    agent_id    TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,
    topic       TEXT NOT NULL DEFAULT '',
    content     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_memories_agent ON memories(agent_id, created_at);
CREATE INDEX idx_memories_agent_topic ON memories(agent_id, topic);
