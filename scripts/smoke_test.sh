#!/bin/bash
# Manual smoke test for Mist
# Prerequisites: Redis (docker-compose up -d), Mist app (cd src && go run .)
# App defaults to GPU_TYPE=CPU so it runs container jobs locally.

set -e
BASE="${BASE_URL:-http://127.0.0.1:3000}"
COOKIES="/tmp/mist_smoke_cookies.txt"
rm -f "$COOKIES"

echo "=== Mist Smoke Test ==="
echo "Target: $BASE"
echo ""

# 1. Login
echo "1. Login..."
LOGIN=$(curl -s -c "$COOKIES" -b "$COOKIES" -L -X POST "$BASE/auth/login" \
  -d "username=admin&password=admin&return_url=/" -o /dev/null -w "%{http_code}")
if [ "$LOGIN" != "200" ] && [ "$LOGIN" != "302" ] && [ "$LOGIN" != "404" ]; then
  echo "   FAIL: expected 200/302/404, got $LOGIN"
  exit 1
fi
echo "   OK (HTTP $LOGIN)"

# 2. Create job (empty gpu - any supervisor handles it; CPU runs container)
echo "2. Create job..."
JOB_RESP=$(curl -s -b "$COOKIES" -X POST "$BASE/jobs" \
  -H "Content-Type: application/json" \
  -d '{"type":"smoke_test","payload":{"test":1}}')
if ! echo "$JOB_RESP" | grep -q job_id; then
  echo "   FAIL: $JOB_RESP"
  exit 1
fi
JOB_ID=$(echo "$JOB_RESP" | grep -o '"job_id":"[^"]*"' | cut -d'"' -f4)
echo "   OK (job_id: $JOB_ID)"

# 3. Get logs (supervisor picks up in ~0-5s, container runs 5s; poll for 15s)
echo "3. Get logs (polling up to 15s)..."
GOT_LOGS=0
for i in $(seq 1 60); do
  RESULT=$(curl -s -b "$COOKIES" -w "\n%{http_code}" "$BASE/jobs/logs/$JOB_ID")
  CODE=$(echo "$RESULT" | tail -n1 | tr -d '\r\n')
  BODY=$(echo "$RESULT" | sed '$d')
  if [ "$CODE" = "200" ]; then
    echo "   OK (HTTP 200)"
    echo ""
    echo "--- Logs preview ---"
    echo "$BODY" | head -c 500
    echo ""
    GOT_LOGS=1
    break
  fi
  sleep 0.25
done

if [ $GOT_LOGS -eq 0 ]; then
  echo "   FAIL: logs not available (expected 200)"
  echo "   Ensure app runs with default GPU_TYPE=CPU and Docker is available."
  exit 1
fi

echo ""
echo "=== Smoke Test PASSED ==="
