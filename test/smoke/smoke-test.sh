#!/usr/bin/env bash
#
# End-to-end smoke test for the C-chain indexer.
#
# Builds the indexer image from source, runs it in FSP mode against a C-chain
# RPC node with a throwaway MySQL, and verifies that it:
#   1. starts and reaches "synced" (HTTP 200 on /health), and
#   2. has written transactions and logs to the database.
#
# The stack is always torn down on exit.
#
# Required:
#   RPC_URL  C-chain RPC endpoint for the network under test (Coston
#            recommended). A keyed endpoint is recommended — public endpoints are
#            rate-limited and the run will likely stall during the FSP event
#            backfill.
#
# Exit code 0 = pass, non-zero = fail.

set -euo pipefail

cd "$(dirname "$0")"

if [ -z "${RPC_URL:-}" ]; then
  cat >&2 <<'EOF'
RPC_URL is not set.

Set it to a C-chain RPC endpoint for the network you want to test (Coston
recommended), e.g.:

  RPC_URL="https://<host>/ext/bc/C/rpc?x-apikey=<key>" ./test/smoke/smoke-test.sh

A keyed endpoint is strongly recommended: public endpoints are rate-limited and
the test will likely time out during the FSP event backfill.
EOF
  exit 1
fi

PROJECT="cchain-indexer-smoke"
COMPOSE=(docker compose -f docker-compose.yaml -p "$PROJECT")
HEALTH_PORT=18080
SYNC_RETRIES=80 # ×3s ≈ 4 min sync budget

cleanup() {
  echo "--- tearing down"
  "${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "--- building and starting stack"
"${COMPOSE[@]}" up -d --build

echo "--- waiting for indexer to sync (HTTP 200 on :${HEALTH_PORT}/health)"
# /health returns 503 while catching up and 200 once startup completes;
# --retry-all-errors retries the 503s, --retry-connrefused covers boot.
if ! curl --fail --silent --show-error \
        --retry "${SYNC_RETRIES}" --retry-delay 3 \
        --retry-connrefused --retry-all-errors \
        "http://localhost:${HEALTH_PORT}/health" >/dev/null; then
  echo "FAIL: indexer did not reach synced state in time"
  "${COMPOSE[@]}" logs --tail=60 indexer || true
  exit 1
fi
echo "--- indexer reported synced"

echo "--- indexer logs"
"${COMPOSE[@]}" logs --tail=40 indexer || true

query() {
  "${COMPOSE[@]}" exec -T mysql \
    mysql -uroot -proot -N -e "$1" 2>/dev/null | tr -d '[:space:]'
}
tx=$(query "select count(*) from flare_ftso_indexer.transactions")
lg=$(query "select count(*) from flare_ftso_indexer.logs")
echo "--- indexed: transactions=${tx:-0} logs=${lg:-0}"

if [ "${tx:-0}" -gt 0 ] && [ "${lg:-0}" -gt 0 ]; then
  echo "PASS: indexer synced and persisted data"
  exit 0
fi

echo "FAIL: expected non-zero transactions and logs"
"${COMPOSE[@]}" logs --tail=60 indexer || true
exit 1
