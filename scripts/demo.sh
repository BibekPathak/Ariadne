#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
BASE=http://localhost:8080
ADMIN_KEY=${ADMIN_API_KEY:-adr-dev-admin}
curl() { command curl -s -H "Authorization: Bearer $ADMIN_KEY" "$@"; }


echo "==> building"
go build -o bin/kubeai ./cmd/api-gateway

echo "==> applying migrations"
./bin/kubeai migrate

echo "==> starting control plane on :8080"
./bin/kubeai >/tmp/kubeai-api.log 2>&1 &
API_PID=$!
trap 'kill $API_PID 2>/dev/null || true' EXIT

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

echo "==> waiting for completion (polling)..."
STATUS=created
for i in $(seq 1 180); do
  STATUS=$(curl -s $BASE/agents/$AGENT_ID | python3 -c 'import sys,json; print(json.load(sys.stdin)["status"])')
  if [[ "$STATUS" == completed || "$STATUS" == failed || "$STATUS" == cancelled ]]; then break; fi
  sleep 1
done
echo "==> final status: $STATUS"
if [[ "$STATUS" != completed ]]; then
  echo "ERROR: agent did not complete"
  curl -s $BASE/agents/$AGENT_ID | python3 -m json.tool
  exit 1
fi

echo ""
echo "==> task graph (compiled DAG)"
curl -s $BASE/agents/$AGENT_ID/graph | python3 -c '
import sys, json
tasks = json.load(sys.stdin)["tasks"]
by = {t["name"]: t for t in tasks}
print("  %-10s status" % "task")
for t in tasks:
    deps = ",".join(by[d.split("_")[-1]]["name"] for d in t["depends_on"] if d.split("_")[-1] in by) or "-"
    arrow = ""
    if deps != "-":
        arrow = " <- " + deps
    print("  %-10s %-8s %s%s" % (t["name"], t["status"], t["template"], arrow))
# Detect parallel branches: nodes with the same dependency set.
from collections import defaultdict
groups = defaultdict(list)
for t in tasks:
    groups[tuple(sorted(d.split("_")[-1] for d in t["depends_on"]))].append(t["name"])
parallel = [g for g in groups.values() if len(g) > 1]
if parallel:
    print("  parallel branches: " + " ∥ ".join("{" + ",".join(g) + "}" for g in parallel))'

echo ""
echo "==> event timeline (event-sourced replay)"
curl -s $BASE/agents/$AGENT_ID/events | python3 -c '
import sys, json
for e in json.load(sys.stdin)["events"]:
    task = e.get("task_id","")
    if task:
        task = "[" + task.split("_")[-1] + "] "
    ts = e["ts"][11:19]
    print("  %s %-4s %s%s" % (ts, e["seq"], task, e["type"]))'

echo ""
echo "==> artifacts"
curl -s $BASE/agents/$AGENT_ID/artifacts | python3 -c '
import sys, json
for a in json.load(sys.stdin)["artifacts"]:
    print("  [%s] %s (%d bytes)" % (a["type"], a["path"], a["size"]))'

echo ""
echo "==> repo changes on host (written through the sandbox bind mount)"
cd demo/repo
git status --short
echo ""
echo "mathx/mathx.go:"
cat mathx/mathx.go
echo ""
echo "==> demo complete"
