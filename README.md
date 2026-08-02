# kubeAI

Kubernetes for AI agents: a distributed execution platform where agents are
created, planned, scheduled, executed in isolated sandboxes, and replayable
from an event-sourced log.

This is **Phase 0+1** of a ten-phase roadmap: a single-agent, single-worker
system architected so the remaining phases (memory, distributed workers over
NATS, Firecracker, model router, evaluation, observability, production
features) slot in behind existing interfaces.

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

| Interface | Phase 1 impl | Phase 4+ impl |
|---|---|---|
| `events.EventBus` | in-memory + Postgres append | NATS JetStream |
| `sandbox.Sandbox` | Docker | Firecracker (Phase 6) |
| `llm.Provider` | Requesty / demo | model router (Phase 7) |
| `workflow.Compiler` | plan → DAG | same, richer plans (Phase 3) |
| `memory.Memory` | (none) | short/long/semantic (Phase 2) |

## Prerequisites

- Go 1.25+
- Docker (daemon running) — used for the sandbox runtime
- `make sandbox-image` — builds the sandbox tooling image (git, go, python, node)

## Quick start (offline demo)

No API key required — the demo provider drives the real tool pipeline inside a
real Docker sandbox.

```bash
docker compose up -d          # postgres + redis
make sandbox-image            # kubeai-sandbox:local
make demo                     # build, migrate, run agent, show results
```

The demo creates a `coder` agent against `./demo/repo`, which plans
(`analyze → implement → test`), adds a `Subtract` function and its test via
the sandbox, runs `go test ./...`, and writes everything as artifacts and
events.

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
cmd/api-gateway        REST + SSE server; wires the in-process control plane
internal/agents        agent templates + lifecycle orchestration
internal/planner       goal → execution plan (LLM or static fallback)
internal/workflow      compiler (plan → DAG) + DAG engine
internal/scheduler     dispatch, worker lifecycle, retries
internal/worker        agent loop: sandbox → LLM → tools → artifacts
internal/sandbox       Sandbox interface + Docker implementation
internal/tools         tool registry: read/write/shell/git/http
internal/context       context builder with token budget (Phase 2: ranking)
internal/llm           Provider interface: Requesty + demo provider
internal/events        event bus + event schema (event sourcing)
internal/store         postgres repositories + migrations
internal/artifacts     first-class artifact store
demo/repo              sample project used by `make demo`
```

## Roadmap

Phase 2 persistent memory & context engineering · Phase 3 parallel DAGs ·
Phase 4 distributed workers over NATS · Phase 5 checkpointing & hardened
sandbox · Phase 6 Firecracker · Phase 7 model router · Phase 8 evaluation ·
Phase 9 observability + execution graph UI · Phase 10 production features.
