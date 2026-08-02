package context

import (
	"sort"
	"strings"

	"adriane/internal/llm"
)

// EstimateTokens approximates token count (chars / 4). A tokenizer-aware
// budget manager is a later-phase refinement.
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

// MemoryItem is a retrieved memory candidate handed to the builder. Score is
// the semantic relevance in [0,1]; 0 means neutral (recency-ranked).
type MemoryItem struct {
	Kind    string
	Topic   string
	Content string
	Score   float32
}

// Input is everything the context builder may draw from.
type Input struct {
	SystemPrompt string
	Goal         string
	TaskPrompt   string
	Messages     []llm.Message
	Memories     []MemoryItem
	Plan         []string
	Artifacts    []string
	MaxTokens    int
}

type component struct {
	kind    string
	content string
	score   float64
	seq     int
}

// Builder assembles the final prompt via a rank -> compress -> budget
// pipeline. Components (memories, plan, artifacts) compete for the token
// budget; higher-scoring components win over older conversation history.
type Builder struct{}

func New() *Builder { return &Builder{} }

const maxCompressLen = 4000

func (b *Builder) Build(in Input) *llm.Request {
	if in.MaxTokens <= 0 {
		in.MaxTokens = 16_000
	}
	budget := &Budget{Max: in.MaxTokens}

	// Mandatory core: system + goal/task. These always fit or are included.
	core := ""
	if in.SystemPrompt != "" {
		core += in.SystemPrompt
	}
	goalMsg := in.Goal
	if in.TaskPrompt != "" {
		goalMsg += "\n\nTASK\n" + in.TaskPrompt
	}

	// Rank components.
	comps := b.components(in)
	sort.SliceStable(comps, func(i, j int) bool { return comps[i].score > comps[j].score })
	comps = dedupe(comps)

	// Compress and budget the components into a context section.
	var sections []string
	for _, c := range comps {
		content := compress(c.content, maxCompressLen)
		if !budget.canFit(content) {
			continue
		}
		budget.Add(content)
		sections = append(sections, formatSection(c, content))
	}

	systemContent := core
	if len(sections) > 0 {
		systemContent += "\n\n" + strings.Join(sections, "\n\n")
	}
	budget.Add(systemContent)

	// History: newest first, keep as much as fits.
	var kept []llm.Message
	for i := len(in.Messages) - 1; i >= 0; i-- {
		m := in.Messages[i]
		if !budget.canFit(m.Content) {
			break
		}
		budget.Add(m.Content)
		kept = append(kept, m)
	}

	messages := make([]llm.Message, 0, len(kept)+2)
	if systemContent != "" {
		messages = append(messages, llm.Message{Role: llm.RoleSystem, Content: systemContent})
	}
	messages = append(messages, llm.Message{Role: llm.RoleUser, Content: goalMsg})
	// kept is newest-first; append in reverse for chronological order.
	for i := len(kept) - 1; i >= 0; i-- {
		messages = append(messages, kept[i])
	}
	return &llm.Request{Messages: messages}
}

func (b *Builder) components(in Input) []component {
	var comps []component
	for i, m := range in.Memories {
		// Semantic relevance dominates; kind and recency break ties.
		score := float64(m.Score) * 10
		switch m.Kind {
		case "preference", "outcome", "failure":
			score += 2
		case "artifact":
			score += 1
		}
		score += float64(len(in.Memories)-i) * 0.1 // recency within the batch
		comps = append(comps, component{kind: "memory", content: m.Content, score: score, seq: i})
	}
	for i, p := range in.Plan {
		comps = append(comps, component{kind: "plan", content: p, score: 5, seq: i})
	}
	for i, a := range in.Artifacts {
		comps = append(comps, component{kind: "artifact", content: a, score: 3, seq: i})
	}
	return comps
}

func dedupe(comps []component) []component {
	seen := map[string]bool{}
	var out []component
	for _, c := range comps {
		if seen[c.content] {
			continue
		}
		seen[c.content] = true
		out = append(out, c)
	}
	return out
}

func compress(s string, max int) string {
	if len(s) <= max {
		return s
	}
	half := (max - 3) / 2
	return s[:half] + "..." + s[len(s)-half:]
}

func formatSection(c component, content string) string {
	switch c.kind {
	case "memory":
		return "RECOLLECTION: " + content
	case "plan":
		return "PLAN: " + content
	case "artifact":
		return "ARTIFACT: " + content
	default:
		return content
	}
}
