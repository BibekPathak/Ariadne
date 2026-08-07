# Adriane

Kubernetes for AI agents: a distributed execution platform where agents are
created, planned, scheduled, executed in isolated sandboxes, and replayable
from an event-sourced log.

This is **Phase 0-10** of a ten-phase roadmap: a distributed platform with
persistent three-tier memory, a ranked/compressed context builder, parallel
DAG execution, remote workers over NATS with dead-worker reassignment, task
checkpointing, hardened Docker + **Firecracker microVM** sandboxes, a
policy-based **model router**, an **evaluation engine**, observability with a
**Run Explorer**, and **production features** — organization isolation, API-key
auth, quotas, rate limiting, worker autoscaling, and leader election.

## Architecture

```
                 Dashboard (Phase 9)
                      │
                 API Gateway (REST + SSE)
                      │
                Control Plane (Phase 1, in-process)
   ┌──────────────┼──────────────┬───────────────┐
 Planner ──▶ Compiler ──▶ DAG ──▶ Scheduler ──▶ Worker
                      │                             │
                  PostgreSQL                  Docker Sandbox
                  (agents, tasks,                  │
                   events, artifacts)          Tool Runtime
                                                 (read/write/shell/git/http)
                                                      │
                                                  LLM Runtime
                                            (Requesty / demo provider)
```

Key seams (each maps to a later phase):

| Interface | Current impl | Later impl |
|---|---|---|
| `events.EventBus` | in-memory + Postgres append; NATS JetStream in remote mode | — |
| `sandbox.Sandbox` | Docker | Firecracker (Phase 6) |
| `llm.Provider` | Requesty / demo provider | model router (Phase 7) |
| `memory.Memory` | Redis + Postgres + Go-native vectors | Qdrant (Phase 6) |
| `workflow.Compiler` | plan → DAG | richer plans (Phase 3+) |

## Prerequisites

- Go 1.25+
- Docker (daemon running) — used for the sandbox runtime
- `make sandbox-image` — builds the sandbox tooling image (git, go, python, node)

## Quick start (offline demo)

No API key required — the demo provider drives the real tool pipeline inside a
real Docker sandbox.

```bash
docker compose up -d          # postgres + redis + nats + prometheus
make sandbox-image            # kubeai-sandbox:local (docker runtime)
make firecracker-rootfs       # deploy/firecracker/rootfs.ext4 (firecracker runtime)
make demo                     # docker runtime, single process (auth on, dev key)
make demo-memory              # two runs: proves memory persists across runs
make demo-distributed         # 3 remote workers over NATS; kills one mid-run
make demo-firecracker         # the same agent, executing inside microVMs
make demo-autoscale           # remote + autoscaling: workers spawn and drain
make eval                     # leaderboard across arms + regression detection
```

## Production features (Phase 10)

- **Organizations & API-key auth** (`internal/auth`): `organizations` /
  `users` / `api_keys` tables; keys are shown once at mint (`POST /auth/keys`,
  hashed at rest) and authorize `GET /me`, `GET /usage`, `GET /quota`. RBAC
  roles `admin | owner | reader`. A default org + admin key
  (`ADMIN_API_KEY`, default `adr-dev-admin`) is seeded idempotently on start.
- **Isolation**: agents carry `org_id`; every list/detail/event/artifact is
  org-scoped (admins may cross orgs).
- **Quotas** (`internal/quota`): per-org concurrent-agent, daily-agent and
  daily-cost caps enforced at create.
- **Rate limiting** (`internal/ratelimit`): token bucket per org on agent
  creation (429 + Retry-After).
- **Worker autoscaling** (`internal/autoscale`, remote mode): the control
  plane spawns/terminates `cmd/worker` subprocesses from scheduler depth
  (`WORKER_AUTOSCALE`, `WORKER_MIN/MAX`, `SCALE_UP_THRESHOLD`,
  `SCALE_DOWN_IDLE_SEC`, `AUTOSCALE_POLL_MS`). Workers **drain gracefully**:
  SIGTERM stops new claims but in-flight tasks finish. `make demo-autoscale`.
- **Leader election** (`internal/leader`): a PostgreSQL advisory lock picks one
  scheduler leader; standbys serve reads and return 503 on agent creation, and
  take over when the leader dies (`LEADER_ENABLED`).

## Sandbox runtimes

`SANDBOX_RUNTIME` selects the execution backend behind the `sandbox.Sandbox`
interface — the demo suite runs unchanged on either:

- **`docker`** (default): hardened container per task (read-only rootfs,
  no capabilities, non-root, pids limit).
- **`firecracker`**: a microVM per task. Each VM boots from a kernel + an
  ext4 rootfs built from the tooling image; a small `sandbox-agent` (PID 1)
  serves exec/read/write/list over a vsock, and the host talks to it over the
  Firecracker vsock bridge. A warm-pool pre-boots VMs (~1.4-2s to agent-ready)
  so task startup is fast. Build the rootfs with `make firecracker-rootfs`
  (`scripts/build-firecracker-rootfs.sh`); needs KVM. Verify with
  `SANDBOX_TEST_FIRECRACKER=1 go test ./internal/sandbox/ -run Firecracker -v`.

## Modes

- **`WORKER_MODE=embedded`** (default): the scheduler dispatches to an
  in-process worker. Single binary — the fastest developer loop.
- **`WORKER_MODE=remote`**: the control plane (`cmd/api-gateway`) and workers
  (`cmd/worker`) are separate processes joined by NATS. Task dispatch,
  results, claims, heartbeats and events flow over JSON on NATS subjects; the
  control plane persists events to Postgres for replay. Workers that stop
  heartbeating are marked dead and their in-flight tasks are reassigned.

## Model router (Phase 7)

`internal/router` selects the model and gateway for every LLM call. It is
policy-based and provider-agnostic: callers describe the task
(`RouteRequest{TaskType, TierHint, LatencyBudget, CostBudget,
RequiresToolCalling, RequiresStructuredOutput}`) and the router maps it to a
logical tier (`fast | coding | reasoning | vision`), then to a model and
gateway.

- **Logical tiers, not model IDs**: `ROUTER_FAST_MODEL`, `ROUTER_CODING_MODEL`
  (defaults to `LLM_MODEL`), `ROUTER_REASONING_MODEL`, `ROUTER_VISION_MODEL`.
- **Cross-gateway failover**: `ROUTER_PRIMARY_URL/API_KEY` (defaults to
  Requesty) with optional `ROUTER_FALLBACK_URL/API_KEY`; if the primary
  gateway errors, the router falls back to the secondary.
- **Policies**: the worker may use any tier (fast for `test`/`analyze`,
  coding for `implement`/`docs`, reasoning for complex goals); the planner is
  pinned to at least the coding tier and never the fast tier.
- Every call emits a `model_routed` event (`{task, tier, model, gateway}`),
  visible in the replay log.

## Safety & resumption

- **Hardened sandbox** (Phase 5): read-only root filesystem with a writable
  `/tmp` tmpfs, all Linux capabilities dropped, `no-new-privileges`, a
  non-root user, `--network none` by default, and a process-count cap — a
  hostile task cannot touch the host, escape the sandbox, or fork-bomb. Verify
  with `SANDBOX_TEST_DOCKER=1 go test ./internal/sandbox/ -run Hardening -v`.
- **Checkpointing** (Phase 5): the agent loop persists its message state to
  Postgres after every LLM round and tool boundary. A task killed mid-run
  resumes from its last checkpoint on the next worker instead of restarting
  from scratch. `make demo-distributed` shows both a dead-worker reassignment
  and a checkpoint resume in the event timeline.

The demo creates a `coder` agent against `./demo/repo`, which plans a
branching DAG (`analyze → {implement ∥ docs} → test`), runs the parallel
branches in isolated sandboxes concurrently, adds a `Subtract` function and
its test, writes a README, runs `go test ./...`, and writes everything as
artifacts and events. `ENGINE_CONCURRENCY` controls branch parallelism.

`make demo-memory` demonstrates persistent memory across runs: run 1 stores
per-task outcomes in short-term (Redis), long-term (Postgres) and semantic
(Go-native vector) memory; run 2 retrieves them and its transcript shows the
recollections injected into the prompt.

## Evaluation engine (Phase 8)

`internal/eval` + `cmd/eval` is a batch harness that runs versioned suites of
golden tasks (`eval/suites/v1/*.yaml`) across router arms and scores them
from the event log: success/pass rate (rule-based `Judge` — must-succeed,
file-exists, content-contains), latency, cost (`llm_called` tokens × per-tier
pricing), and tool-error rate.

- **`make eval`**: runs the suite across `fast,coding,reasoning`, writes
  reproducible artifacts under `evals/<run-id>/` (`leaderboard.json`,
  `results.json`, `metrics.csv`, `summary.md`), then re-runs with an
  impossible check to demonstrate **regression detection**.
- **Regression**: per-metric thresholds (success drop, latency/cost/tool-error
  increases) compared against the previous run of the same suite+arm; a breach
  exits non-zero.
- Every run records suite version, git SHA, and arm. Offline, all arms share
  the mock provider; with real `ROUTER_*_MODEL` IDs the leaderboard
  differentiates models. The `Judge` interface is the seam for LLM/human
  judges later.

## Observability & Run Explorer (Phase 9)

**Metrics** (`internal/obs`): the control plane exposes `GET /metrics`
(Prometheus). Every subsystem emits where it happens — counters (agents,
tasks by status, retries, tool errors, artifacts, LLM calls, tokens, cost),
histograms (task duration, sandbox startup, memory retrieval, planner time,
router decision), and gauges (worker utilization, queue depth, avg DAG
width). `docker compose` includes a prometheus service that scrapes it;
Grafana is deferred until there is enough history to justify dashboards.

**Run Explorer** (`dashboard/` — Next.js App Router + React Flow, plain CSS):
a dumb client that talks directly to the API gateway via REST + SSE. `make
dashboard` starts it on :3000.

- `/` — agents list with status/cost/latency.
- `/agents/[id]` — the Run Explorer: the **execution graph** (DAG, status-
  colored, live SSE updates), **replay controls** (▶ play / ⏸ pause / ⏮ step
  back / ⏭ step forward + a timeline slider over event `seq`), a **task
  details** panel (worker, model, tokens, cost, duration, tool calls,
  retries, artifacts, transcript), a **critical path** view (longest real
  duration), and an event log.

The event-sourced timeline is the trace: it answers what ran, which tool, which
model, how long, and how much it cost. OpenTelemetry is a later phase (events
→ OTel spans → Tempo/Jaeger) without changing business logic.

## Running with a real LLM (Requesty)

```bash
cp .env.example .env
# set REQUESTY_API_KEY (and optionally REQUESTY_BASE_URL, LLM_MODEL)
source .env
make demo
```

Without `REQUESTY_API_KEY` the system runs in offline mode with a scripted
provider. With it, the planner and worker use the configured model.

## API

| Endpoint | Description |
|---|---|
| `POST /agents` | Create and run an agent `{template, goal, repo_url\|repo_path}` → `agent_id` |
| `POST /agents/{id}/run` | Re-run an existing agent (task IDs scoped per run; memory persists) |
| `GET /agents/{id}` | Agent status, goal, error |
| `GET /agents/{id}/graph` | Compiled task DAG with statuses and attempts |
| `GET /agents/{id}/events` | Full event-sourced timeline (replay) |
| `GET /agents/{id}/events/stream` | SSE: replay then live tail |
| `GET /agents/{id}/artifacts` | Per-task artifacts (transcripts, logs) |
| `GET /templates` | Available agent templates |
| `GET /health` | Liveness |

```bash
curl -s http://localhost:8080/agents -X POST -H 'Content-Type: application/json' -d '{
  "template": "coder",
  "goal": "add a Subtract function and its test, then run the suite",
  "repo_path": "./demo/repo"
}'
```

## Development

```bash
make test        # unit tests (DAG, tools, context, events, worker, scheduler)
make lint        # gofmt + go vet
make run         # build and run the API
make migrate     # apply schema migrations
```

## Layout

```
cmd/api-gateway        control plane: REST + SSE, scheduler, workflow engine
cmd/worker             standalone worker: claims NATS tasks, runs agent loop
cmd/sandbox-agent      PID 1 inside Firecracker VMs: vsock exec/read/write/list
internal/agents        agent templates + lifecycle orchestration
internal/planner       goal → execution plan (LLM or static fallback)
internal/workflow      compiler (plan → DAG) + DAG engine
internal/scheduler     dispatch (embedded or NATS remote), retries, dead-worker detection
internal/transport     stable NATS wire contract: TaskMessage/Result/Heartbeat
internal/worker        agent loop: sandbox → LLM → tools → artifacts → memory
internal/sandbox       Sandbox interface: Docker + Firecracker (microVM) runtimes
internal/tools         tool registry: read/write/shell/git/http
internal/context       context builder: rank → compress → token budget
internal/llm           Provider interface: Requesty + demo provider + embeddings
internal/router        policy-based model router: tiers, gateways, failover
internal/memory        three-tier memory: Redis (short), Postgres (long), vectors (semantic)
internal/events        event bus: in-memory + NATS JetStream
internal/checkpoint    task checkpointing at tool boundaries (Postgres)
internal/store         postgres repositories + migrations
internal/artifacts     first-class artifact store
internal/runtime       shared worker-stack construction for both binaries
internal/controlplane  shared control-plane assembly (API + eval)
internal/eval          evaluation engine: suites, judges, scoring, regression
internal/obs           Prometheus metrics registry + /metrics
internal/auth          orgs, API keys, RBAC
internal/quota         per-org quotas (concurrent/daily/cost)
internal/ratelimit     token-bucket rate limiter
internal/autoscale     worker subprocess autoscaling + graceful drain
internal/leader        Postgres advisory-lock leader election
dashboard/             Run Explorer (Next.js + React Flow)
demo/repo              sample project used by the demos
eval/suites            versioned golden-task suites
```

## Roadmap

The ten-phase roadmap is complete. Natural follow-ons: OpenTelemetry spans
(events → OTel → Tempo/Jaeger), a shared/MinIO artifact store, GPU workers,
and multi-region control planes.
