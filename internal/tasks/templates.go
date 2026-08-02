package tasks

import (
	"fmt"
	"time"
)

// Template describes a reusable unit of agent work. The planner chooses
// templates; templates define the prompt, toolset, timeout, permissions and
// retry policy for that kind of work.
type Template struct {
	Name         string
	Description  string
	Prompt       string
	AllowedTools []string
	Timeout      time.Duration
	Retries      int
	Network      bool
}

var defaultTemplates = map[string]Template{
	"analyze": {
		Name:         "analyze",
		Description:  "Inspect the repository and produce a report.",
		Prompt:       "Analyze the repository in the workspace. Understand its structure, framework and relevant files for the goal. Report findings concisely.",
		AllowedTools: []string{"list_files", "read_file", "shell"},
		Timeout:      5 * time.Minute,
		Retries:      1,
	},
	"implement": {
		Name:         "implement",
		Description:  "Implement changes to the repository to satisfy the goal.",
		Prompt:       "Implement the changes required by the goal in the workspace. Read relevant files, edit or create files, and verify your work. Prefer minimal, focused changes.",
		AllowedTools: []string{"list_files", "read_file", "write_file", "shell", "git"},
		Timeout:      10 * time.Minute,
		Retries:      1,
	},
	"test": {
		Name:         "test",
		Description:  "Run the test suite and report results.",
		Prompt:       "Run the project's test suite and any relevant checks. Report the results and whether everything passes.",
		AllowedTools: []string{"shell", "read_file"},
		Timeout:      10 * time.Minute,
		Retries:      2,
	},
	"docs": {
		Name:         "docs",
		Description:  "Create or update project documentation.",
		Prompt:       "Update the project documentation to reflect the current state. Create or edit README.md with a concise project description.",
		AllowedTools: []string{"read_file", "write_file", "list_files", "shell"},
		Timeout:      5 * time.Minute,
		Retries:      1,
	},
	"review": {
		Name:         "review",
		Description:  "Review the changes made against the goal.",
		Prompt:       "Review the current state of the repository against the goal. Identify gaps or problems and summarize the outcome.",
		AllowedTools: []string{"read_file", "list_files", "shell", "git"},
		Timeout:      5 * time.Minute,
		Retries:      1,
	},
	"git_clone": {
		Name:         "git_clone",
		Description:  "Clone a repository into the workspace.",
		Prompt:       "Clone the repository from the provided URL into the workspace root and confirm it is ready.",
		AllowedTools: []string{"shell", "git", "list_files"},
		Timeout:      10 * time.Minute,
		Retries:      2,
	},
}

// Registry of task templates. Templates can be registered at startup,
// forming the Phase 1 seed of the plugin architecture.
type Registry struct {
	templates map[string]Template
}

func NewRegistry() *Registry {
	r := &Registry{templates: map[string]Template{}}
	for name, t := range defaultTemplates {
		r.templates[name] = t
	}
	return r
}

func (r *Registry) Get(name string) (Template, bool) {
	t, ok := r.templates[name]
	return t, ok
}

func (r *Registry) Names() []string {
	var out []string
	for k := range r.templates {
		out = append(out, k)
	}
	return out
}

func (r *Registry) Validate(name string) error {
	if _, ok := r.templates[name]; !ok {
		return fmt.Errorf("unknown task template %q", name)
	}
	return nil
}
