package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"adriane/internal/llm"
)

// Outcome is everything a judge needs to score one eval task.
type Outcome struct {
	Task       string
	AgentID    string
	Success    bool   // agent run completed
	RepoDir    string // scratch repo after the run (for file/contains checks)
	Transcript []llm.Message
	Artifacts  []string
	LatencyMs  int64
	Cost       float64
	Tokens     int
	ToolErrors int
	Error      string
}

// Verdict is the result of judging one outcome.
type Verdict struct {
	Pass   bool
	Reason string
	Score  float64
}

// Judge scores an outcome. Rule-based judges ship now; LLM and human judges
// implement the same interface later.
type Judge interface {
	Score(ctx context.Context, o Outcome) (Verdict, error)
}

// CompositeJudge applies the rule-based rubric from the suite definition:
// must succeed, expected files exist, expected substrings present.
type CompositeJudge struct {
	Expected Expected
	WorkDir  string // host path of the scratch repo
}

func (j CompositeJudge) Score(ctx context.Context, o Outcome) (Verdict, error) {
	var failures []string

	if j.Expected.MustSucceed && !o.Success {
		failures = append(failures, "agent did not complete: "+o.Error)
	}

	for _, f := range j.Expected.Files {
		if _, err := os.Stat(filepath.Join(j.WorkDir, f)); err != nil {
			failures = append(failures, "missing file "+f)
		}
	}

	if len(j.Expected.Contains) > 0 {
		var blob strings.Builder
		_ = filepath.Walk(j.WorkDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(j.WorkDir, path)
			if err != nil {
				return nil
			}
			if strings.HasPrefix(rel, ".git/") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err == nil {
				blob.Write(data)
			}
			return nil
		})
		content := blob.String()
		for _, want := range j.Expected.Contains {
			if !strings.Contains(content, want) {
				failures = append(failures, "missing content "+want)
			}
		}
	}

	if len(failures) == 0 {
		return Verdict{Pass: true, Score: 1}, nil
	}
	return Verdict{Pass: false, Reason: strings.Join(failures, "; "), Score: 0}, nil
}
