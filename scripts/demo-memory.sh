#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
BASE=http://localhost:8080

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

wait_status() {
  local id=$1
  local status
  for i in $(seq 1 180); do
    status=$(curl -s $BASE/agents/$id | python3 -c 'import sys,json; print(json.load(sys.stdin)["status"])')
    if [[ "$status" == completed || "$status" == failed ]]; then echo "$status"; return; fi
    sleep 1
  done
  echo timeout
}

create_agent() {
  curl -s $BASE/agents -X POST -H 'Content-Type: application/json' -d "$1" \
    | python3 -c 'import sys,json; print(json.load(sys.stdin)["agent_id"])'
}

AGENT_ID=$(create_agent '{
  "template": "coder",
  "goal": "Refactor the mathx package to prefer table driven tests and clean style. Implement the refactor and run the test suite.",
  "repo_path": "./demo/repo"
}')
echo "==> run 1 agent: $AGENT_ID"
echo "==> waiting for run 1..."
S1=$(wait_status $AGENT_ID)
echo "==> run 1: $S1"

echo ""
echo "==> re-running same agent (run 2) — memory should persist across runs"
curl -s -X POST $BASE/agents/$AGENT_ID/run >/dev/null
echo "==> waiting for run 2..."
S2=$(wait_status $AGENT_ID)
echo "==> run 2: $S2"

echo ""
echo "==> memory events for run 1 (memory_saved)"
curl -s $BASE/agents/$AGENT_ID/events | python3 -c '
import sys, json
for e in json.load(sys.stdin)["events"]:
    if e["type"] in ("memory_retrieved", "memory_saved"):
        print("  %s %s" % (e["type"], json.dumps(e.get("payload",{}))))' | head -8

echo ""
echo "==> run 2 analyze transcript (should contain RECOLLECTION from run 1)"
TRANS=$(find artifacts/$AGENT_ID -path '*analyze*' -name 'transcript_*.json' -printf '%T@ %p\n' | sort -n | tail -1 | cut -d' ' -f2-)
echo "  transcript: $TRANS"
python3 -c '
import json, sys
msgs = json.load(open(sys.argv[1]))
for m in msgs:
    if m.get("role") == "assistant" and m.get("content"):
        print("  assistant: %s" % m["content"])
' "$TRANS"

echo ""
echo "==> demo-memory complete"
