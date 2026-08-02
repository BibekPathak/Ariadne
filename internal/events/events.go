package events

import (
	"context"
	"time"
)

type Type string

const (
	AgentCreated    Type = "agent_created"
	PlanCreated     Type = "plan_created"
	TaskStarted     Type = "task_started"
	TaskFinished    Type = "task_finished"
	TaskFailed      Type = "task_failed"
	RetryScheduled  Type = "retry_scheduled"
	ToolCalled      Type = "tool_called"
	ToolFinished    Type = "tool_finished"
	LLMCalled       Type = "llm_called"
	WorkerHeartbeat Type = "worker_heartbeat"
	MemoryRetrieved Type = "memory_retrieved"
	MemorySaved     Type = "memory_saved"
	AgentCompleted  Type = "agent_completed"
	AgentFailed     Type = "agent_failed"
)

type Event struct {
	Seq     int64          `json:"seq"`
	AgentID string         `json:"agent_id"`
	TaskID  string         `json:"task_id,omitempty"`
	Type    Type           `json:"type"`
	Payload map[string]any `json:"payload"`
	TS      time.Time      `json:"ts"`
}

func New(agentID, taskID string, t Type, payload map[string]any) Event {
	return Event{
		AgentID: agentID,
		TaskID:  taskID,
		Type:    t,
		Payload: payload,
		TS:      time.Now().UTC(),
	}
}

// Publisher is the minimal event output a component needs. Workers and the
// control plane both publish; only the control plane subscribes.
type Publisher interface {
	Publish(ctx context.Context, e Event) error
}

// EventBus is the event-sourcing seam. Phase 4 adds a NATS JetStream
// implementation for the distributed data plane.
type EventBus interface {
	Publisher
	// Subscribe returns a receive-only channel and a cancel function.
	Subscribe() (<-chan Event, func())
	Close() error
}
