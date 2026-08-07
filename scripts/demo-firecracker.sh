#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
BASE=http://localhost:8080
ADMIN_KEY=${ADMIN_API_KEY:-adr-dev-admin}
curl() { command curl -s -H "Authorization: Bearer $ADMIN_KEY" "$@"; }


if [[ ! -f deploy/firecracker/rootfs.ext4 ]]; then
  echo "ERROR: deploy/firecracker/rootfs.ext4 missing. Build it with:"
  echo "  ./scripts/build-firecracker-rootfs.sh"
  exit 1
fi

echo "==> building"
rm -rf data/firecracker artifacts bin
go build -o bin/kubeai ./cmd/api-gateway

echo "==> applying migrations"
./bin/kubeai migrate

echo "==> starting control plane (firecracker sandbox) on :8080"
SANDBOX_RUNTIME=firecracker ./bin/kubeai >/tmp/kubeai-api.log 2>&1 &
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

echo "==> waiting for completion..."
STATUS=created
for i in $(seq 1 240); do
  STATUS=$(curl -s $BASE/agents/$AGENT_ID | python3 -c 'import sys,json; print(json.load(sys.stdin)["status"])')
  if [[ "$STATUS" == completed || "$STATUS" == failed ]]; then break; fi
  sleep 1
done
echo "==> final status: $STATUS"
if [[ "$STATUS" != completed ]]; then
  curl -s $BASE/agents/$AGENT_ID | python3 -m json.tool
  exit 1
fi

echo ""
echo "==> task graph"
curl -s $BASE/agents/$AGENT_ID/graph | python3 -c '
import sys, json
tasks = json.load(sys.stdin)["tasks"]
by = {t["name"]: t for t in tasks}
for t in tasks:
    deps = ",".join(by[d.split("_")[-1]]["name"] for d in t["depends_on"] if d.split("_")[-1] in by) or "-"
    print("  %-10s %-8s template=%s <- %s" % (t["name"], t["status"], t["template"], deps))'

echo ""
echo "==> event timeline (tool activity)"
curl -s $BASE/agents/$AGENT_ID/events | python3 -c '
import sys, json
for e in json.load(sys.stdin)["events"]:
    task = e.get("task_id","")
    if task: task = "[" + task.split("_")[-1] + "] "
    if e["type"] in ("tool_called","tool_finished","task_finished","agent_completed"):
        print("  %s%s %s" % (task, e["type"], json.dumps(e.get("payload",{}))[:100]))'

echo ""
echo "==> test task result (ran inside the microVM)"
curl -s $BASE/agents/$AGENT_ID/artifacts | python3 -c '
import sys, json
for a in json.load(sys.stdin)["artifacts"]:
    if a["path"].endswith(".json"):
        print("  [%s] %s" % (a["type"], a["path"]))' >/dev/null
T=$(ls artifacts/$AGENT_ID/*/*test*/transcript_*.json 2>/dev/null | head -1)
if [[ -n "$T" ]]; then
  python3 -c '
import json,sys
for m in json.load(open(sys.argv[1])):
    if m.get("role")=="tool":
        print("  %s" % m.get("content","")[:200])
        break
' "$T"
fi

echo ""
echo "==> firecracker VM boot latencies"
grep -h "firecracker VM ready" /tmp/kubeai-api.log | python3 -c '
import sys, json, re
for line in sys.stdin:
    m = re.search(r"boot_ms\":(\d+)", line)
    if m: print("  boot in %s ms" % m.group(1))' | head -8

echo ""
echo "==> demo-firecracker complete"
