package worker

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	ctxbuilder "adriane/internal/context"
	"adriane/internal/events"
	"adriane/internal/llm"
	"adriane/internal/memory"
	"adriane/internal/workflow"
)

// loadMemory gathers short-term, long-term and (optionally) semantic memories
// for the agent and returns them as ranked context items. The goal is the
// query: its significant words drive long-term topic matching and semantic
// search.
func (w *Worker) loadMemory(ctx context.Context, node *workflow.Node) []ctxbuilder.MemoryItem {
	if w.memory == nil {
		return nil
	}
	goal, _ := node.Inputs["goal"].(string)
	topics := topicsFrom(goal)

	var items []ctxbuilder.MemoryItem
	nShort, nLong, nSem := 0, 0, 0

	if st, err := w.memory.LoadShortTerm(ctx, node.AgentID, 20); err == nil {
		nShort = len(st)
		for _, e := range st {
			items = append(items, itemFromEntry(e))
		}
	}

	if lt, err := w.memory.LoadLongTerm(ctx, node.AgentID, topics, 8); err == nil {
		nLong = len(lt)
		for _, e := range lt {
			items = append(items, itemFromEntry(e))
		}
	}

	if w.useSemantic {
		if sem, err := w.memory.SearchSemantic(ctx, node.AgentID, goal, 6); err == nil {
			nSem = len(sem)
			for _, e := range sem {
				items = append(items, itemFromEntry(e))
			}
		}
	}

	_ = w.bus.Publish(ctx, events.New(node.AgentID, node.ID, events.MemoryRetrieved, map[string]any{
		"short_term": nShort, "long_term": nLong, "semantic": nSem,
	}))
	return items
}

// storeMemory records what the agent learned from this task across all tiers.
func (w *Worker) storeMemory(ctx context.Context, node *workflow.Node, recalled []ctxbuilder.MemoryItem,
	outputs map[string]any, transcript []llm.Message, runErr error) {
	if w.memory == nil {
		return
	}
	goal, _ := node.Inputs["goal"].(string)
	topics := topicsFrom(goal)
	topic := strings.Join(topics, " ")

	status := "completed"
	kind := memory.KindOutcome
	if runErr != nil {
		status = "failed"
		kind = memory.KindFailure
	}
	final := ""
	if outputs != nil {
		if f, ok := outputs["final_message"].(string); ok {
			final = f
		}
	}
	summary := fmt.Sprintf("Task %s (%s) for goal %q: %s. Final: %s",
		node.Name, node.Template, goal, status, final)

	// Long-term: durable outcome for future runs.
	if topic != "" {
		_ = w.memory.StoreLongTerm(ctx, node.AgentID, memory.Entry{
			AgentID: node.AgentID, Kind: kind, Topic: topic, Content: summary,
		})
	}

	// Semantic: index the outcome and notable tool observations.
	if w.useSemantic {
		_ = w.memory.IndexSemantic(ctx, node.AgentID, []memory.IndexItem{
			{AgentID: node.AgentID, Kind: kind, Topic: topic, Content: summary},
		})
	}

	// Short-term: conversation snapshot for the next task in this run.
	if conv := transcriptSummary(transcript); conv != "" {
		_ = w.memory.StoreShortTerm(ctx, node.AgentID, []memory.Entry{
			{AgentID: node.AgentID, Kind: memory.KindConversation, Topic: node.Name, Content: conv},
		})
	}

	_ = w.bus.Publish(ctx, events.New(node.AgentID, node.ID, events.MemorySaved, map[string]any{
		"kind": kind, "recalled": len(recalled),
	}))
}

func itemFromEntry(e memory.Entry) ctxbuilder.MemoryItem {
	return ctxbuilder.MemoryItem{Kind: e.Kind, Topic: e.Topic, Content: e.Content, Score: e.Score}
}

// topicsFrom extracts the significant words of a text for memory retrieval.
func topicsFrom(s string) []string {
	seen := map[string]bool{}
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len(w) >= 4 && !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
		if len(out) >= 8 {
			break
		}
	}
	return out
}

// transcriptSummary condenses a transcript into a short memory of the last
// assistant conclusion.
func transcriptSummary(transcript []llm.Message) string {
	for i := len(transcript) - 1; i >= 0; i-- {
		m := transcript[i]
		if m.Role == llm.RoleAssistant && strings.TrimSpace(m.Content) != "" {
			return strings.TrimSpace(m.Content)
		}
	}
	return ""
}
