#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
BASE=http://localhost:8080
ADMIN_KEY=${ADMIN_API_KEY:-adr-dev-admin}
curl() { command curl -s -H "Authorization: Bearer $ADMIN_KEY" "$@"; }

echo "==> building"
go build -o bin/kubeai ./cmd/api-gateway
go build -o bin/kubeai-worker ./cmd/worker

echo "==> applying migrations"
./bin/kubeai migrate

echo "==> starting control plane (remote + autoscale) on :8080"
WORKER_MODE=remote WORKER_AUTOSCALE=true WORKER_MIN=1 WORKER_MAX=3 \
  SCALE_UP_THRESHOLD=1 SCALE_DOWN_IDLE_SEC=20 AUTOSCALE_POLL_MS=300 \
  ./bin/kubeai >/tmp/kubeai-api.log 2>&1 &
API_PID=$!
trap 'kill $API_PID 2>/dev/null || true' EXIT

for i in $(seq 1 30); do
  if curl -sf $BASE/health >/dev/null 2>&1; then break; fi
  sleep 0.5
done

echo "==> waiting for the autoscaler to spawn the floor worker (WORKER_MIN=1)..."
sleep 4
grep -c "autoscaler: spawned worker" /tmp/kubeai-api.log | xargs echo "workers spawned so far:"

AGENT_ID=$(curl -s $BASE/agents -X POST -H 'Content-Type: application/json' -d '{
  "template": "coder",
  "goal": "In the mathx package, add a Subtract(a, b int) int function and a passing test TestSubtract that checks Subtract(5,3) == 2. Then run the full test suite.",
  "repo_path": "./demo/repo"
}' | python3 -c 'import sys,json; print(json.load(sys.stdin)["agent_id"])')
echo "==> agent: $AGENT_ID (creating a second agent to raise load)"
AGENT2=$(curl -s $BASE/agents -X POST -H 'Content-Type: application/json' -d '{
  "template": "coder",
  "goal": "Write a README.md documenting this Go project, mentioning Adriane.",
  "repo_path": "./demo/repo"
}' | python3 -c 'import sys,json; print(json.load(sys.stdin)["agent_id"])')

echo "==> run active; parallel branches raise queue depth -> autoscaler scales up"
STATUS=created
for i in $(seq 1 240); do
  STATUS=$(curl -s $BASE/agents/$AGENT_ID | python3 -c 'import sys,json; print(json.load(sys.stdin)["status"])')
  if [[ "$STATUS" == completed || "$STATUS" == failed ]]; then break; fi
  sleep 1
done
echo "==> final status: $STATUS"

echo ""
echo "==> autoscaler events (spawn / drain)"
grep -E "autoscaler: spawned worker|autoscaler: draining worker|autoscaler: scaled down" /tmp/kubeai-api.log | tail -10

echo ""
echo "==> now idle: waiting for scale-down (SCALE_DOWN_IDLE=20s)..."
sleep 28
echo "==> autoscaler scale-down events"
grep -E "autoscaler: draining worker|autoscaler: scaled down" /tmp/kubeai-api.log | tail -8

echo ""
echo "==> demo-autoscale complete"
