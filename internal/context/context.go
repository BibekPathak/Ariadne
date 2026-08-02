package context

import (
	"kubeai/internal/llm"
)

// EstimateTokens approximates token count (chars / 4). Phase 2 replaces this
// with a proper tokenizer-aware budget manager.
func EstimateTokens(s string) int {
	return (len(s) + 3) / 4
}

// Budget tracks the assembled prompt against a token budget.
type Budget struct {
	Max int
	cur int
}

func (b *Budget) Remaining() int { return b.Max - b.cur }

func (b *Budget) canFit(s string) bool { return b.cur+EstimateTokens(s) <= b.Max }

func (b *Budget) Add(s string) { b.cur += EstimateTokens(s) }

// Input is everything the context builder may draw from to assemble a prompt.
type Input struct {
	SystemPrompt string
	Goal         string
	TaskPrompt   string
	Messages     []llm.Message
	MaxTokens    int
}

// Builder assembles the final prompt sent to the LLM. Phase 1 keeps the
// policy simple: keep system + goal + task, then keep the most recent history
// that fits the token budget, dropping the oldest messages first. Phase 2
// replaces this with ranking, compression and retrieval.
type Builder struct{}

func New() *Builder { return &Builder{} }

func (b *Builder) Build(in Input) *llm.Request {
	if in.MaxTokens <= 0 {
		in.MaxTokens = 16_000
	}
	budget := &Budget{Max: in.MaxTokens}

	messages := make([]llm.Message, 0, len(in.Messages)+3)
	if in.SystemPrompt != "" {
		messages = append(messages, llm.Message{Role: llm.RoleSystem, Content: in.SystemPrompt})
		budget.Add(in.SystemPrompt)
	}

	goalMsg := llm.Message{Role: llm.RoleUser, Content: in.Goal}
	if in.TaskPrompt != "" {
		goalMsg.Content += "\n\nTASK\n" + in.TaskPrompt
	}
	messages = append(messages, goalMsg)
	budget.Add(goalMsg.Content)

	// Walk history from the newest backwards, keeping as much as fits.
	var kept []llm.Message
	for i := len(in.Messages) - 1; i >= 0; i-- {
		m := in.Messages[i]
		if !budget.canFit(m.Content) {
			break
		}
		budget.Add(m.Content)
		kept = append(kept, m)
	}
	// Restore chronological order.
	for i := len(kept) - 1; i >= 0; i-- {
		messages = append(messages, kept[i])
	}

	return &llm.Request{Messages: messages}
}
