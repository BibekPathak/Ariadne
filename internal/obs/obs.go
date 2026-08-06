// Package obs exposes Prometheus metrics for the platform. Metrics are emitted
// where the events happen (router, worker, scheduler, engine, agent service);
// the control plane exposes them on GET /metrics.
package obs

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	reg *prometheus.Registry

	AgentsCreated   prometheus.Counter
	AgentsCompleted prometheus.Counter
	AgentsFailed    prometheus.Counter
	TasksSucceeded  prometheus.Counter
	TasksFailed     prometheus.Counter
	TasksBlocked    prometheus.Counter
	Retries         prometheus.Counter
	ToolErrors      prometheus.Counter
	Artifacts       prometheus.Counter
	LLMCalls        prometheus.Counter
	LLMTokens       *prometheus.CounterVec
	Cost            prometheus.Counter

	TaskDuration    prometheus.Histogram
	SandboxStartup  prometheus.Histogram
	MemoryRetrieval prometheus.Histogram
	PlannerTime     prometheus.Histogram
	RouterDecision  prometheus.Histogram

	WorkerUtilization *prometheus.GaugeVec
	QueueDepth        prometheus.Gauge
	DagWidthAvg       prometheus.Gauge
}

func New() *Metrics {
	reg := prometheus.NewRegistry()
	ns := "adriane"
	m := &Metrics{reg: reg}

	m.AgentsCreated = counter(reg, ns, "agents_created_total", "Agents created.")
	m.AgentsCompleted = counter(reg, ns, "agents_completed_total", "Agents completed.")
	m.AgentsFailed = counter(reg, ns, "agents_failed_total", "Agents failed.")
	m.TasksSucceeded = counter(reg, ns, "tasks_succeeded_total", "Tasks succeeded.")
	m.TasksFailed = counter(reg, ns, "tasks_failed_total", "Tasks failed.")
	m.TasksBlocked = counter(reg, ns, "tasks_blocked_total", "Tasks blocked by a failed dependency.")
	m.Retries = counter(reg, ns, "retries_total", "Task retries.")
	m.ToolErrors = counter(reg, ns, "tool_errors_total", "Tool executions that errored.")
	m.Artifacts = counter(reg, ns, "artifacts_total", "Artifacts generated.")
	m.LLMCalls = counter(reg, ns, "llm_calls_total", "LLM generate calls.")
	m.LLMTokens = counterVec(reg, ns, "llm_tokens_total", "LLM tokens by type.", "type")
	m.Cost = counter(reg, ns, "llm_cost_total", "Estimated LLM cost (USD).")

	m.TaskDuration = histogram(reg, ns, "task_duration_seconds", "Task execution duration.", []float64{0.1, 1, 5, 15, 60, 300})
	m.SandboxStartup = histogram(reg, ns, "sandbox_startup_seconds", "Sandbox preparation duration.", []float64{0.1, 0.5, 1, 3, 10})
	m.MemoryRetrieval = histogram(reg, ns, "memory_retrieval_seconds", "Memory load duration.", []float64{0.001, 0.01, 0.1, 0.5})
	m.PlannerTime = histogram(reg, ns, "planner_seconds", "Planner LLM call duration.", []float64{0.5, 2, 5, 15, 60})
	m.RouterDecision = histogram(reg, ns, "router_decision_seconds", "Model router decision + call duration.", []float64{0.5, 2, 5, 15, 60})

	m.WorkerUtilization = gaugeVec(reg, ns, "worker_utilization", "Workers busy by state (0..1).", "state")
	m.QueueDepth = gauge(reg, ns, "queue_depth", "Tasks currently executing.")
	m.DagWidthAvg = gauge(reg, ns, "dag_width_avg", "Running average width of compiled DAGs.")

	return m
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// Noop returns a Metrics with no-op collectors for tests and callers that
// don't want to emit.
func Noop() *Metrics { return New() }

func counter(reg *prometheus.Registry, ns, name, help string) prometheus.Counter {
	c := prometheus.NewCounter(prometheus.CounterOpts{Namespace: ns, Name: name, Help: help})
	reg.MustRegister(c)
	return c
}

func counterVec(reg *prometheus.Registry, ns, name, help, label string) *prometheus.CounterVec {
	c := prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: ns, Name: name, Help: help}, []string{label})
	reg.MustRegister(c)
	return c
}

func histogram(reg *prometheus.Registry, ns, name, help string, buckets []float64) prometheus.Histogram {
	h := prometheus.NewHistogram(prometheus.HistogramOpts{Namespace: ns, Name: name, Help: help, Buckets: buckets})
	reg.MustRegister(h)
	return h
}

func gauge(reg *prometheus.Registry, ns, name, help string) prometheus.Gauge {
	g := prometheus.NewGauge(prometheus.GaugeOpts{Namespace: ns, Name: name, Help: help})
	reg.MustRegister(g)
	return g
}

func gaugeVec(reg *prometheus.Registry, ns, name, help, label string) *prometheus.GaugeVec {
	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: ns, Name: name, Help: help}, []string{label})
	reg.MustRegister(g)
	return g
}
