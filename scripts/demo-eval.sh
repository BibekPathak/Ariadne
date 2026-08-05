#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

echo "==> building"
go build -o bin/kubeai ./cmd/api-gateway
go build -o bin/kubeai-eval ./cmd/eval

echo "==> applying migrations"
./bin/kubeai migrate

echo "==> clearing previous eval runs"
docker exec kubeai-postgres-1 psql -U kubeai -d kubeai -qc "TRUNCATE eval_runs, eval_results CASCADE" 2>/dev/null || true

echo "==> run 1: baseline across arms fast,coding,reasoning (offline: shared mock provider)"
rm -rf evals
./bin/kubeai-eval --suite eval/suites/v1/coder.yaml --tier fast,coding,reasoning || true

echo ""
echo "==> run 2: same suite with an impossible check -> success rate drops -> regression"
sed 's/"func Subtract"/"func Multiply"/' eval/suites/v1/coder.yaml > /tmp/regress-suite.yaml
OUT=$(./bin/kubeai-eval --suite /tmp/regress-suite.yaml --tier coding 2>&1 || true)
echo "$OUT" | grep -E "regression vs|REGRESSION" || true
if echo "$OUT" | grep -q "REGRESSION DETECTED"; then
  echo ""
  echo "==> REGRESSION DETECTED as expected"
else
  echo "ERROR: regression was not detected"
  exit 1
fi

echo ""
echo "==> artifacts"
find evals -name leaderboard.json -o -name summary.md | sort | head
echo ""
echo "==> demo-eval complete"
