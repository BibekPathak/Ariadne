package transport

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTaskMessageRoundTrip(t *testing.T) {
	in := &TaskMessage{
		TaskID: "agent1_r1_analyze", WorkflowID: "wf1", AgentID: "agent1",
		Name: "analyze", Type: "analyze",
		Inputs:  map[string]any{"goal": "build api", "repo_path": "./demo/repo"},
		Attempt: 1, MaxAttempt: 2,
		Deadline: time.Now().UTC().Add(time.Hour), TraceID: "trace-1",
	}
	raw, err := Encode(in)
	if err != nil {
		t.Fatal(err)
	}
	var out TaskMessage
	if err := Decode(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.TaskID != in.TaskID || out.Type != in.Type || out.Attempt != 1 || out.MaxAttempt != 2 {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
	if out.Inputs["goal"] != "build api" {
		t.Fatalf("inputs lost: %+v", out.Inputs)
	}
	if !out.Deadline.Equal(in.Deadline) {
		t.Fatalf("deadline lost: %v != %v", out.Deadline, in.Deadline)
	}
}

func TestTaskMessageHasNoPromptField(t *testing.T) {
	// Guard rail: the wire contract must never carry LLM prompts. Workers build
	// prompts locally from Inputs.
	var m map[string]any
	_ = json.Unmarshal([]byte(`{}`), &m)
	raw, _ := Encode(&TaskMessage{Inputs: map[string]any{"goal": "x"}})
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	for _, forbidden := range []string{"prompt", "system_prompt", "messages"} {
		if _, ok := got[forbidden]; ok {
			t.Fatalf("TaskMessage must not carry %q", forbidden)
		}
	}
	if _, ok := got["inputs"].(map[string]any)["prompt"]; ok {
		t.Fatal("inputs must not carry prompts")
	}
}
