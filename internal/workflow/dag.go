package workflow

import "context"

// TaskStatus mirrors the persisted task lifecycle.
type TaskStatus string

const (
	StatusPending TaskStatus = "pending"
	StatusRunning TaskStatus = "running"
	StatusDone    TaskStatus = "done"
	StatusFailed  TaskStatus = "failed"
	StatusBlocked TaskStatus = "blocked"
)

// Node is a single unit of work in the compiled DAG.
type Node struct {
	ID         string
	AgentID    string
	Name       string
	Template   string
	Inputs     map[string]any
	DependsOn  []string
	Status     TaskStatus
	Attempt    int
	MaxAttempt int
	Outputs    map[string]any
	Error      string
}

// DAG is the compiled execution graph.
type DAG struct {
	AgentID string
	Nodes   []*Node
}

func (d *DAG) index() map[string]*Node {
	m := make(map[string]*Node, len(d.Nodes))
	for _, n := range d.Nodes {
		m[n.ID] = n
	}
	return m
}

func (d *DAG) byName(name string) *Node {
	for _, n := range d.Nodes {
		if n.Name == name {
			return n
		}
	}
	return nil
}

// Ready returns nodes whose dependencies have all completed.
func (d *DAG) Ready() []*Node {
	idx := d.index()
	var out []*Node
	for _, n := range d.Nodes {
		if n.Status != StatusPending {
			continue
		}
		ready := true
		for _, dep := range n.DependsOn {
			dNode := idx[dep]
			if dNode == nil {
				ready = false
				continue
			}
			if dNode.Status != StatusDone {
				ready = false
				break
			}
		}
		if ready {
			out = append(out, n)
		}
	}
	return out
}

// RemainingPending returns true while any node still needs work.
func (d *DAG) RemainingPending() bool {
	for _, n := range d.Nodes {
		if n.Status == StatusPending || n.Status == StatusRunning {
			return true
		}
	}
	return false
}

// AnyFailed reports whether any node has irrecoverably failed.
func (d *DAG) AnyFailed() bool {
	for _, n := range d.Nodes {
		if n.Status == StatusFailed {
			return true
		}
	}
	return false
}

// Blocked marks all pending nodes that depend (directly or transitively) on a
// failed node as blocked.
func (d *DAG) Blocked() {
	idx := d.index()
	for _, n := range d.Nodes {
		if n.Status != StatusPending {
			continue
		}
		if dependsOnFailed(n, idx) {
			n.Status = StatusBlocked
		}
	}
}

func dependsOnFailed(n *Node, idx map[string]*Node) bool {
	for _, dep := range n.DependsOn {
		dn := idx[dep]
		if dn == nil {
			continue
		}
		if dn.Status == StatusFailed || dn.Status == StatusBlocked {
			return true
		}
		if dn.Status == StatusPending && dependsOnFailed(dn, idx) {
			return true
		}
	}
	return false
}

// TaskExecutor runs a single node to completion. The scheduler implements it.
type TaskExecutor interface {
	Execute(ctx context.Context, node *Node) (map[string]any, error)
}
