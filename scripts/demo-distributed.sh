#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
BASE=http://localhost:8080

echo "==> building binaries"
go build -o bin/kubeai ./cmd/api-gateway
go build -o bin/kubeai-worker ./cmd/worker

echo "==> applying migrations"
./bin/kubeai migrate

echo "==> starting control plane (remote mode) on :8080"
WORKER_MODE=remote ./bin/kubeai >/tmp/kubeai-api.log 2>&1 &
CP_PID=$!

echo "==> starting 3 remote workers"
WORKER_PIDS=()
for i in 1 2 3; do
  ./bin/kubeai-worker >/tmp/kubeai-worker-$i.log 2>&1 &
  WORKER_PIDS+=($!)
done
trap 'kill $CP_PID ${WORKER_PIDS[@]:-} 2>/dev/null || true' EXIT

for i in $(seq 1 30); do
  if curl -sf $BASE/health >/dev/null 2>&1; then break; fi
  sleep 0.5
done

AGENT_ID=$(curl -s $BASE/agents -X POST -H 'Content-Type: application/json' -d '{
  "template": "coder",
  "goal": "In the mathx package, add a Subtract(a, b int) int function and a passing test TestSubtract that checks Subtract(5,3) == 2. Then run the full test suite.",
  "repo_path": "./demo/repo"
}' | python3 -c 'import sys,json; print(json.load(sys.stdin)["agent_id"])')
echo "==> agent: $AGENT_ID"

echo "==> waiting for the long-running test task to start..."
STARTED=""
for i in $(seq 1 90); do
  STARTED=$(curl -s $BASE/agents/$AGENT_ID/events | python3 -c '
import sys, json
try:
    for e in json.load(sys.stdin)["events"]:
        if e["type"] == "task_started" and e.get("task_id","").endswith("_test"):
            print(e["task_id"]); break
except Exception:
    pass')
  if [[ -n "$STARTED" ]]; then break; fi
  sleep 1
done
if [[ -z "$STARTED" ]]; then
  echo "==> test task never started; skipping kill"
else
  echo "==> test task: $STARTED"
  for i in 1 2 3; do
    if grep -q "$STARTED" /tmp/kubeai-worker-$i.log 2>/dev/null; then
      VICTIM_PID="${WORKER_PIDS[$((i-1))]}"
      echo "==> test task claimed by worker $i (pid $VICTIM_PID); killing it mid-run"
      kill -9 "$VICTIM_PID"
      break
    fi
  done
fi

echo "==> waiting for completion..."
STATUS=created
for i in $(seq 1 180); do
  STATUS=$(curl -s $BASE/agents/$AGENT_ID | python3 -c 'import sys,json; print(json.load(sys.stdin)["status"])')
  if [[ "$STATUS" == completed || "$STATUS" == failed ]]; then break; fi
  sleep 1
done
echo "==> final status: $STATUS"

echo ""
echo "==> reassignment evidence (retry_scheduled events)"
curl -s $BASE/agents/$AGENT_ID/events | python3 -c '
import sys, json
found = False
for e in json.load(sys.stdin)["events"]:
    if e["type"] == "retry_scheduled":
        found = True
        print("  task %s attempt=%s error=%s" % (e.get("task_id",""), e["payload"].get("attempt"), e["payload"].get("error","")[:100]))
if not found:
    print("  (no reassignment observed)")'

echo ""
echo "==> task graph"
curl -s $BASE/agents/$AGENT_ID/graph | python3 -c '
import sys, json
for t in json.load(sys.stdin)["tasks"]:
    print("  %-10s %-8s template=%s attempt=%d/%d" % (t["name"], t["status"], t["template"], t["attempt"], t["max_attempt"]))'

echo ""
echo "==> last 20 events"
curl -s $BASE/agents/$AGENT_ID/events | python3 -c '
import sys, json
evs = json.load(sys.stdin)["events"]
for e in evs[-20:]:
    task = e.get("task_id","")
    if task: task = "[" + task.split("_")[-1] + "] "
    print("  %s %s%s" % (e["type"], task, ""))'

echo ""
echo "==> worker log snippets"
for i in 1 2 3; do
  echo "  -- worker $i --"
  grep -hE 'claimed task|finished task|marked dead' /tmp/kubeai-worker-$i.log | tail -4 | sed 's/^/    /'
done

echo ""
echo "==> demo-distributed complete"

# Clean up any sandbox containers leaked by the SIGKILLed worker.
docker ps --filter name=kubeai- -q | xargs -r docker rm -f >/dev/null 2>&1 || true
