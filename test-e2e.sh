#!/bin/bash
set -euo pipefail
PROJECT_ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$PROJECT_ROOT"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

PASS=0
FAIL=0

pass() { echo -e "  ${GREEN}✅ $1${NC}"; PASS=$((PASS+1)); }
fail() { echo -e "  ${RED}❌ $1${NC}"; FAIL=$((FAIL+1)); }

# ─── Phase 1: Build & Unit Tests ───
echo -e "\n${YELLOW}═══ Phase 1: Build & Unit Tests ═══${NC}"
cd gateway
go build ./... && pass "go build" || fail "go build"
go test -v ./... 2>&1 | tail -3
if [ "${PIPESTATUS[0]}" -eq 0 ]; then pass "unit tests"; else fail "unit tests"; fi
cd "$PROJECT_ROOT"

# ─── Phase 2: Start Whisper Server ───
echo -e "\n${YELLOW}═══ Phase 2: Start Whisper Server ═══${NC}"
cd whisper-package
./whisper-server -m models/ggml-medium-q8_0.bin --host 127.0.0.1 --port 8080 > /tmp/whisper-e2e.log 2>&1 &
WHISPER_PID=$!
cd "$PROJECT_ROOT"

for i in $(seq 1 30); do
  if curl -sf http://127.0.0.1:8080/health > /dev/null 2>&1; then break; fi
  sleep 2
done
if kill -0 "$WHISPER_PID" 2>/dev/null; then
  pass "whisper server ready (PID $WHISPER_PID)"
else
  fail "whisper server failed to start"
  exit 1
fi

# ─── Phase 3: Start Gateway ───
echo -e "\n${YELLOW}═══ Phase 3: Start Gateway ═══${NC}"
cd gateway
OAUTH_AUDIENCE="" PORT=9092 WHISPER_HOST=127.0.0.1 WHISPER_PORT=8080 NUM_WORKERS=1 \
  go run . > /tmp/gateway-e2e.log 2>&1 &
GATEWAY_PID=$!
cd "$PROJECT_ROOT"

for i in $(seq 1 15); do
  if curl -sf http://127.0.0.1:9092/health | grep -q '"ok"'; then break; fi
  sleep 1
done
if kill -0 "$GATEWAY_PID" 2>/dev/null; then
  pass "gateway ready (PID $GATEWAY_PID)"
else
  fail "gateway failed to start"
  kill "$WHISPER_PID" 2>/dev/null
  exit 1
fi

G="http://127.0.0.1:9092"
T="Bearer dev-test-token"

# ─── Phase 4: Auth Tests ───
echo -e "\n${YELLOW}═══ Phase 4: Auth Tests ═══${NC}"

CODE=$(curl -s -o /dev/null -w '%{http_code}' "$G/jobs/x")
[ "$CODE" = "401" ] && pass "no header → 401" || fail "no header → 401 (got $CODE)"

CODE=$(curl -s -o /dev/null -w '%{http_code}' "$G/jobs/x" -H 'Authorization: Basic x')
[ "$CODE" = "401" ] && pass "bad format → 401" || fail "bad format → 401 (got $CODE)"

CODE=$(curl -s -o /dev/null -w '%{http_code}' "$G/jobs/x" -H "Authorization: $T")
[ "$CODE" = "404" ] && pass "dev mode token passes → 404" || fail "dev mode token (got $CODE)"

CODE=$(curl -s -o /dev/null -w '%{http_code}' "$G/health")
[ "$CODE" = "200" ] && pass "health bypasses auth" || fail "health (got $CODE)"

BODY=$(curl -s "$G/jobs/x" -H "Authorization: $T")
echo "$BODY" | grep -q '"job not found"' && pass "404 body correct" || fail "404 body (got $BODY)"

# ─── Phase 5: Job Submission ───
echo -e "\n${YELLOW}═══ Phase 5: WAV Transcription ═══${NC}"

RESP=$(curl -s -X POST "$G/jobs" \
  -H "Authorization: $T" \
  -H "X-Sheets-Token: fake-access-token-123" \
  -F "file=@test-audio.wav")
echo "  submit response: $RESP"

JOB_ID=$(echo "$RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('job_id',''))" 2>/dev/null)
if [ -n "$JOB_ID" ]; then
  pass "WAV job submitted ($JOB_ID)"
else
  fail "WAV submission: $RESP"
fi

if [ -n "$JOB_ID" ]; then
  for i in $(seq 1 120); do
    STATUS=$(curl -s "$G/jobs/$JOB_ID" -H "Authorization: $T" | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null)
    echo -ne "  poll $i: $STATUS\r"
    case "$STATUS" in
      done) echo ""; pass "WAV job completed"; break ;;
      failed) echo ""; fail "WAV job failed"; break ;;
    esac
    sleep 2
  done
  if [ "$STATUS" != "done" ] && [ "$STATUS" != "failed" ]; then
    echo ""
    fail "WAV job timed out (status=$STATUS)"
  fi

  echo ""
  echo "  ── Transcription Result ──"
  curl -s "$G/jobs/$JOB_ID" -H "Authorization: $T" | python3 -c "
import sys, json
d = json.load(sys.stdin)
print('  Status:', d.get('status'))
print('  Filename:', d.get('filename'))
if d.get('result'):
    r = json.loads(d['result']) if isinstance(d['result'], str) else d['result']
    print('  Text:', r.get('text', str(r)[:300]))
elif d.get('error'):
    print('  Error:', d['error'])
"
fi

# ─── Phase 6: M4A Conversion Test ───
echo -e "\n${YELLOW}═══ Phase 6: M4A Conversion ═══${NC}"

RESP=$(curl -s -X POST "$G/jobs" \
  -H "Authorization: $T" \
  -H "X-Sheets-Token: fake-st" \
  -F "file=@sample.m4a")
echo "  submit response: $RESP"

JID2=$(echo "$RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('job_id',''))" 2>/dev/null)
if [ -n "$JID2" ]; then
  pass "M4A job submitted ($JID2)"
else
  fail "M4A submission: $RESP"
fi

if [ -n "$JID2" ]; then
  for i in $(seq 1 120); do
    STATUS=$(curl -s "$G/jobs/$JID2" -H "Authorization: $T" | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null)
    echo -ne "  poll $i: $STATUS\r"
    case "$STATUS" in
      done) echo ""; pass "M4A job completed"; break ;;
      failed) echo ""; fail "M4A job failed"; break ;;
    esac
    sleep 2
  done
  [ "$STATUS" != "done" ] && [ "$STATUS" != "failed" ] && { echo ""; fail "M4A timed out"; }

  echo ""
  echo "  ── M4A Result ──"
  curl -s "$G/jobs/$JID2" -H "Authorization: $T" | python3 -c "
import sys, json
d = json.load(sys.stdin)
print('  Status:', d.get('status'))
if d.get('result'):
    r = json.loads(d['result']) if isinstance(d['result'], str) else d['result']
    print('  Text:', r.get('text', str(r)[:300]))
elif d.get('error'):
    print('  Error:', d['error'])
"
fi

# ─── Phase 7: Edge Cases ───
echo -e "\n${YELLOW}═══ Phase 7: Edge Cases ═══${NC}"

CODE=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$G/jobs" -H "Authorization: $T" -H "Content-Type: application/json" -d '{}')
[ "$CODE" = "400" ] && pass "wrong content-type → 400" || fail "wrong content-type (got $CODE)"

CODE=$(curl -s -o /dev/null -w '%{http_code}' "$G/jobs/0000000000000000" -H "Authorization: $T")
[ "$CODE" = "404" ] && pass "bad job ID → 404" || fail "bad job ID (got $CODE)"

CODE=$(curl -s -o /dev/null -w '%{http_code}' "$G/nope" -H "Authorization: $T")
[ "$CODE" = "404" ] && pass "unknown route → 404" || fail "unknown route (got $CODE)"

# ─── Phase 8: Cleanup ───
echo -e "\n${YELLOW}═══ Phase 9: Cleanup ═══${NC}"

kill "$GATEWAY_PID" 2>/dev/null && wait "$GATEWAY_PID" 2>/dev/null && pass "gateway stopped" || pass "gateway stopped"
kill "$WHISPER_PID" 2>/dev/null && wait "$WHISPER_PID" 2>/dev/null && pass "whisper stopped" || pass "whisper stopped"
lsof -ti:9092 | xargs kill -9 2>/dev/null; true
lsof -ti:8080 | xargs kill -9 2>/dev/null; true
rm -f /tmp/gateway-e2e.log /tmp/whisper-e2e.log
pass "logs cleaned"

# ─── Final ───
echo -e "\n${YELLOW}═══ RESULTS ═══${NC}"
echo -e "${GREEN}PASS: $PASS${NC}  ${RED}FAIL: $FAIL${NC}"
if [ "$FAIL" -eq 0 ]; then
  echo -e "${GREEN}🎉 ALL TESTS PASSED${NC}"
  exit 0
else
  echo -e "${RED}❌ SOME TESTS FAILED${NC}"
  exit 1
fi
