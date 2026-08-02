package agents

import "time"

type SandboxPolicy struct {
	Network bool
	CPU     string
	MemMB   int
	Image   string
}

type MemoryPolicy struct {
	UseShortTerm bool
	UseLongTerm  bool
	UseSemantic  bool
	ShortTermTTL time.Duration
}

// AgentTemplate defines the personality and permissions of an agent type.
// Each template owns its planner prompt, allowed tools, sandbox policy and
// memory policy. Phase 1 ships the full coder template plus placeholders for
// the others.
type AgentTemplate struct {
	Name          string
	Description   string
	PlannerPrompt string
	AllowedTools  []string
	Sandbox       SandboxPolicy
	Memory        MemoryPolicy
	DefaultPlan   []string // task template names used by the static fallback
}

func defaultTemplates() map[string]AgentTemplate {
	return map[string]AgentTemplate{
		"coder": {
			Name:        "coder",
			Description: "Implements and verifies changes against a repository.",
			PlannerPrompt: "You plan coding work. Produce a DAG of tasks using the available templates " +
				"to analyze, implement, test and review changes against the goal.",
			AllowedTools: []string{"list_files", "read_file", "write_file", "shell", "git"},
			Sandbox:      SandboxPolicy{CPU: "1", MemMB: 1024, Image: "ubuntu:22.04"},
			Memory:       MemoryPolicy{UseShortTerm: true},
			DefaultPlan:  []string{"analyze", "implement", "test"},
		},
		"researcher": {
			Name:          "researcher",
			Description:   "Searches information and synthesizes reports. Placeholder for Phase 3.",
			PlannerPrompt: "You plan research work. Produce a DAG of tasks to gather, read and synthesize information.",
			AllowedTools:  []string{"http_get", "shell"},
			Sandbox:       SandboxPolicy{Network: true, CPU: "1", MemMB: 512, Image: "ubuntu:22.04"},
			Memory:        MemoryPolicy{UseShortTerm: true},
			DefaultPlan:   []string{"analyze"},
		},
		"reviewer": {
			Name:          "reviewer",
			Description:   "Reviews code and produces feedback. Placeholder for Phase 3.",
			PlannerPrompt: "You plan review work. Produce a DAG of tasks to inspect code and report findings.",
			AllowedTools:  []string{"read_file", "list_files", "shell", "git"},
			Sandbox:       SandboxPolicy{CPU: "1", MemMB: 512, Image: "ubuntu:22.04"},
			Memory:        MemoryPolicy{UseShortTerm: true},
			DefaultPlan:   []string{"review"},
		},
		"refactorer": {
			Name:          "refactorer",
			Description:   "Refactors codebases. Placeholder for Phase 3.",
			PlannerPrompt: "You plan refactoring work. Produce a DAG of tasks to analyze, refactor and verify.",
			AllowedTools:  []string{"read_file", "write_file", "shell", "git"},
			Sandbox:       SandboxPolicy{CPU: "1", MemMB: 1024, Image: "ubuntu:22.04"},
			Memory:        MemoryPolicy{UseShortTerm: true},
			DefaultPlan:   []string{"analyze", "implement", "test"},
		},
		"security_auditor": {
			Name:          "security_auditor",
			Description:   "Audits code for vulnerabilities. Placeholder for Phase 3.",
			PlannerPrompt: "You plan security audits. Produce a DAG of tasks to inspect code and report risks.",
			AllowedTools:  []string{"read_file", "list_files", "shell", "git", "http_get"},
			Sandbox:       SandboxPolicy{Network: true, CPU: "1", MemMB: 512, Image: "ubuntu:22.04"},
			Memory:        MemoryPolicy{UseShortTerm: true},
			DefaultPlan:   []string{"analyze", "review"},
		},
		"data_analyst": {
			Name:          "data_analyst",
			Description:   "Analyzes datasets and produces reports. Placeholder for Phase 3.",
			PlannerPrompt: "You plan data analysis work. Produce a DAG of tasks to load, explore and summarize data.",
			AllowedTools:  []string{"shell", "read_file", "write_file"},
			Sandbox:       SandboxPolicy{CPU: "1", MemMB: 1024, Image: "ubuntu:22.04"},
			Memory:        MemoryPolicy{UseShortTerm: true},
			DefaultPlan:   []string{"analyze"},
		},
	}
}

type TemplateRegistry struct {
	templates map[string]AgentTemplate
}

func NewTemplateRegistry() *TemplateRegistry {
	return &TemplateRegistry{templates: defaultTemplates()}
}

func (r *TemplateRegistry) Get(name string) (AgentTemplate, bool) {
	t, ok := r.templates[name]
	return t, ok
}

func (r *TemplateRegistry) Names() []string {
	var out []string
	for k := range r.templates {
		out = append(out, k)
	}
	return out
}
