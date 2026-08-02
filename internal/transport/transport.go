// Package transport defines the stable wire contract between the control
// plane and remote workers. Messages are plain JSON over NATS JetStream.
// Tasks are described structurally (template + inputs) — never as LLM
// prompts; workers resolve context, memory and prompts locally.
package transport

import (
	"encoding/json"
	"time"
)

// Subjects for the distributed data plane.
const (
	SubjectDispatch   = "tasks.dispatch"
	SubjectResult     = "tasks.result"
	SubjectClaim      = "tasks.claim"
	SubjectHeartbeat  = "workers.heartbeat"
	SubjectEvents     = "events"
	DispatchQueue     = "workers" // queue group: one worker claims each task
	HeartbeatInterval = 2 * time.Second
)

// TaskMessage is what the scheduler sends a worker to run one node.
type TaskMessage struct {
	TaskID     string         `json:"task_id"`
	WorkflowID string         `json:"workflow_id"` // run-scoped DAG id
	AgentID    string         `json:"agent_id"`    // stable agent id (memory key)
	Name       string         `json:"name"`
	Type       string         `json:"type"` // task template
	Inputs     map[string]any `json:"inputs"`
	Attempt    int            `json:"retry"`
	MaxAttempt int            `json:"max_attempt"`
	Deadline   time.Time      `json:"deadline"`
	TraceID    string         `json:"trace_id"`
}

// ResultMessage is what a worker sends back for a completed (or failed) task.
type ResultMessage struct {
	TaskID   string         `json:"task_id"`
	WorkerID string         `json:"worker_id"`
	Attempt  int            `json:"attempt"`
	Outputs  map[string]any `json:"outputs"`
	Error    string         `json:"error,omitempty"`
}

// ClaimMessage announces that a worker has taken ownership of a task.
type ClaimMessage struct {
	TaskID   string `json:"task_id"`
	WorkerID string `json:"worker_id"`
	Attempt  int    `json:"attempt"`
}

// HeartbeatMessage keeps the control plane aware that a worker is alive.
type HeartbeatMessage struct {
	WorkerID string    `json:"worker_id"`
	TS       time.Time `json:"ts"`
}

func Encode(v any) ([]byte, error) { return json.Marshal(v) }

func Decode(raw []byte, v any) error { return json.Unmarshal(raw, v) }
