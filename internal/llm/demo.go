package llm

import (
	"context"
	"strings"
)

// DemoProvider is the offline stand-in used when no API key is configured. It
// is a deterministic, scripted agent that exercises the real tool pipeline
// (write_file, shell, git) inside the real sandbox, so `make demo` shows a
// genuine end-to-end run without network access. It understands the stock
// coder goal format used by the demo repo.
type DemoProvider struct{}

func (DemoProvider) Name() string { return "demo-scripted" }

func (p DemoProvider) Generate(ctx context.Context, req Request) (*Response, error) {
	// If the last message is a tool result, the agent is done: summarize.
	if len(req.Messages) > 0 && req.Messages[len(req.Messages)-1].Role == RoleTool {
		return &Response{Content: "Work verified; reporting results."}, nil
	}
	prompt := lastUserContent(req.Messages)

	switch {
	case strings.Contains(prompt, "Run the project's test suite"):
		// test template: run the suite inside the sandbox.
		return &Response{ToolCalls: []ToolCall{{
			ID:       "call_test",
			Function: Function{Name: "shell", Arguments: `{"command":"cd /repo && go test ./... 2>&1"}`},
		}}}, nil
	case strings.Contains(prompt, "Implement"):
		// implement template: add the Subtract function and its test, then build.
		return &Response{ToolCalls: []ToolCall{
			{ID: "call_write", Function: Function{Name: "write_file", Arguments: `{"path":"mathx/mathx.go","content":"package mathx\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n\nfunc Subtract(a, b int) int {\n\treturn a - b\n}\n"}`}},
			{ID: "call_writetest", Function: Function{Name: "write_file", Arguments: `{"path":"mathx/mathx_test.go","content":"package mathx\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif got := Add(2, 3); got != 5 {\n\t\tt.Fatalf(\"Add(2,3) = %d, want 5\", got)\n\t}\n}\n\nfunc TestSubtract(t *testing.T) {\n\tif got := Subtract(5, 3); got != 2 {\n\t\tt.Fatalf(\"Subtract(5,3) = %d, want 2\", got)\n\t}\n}\n"}`}},
			{ID: "call_build", Function: Function{Name: "shell", Arguments: `{"command":"cd /repo && go build ./... 2>&1"}`}},
		}}, nil
	case strings.Contains(prompt, "Analyze") || strings.Contains(prompt, "analyze"):
		return &Response{Content: "Repository inspected: a single Go module with a mathx package."}, nil
	default:
		return &Response{Content: "Done."}, nil
	}
}

func lastUserContent(msgs []Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == RoleUser {
			return msgs[i].Content
		}
	}
	return ""
}
