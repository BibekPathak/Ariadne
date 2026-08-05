package eval

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Suite is a versioned set of golden evaluation tasks.
type Suite struct {
	Name        string     `yaml:"name"`
	Version     int        `yaml:"version"`
	Description string     `yaml:"description"`
	Thresholds  Thresholds `yaml:"thresholds"`
	Tasks       []Task     `yaml:"tasks"`
}

// Task is one golden scenario: a goal, a repository, and expected outcomes.
type Task struct {
	Name       string   `yaml:"name"`
	Template   string   `yaml:"template"`
	Goal       string   `yaml:"goal"`
	Repo       string   `yaml:"repo"`
	Expected   Expected `yaml:"expected"`
	TimeoutSec int      `yaml:"timeout_sec"`
}

type Expected struct {
	MustSucceed bool     `yaml:"must_succeed"`
	Files       []string `yaml:"files"`
	Contains    []string `yaml:"contains"`
	TestsPass   bool     `yaml:"tests_pass"`
}

// Thresholds define when a metric change counts as a regression.
type Thresholds struct {
	SuccessDropPercent        float64 `yaml:"success_drop_percent"`
	LatencyIncreasePercent    float64 `yaml:"latency_increase_percent"`
	CostIncreasePercent       float64 `yaml:"cost_increase_percent"`
	ToolErrorsIncreasePercent float64 `yaml:"tool_errors_increase_percent"`
}

func DefaultThresholds() Thresholds {
	return Thresholds{
		SuccessDropPercent: 10, LatencyIncreasePercent: 20,
		CostIncreasePercent: 30, ToolErrorsIncreasePercent: 10,
	}
}

func (s *Suite) Timeout() time.Duration {
	if len(s.Tasks) == 0 || s.Tasks[0].TimeoutSec <= 0 {
		return 3 * time.Minute
	}
	return time.Duration(s.Tasks[0].TimeoutSec) * time.Second
}

func LoadSuite(path string) (*Suite, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read suite %s: %w", path, err)
	}
	var s Suite
	if err := yaml.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("parse suite %s: %w", path, err)
	}
	if s.Name == "" || len(s.Tasks) == 0 {
		return nil, fmt.Errorf("suite %s: name and at least one task required", path)
	}
	if s.Version <= 0 {
		s.Version = 1
	}
	if s.Thresholds == (Thresholds{}) {
		s.Thresholds = DefaultThresholds()
	}
	for i := range s.Tasks {
		if s.Tasks[i].Template == "" {
			s.Tasks[i].Template = "coder"
		}
		if s.Tasks[i].TimeoutSec <= 0 {
			s.Tasks[i].TimeoutSec = 180
		}
	}
	return &s, nil
}
