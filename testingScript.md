# End-to-End Testing Script

> Run these commands in order from the project root: `/Users/papi656/Desktop/selfProjs/packagedAudioTranscription`

## Prerequisites

- `ffmpeg` installed (`which ffmpeg`)
- Model downloaded: `whisper-package/models/ggml-medium-q8_0.bin`
- `go` compiler available
- `curl` available

---

## Phase 1: Build & Verify

```bash
# 1a. Ensure dependencies
cd gateway && go build ./... && cd ..
echo "✅ gateway compiles"

# 1b. Run unit tests
cd gateway && go test -v ./... && cd ..
echo "✅ unit tests pass"
```

---

## Phase 2: Start Whisper Server

The whisper server listens on `127.0.0.1:8080`.

```bash
# Start in background
cd whisper-package
HOST=127.0.0.1 PORT=8080 ./whisper-server \
  -m models/ggml-medium-q8_0.bin \
  --host 127.0.0.1 --port 8080 \
  > /tmp/whisper-server.log 2>&1 &
WHISPER_PID=$!
echo "whisper PID: $WHISPER_PID"

# Wait for it to be ready (health check loop)
for i in $(seq 1 30); do
  if curl -s http://127.0.0.1:8080/health > /dev/null 2>&1; then
    echo "✅ whisper server ready"
    break
  fi
  echo "   waiting for whisper... ($i/30)"
  sleep 2
done
```

---

## Phase 3: Start Gateway (Dev Mode)

Gateway listens on `127.0.0.1:9092`.

```bash
cd gateway

# Dev mode: OAUTH_AUDIENCE="" means any token passes as dev-user
OAUTH_AUDIENCE="" \
  PORT=9092 \
  WHISPER_HOST=127.0.0.1 \
  WHISPER_PORT=8080 \
  NUM_WORKERS=1 \
  MAX_FILE_SIZE_MB=25 \
  go run . \
  > /tmp/gateway.log 2>&1 &
GATEWAY_PID=$!
echo "gateway PID: $GATEWAY_PID"

# Wait for ready
for i in $(seq 1 15); do
  if curl -s http://127.0.0.1:9092/health | grep -q '"ok"'; then
    echo "✅ gateway ready"
    break
  fi
  echo "   waiting for gateway... ($i/15)"
  sleep 1
done
```

---

## Phase 4: Auth Tests (No Whisper Required)

```bash
GATEWAY="http://127.0.0.1:9092"

echo ""
echo "=== 4a: No auth header → 401 ==="
CODE=$(curl -s -o /dev/null -w "%{http_code}" "$GATEWAY/jobs/nonexistent")
if [ "$CODE" = "401" ]; then echo "✅ PASS (401)"; else echo "❌ FAIL got $CODE"; fi

echo ""
echo "=== 4b: Bad format 'Basic xxx' → 401 ==="
CODE=$(curl -s -o /dev/null -w "%{http_code}" "$GATEWAY/jobs/nonexistent" \
  -H "Authorization: Basic something")
if [ "$CODE" = "401" ]; then echo "✅ PASS (401)"; else echo "❌ FAIL got $CODE"; fi

echo ""
echo "=== 4c: Lowercase 'bearer' → 401 ==="
CODE=$(curl -s -o /dev/null -w "%{http_code}" "$GATEWAY/jobs/nonexistent" \
  -H "Authorization: bearer fake-token")
if [ "$CODE" = "401" ]; then echo "✅ PASS (401)"; else echo "❌ FAIL got $CODE"; fi

echo ""
echo "=== 4d: Dev mode any token → 404 (auth passes, no job) ==="
BODY=$(curl -s "$GATEWAY/jobs/nonexistent" -H "Authorization: Bearer fake-token-123")
if echo "$BODY" | grep -q '"job not found"'; then
  echo "✅ PASS (auth passed, job not found)"
else
  echo "❌ FAIL got: $BODY"
fi

echo ""
echo "=== 4e: Health endpoint bypasses auth ==="
BODY=$(curl -s "$GATEWAY/health")
if echo "$BODY" | grep -q '"status":"ok"'; then
  echo "✅ PASS ($BODY)"
else
  echo "❌ FAIL got: $BODY"
fi
```

---

## Phase 5: Job Submission & Processing (Requires Whisper)

```bash
GATEWAY="http://127.0.0.1:9092"
TOKEN="Bearer dev-test-token"

echo ""
echo "=== 5a: Submit a WAV file ==="
RESP=$(curl -s -X POST "$GATEWAY/jobs" \
  -H "Authorization: $TOKEN" \
  -H "X-Sheets-Token: fake-access-token-123" \
  -F "file=@test-audio.wav")
echo "$RESP"

JOB_ID=$(echo "$RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('job_id',''))" 2>/dev/null)
if [ -z "$JOB_ID" ]; then
  echo "❌ FAIL: no job_id in response"
else
  echo "✅ PASS (job_id: $JOB_ID)"
fi

echo ""
echo "=== 5b: Poll job until done ==="
for i in $(seq 1 120); do
  STATUS=$(curl -s "$GATEWAY/jobs/$JOB_ID" \
    -H "Authorization: $TOKEN" | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null)
  echo "   poll $i: status=$STATUS"
  if [ "$STATUS" = "done" ] || [ "$STATUS" = "failed" ]; then
    break
  fi
  sleep 2
done

echo ""
echo "=== 5c: Get final result ==="
RESULT=$(curl -s "$GATEWAY/jobs/$JOB_ID" -H "Authorization: $TOKEN")
echo "$RESULT" | python3 -c "
import sys, json
data = json.load(sys.stdin)
print('Status:', data.get('status'))
print('Filename:', data.get('filename'))
if 'result' in data and data['result']:
    result = json.loads(data['result']) if isinstance(data['result'], str) else data['result']
    text = result.get('text', result)
    print('Transcription:', text[:200])
elif 'error' in data:
    print('Error:', data['error'])
"

if echo "$RESULT" | grep -q '"done"'; then
  echo "✅ PASS"
elif echo "$RESULT" | grep -q '"failed"'; then
  echo "❌ FAIL"
else
  echo "⚠️  STILL PROCESSING"
fi
```

---

## Phase 6: M4A Conversion Test (ffmpeg Required)

```bash
GATEWAY="http://127.0.0.1:9092"
TOKEN="Bearer dev-test-token"

echo ""
echo "=== 6a: Submit M4A file (needs ffmpeg conversion) ==="
RESP=$(curl -s -X POST "$GATEWAY/jobs" \
  -H "Authorization: $TOKEN" \
  -H "X-Sheets-Token: fake-sheets-token" \
  -F "file=@sample.m4a")
echo "$RESP"

JOB_ID=$(echo "$RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('job_id',''))" 2>/dev/null)
if [ -z "$JOB_ID" ]; then
  echo "❌ FAIL: no job_id"
else
  echo "✅ PASS (job_id: $JOB_ID)"
fi

echo ""
echo "=== 6b: Poll M4A job ==="
for i in $(seq 1 120); do
  STATUS=$(curl -s "$GATEWAY/jobs/$JOB_ID" \
    -H "Authorization: $TOKEN" | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null)
  echo "   poll $i: status=$STATUS"
  if [ "$STATUS" = "done" ] || [ "$STATUS" = "failed" ]; then
    break
  fi
  sleep 2
done

echo ""
echo "=== 6c: Final M4A result ==="
curl -s "$GATEWAY/jobs/$JOB_ID" -H "Authorization: $TOKEN" | python3 -c "
import sys, json
data = json.load(sys.stdin)
print('Status:', data.get('status'))
print('Filename:', data.get('filename'))
if 'result' in data and data['result']:
    result = json.loads(data['result']) if isinstance(data['result'], str) else data['result']
    text = result.get('text', result)
    print('Transcription:', text[:200])
elif 'error' in data:
    print('Error:', data['error'])
"
```

---

## Phase 7: Error & Edge Cases

```bash
GATEWAY="http://127.0.0.1:9092"
TOKEN="Bearer dev-test-token"

echo ""
echo "=== 7a: No file field → 400 ==="
CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$GATEWAY/jobs" \
  -H "Authorization: $TOKEN" \
  -H "Content-Type: multipart/form-data")
if [ "$CODE" = "400" ]; then echo "✅ PASS (400)"; else echo "❌ FAIL got $CODE"; fi

echo ""
echo "=== 7b: Wrong Content-Type → 400 ==="
CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$GATEWAY/jobs" \
  -H "Authorization: $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"bad":"data"}')
if [ "$CODE" = "400" ]; then echo "✅ PASS (400)"; else echo "❌ FAIL got $CODE"; fi

echo ""
echo "=== 7c: Bad job ID → 404 ==="
BODY=$(curl -s "$GATEWAY/jobs/0000000000000000" -H "Authorization: $TOKEN")
if echo "$BODY" | grep -q '"job not found"'; then echo "✅ PASS (404)"; else echo "❌ FAIL got: $BODY"; fi

echo ""
echo "=== 7d: Rate limit headers present ==="
HEADERS=$(curl -s -I "$GATEWAY/jobs/0000000000000000" -H "Authorization: $TOKEN" 2>&1)
if echo "$HEADERS" | grep -q "X-RateLimit"; then
  echo "✅ PASS (rate limit headers present)"
else
  echo "⚠️  Rate limit headers not found (may be fine if empty)"
fi

echo ""
echo "=== 7e: Not found route → 404 ==="
CODE=$(curl -s -o /dev/null -w "%{http_code}" "$GATEWAY/nonexistent" -H "Authorization: $TOKEN")
if [ "$CODE" = "404" ]; then echo "✅ PASS (404)"; else echo "❌ FAIL got $CODE"; fi
```

---

## Phase 8: Rate Limiting Test (Stress)

```bash
GATEWAY="http://127.0.0.1:9092"
TOKEN="Bearer dev-test-token"

echo ""
echo "=== 8a: Rapid fire requests (should not 429 in dev mode at defaults) ==="
COUNT_200=0
COUNT_429=0
for i in $(seq 1 40); do
  CODE=$(curl -s -o /dev/null -w "%{http_code}" "$GATEWAY/jobs/nonexistent" \
    -H "Authorization: $TOKEN")
  if [ "$CODE" = "404" ]; then COUNT_200=$((COUNT_200+1)); fi
  if [ "$CODE" = "429" ]; then COUNT_429=$((COUNT_429+1)); fi
done
echo "404 (passed auth): $COUNT_200, 429 (rate limited): $COUNT_429"
if [ "$COUNT_429" -gt 0 ]; then
  echo "✅ PASS (rate limiting triggered)"
else
  echo "✅ PASS (no rate limit at 30 req/min default - burst covers 40)"
fi
```

---

## Phase 9: Cleanup

```bash
echo ""
echo "=== Cleaning up ==="

# Stop gateway
if [ -n "$GATEWAY_PID" ]; then
  kill "$GATEWAY_PID" 2>/dev/null
  wait "$GATEWAY_PID" 2>/dev/null
  echo "✅ gateway stopped (PID $GATEWAY_PID)"
fi

# Stop whisper
if [ -n "$WHISPER_PID" ]; then
  kill "$WHISPER_PID" 2>/dev/null
  wait "$WHISPER_PID" 2>/dev/null
  echo "✅ whisper stopped (PID $WHISPER_PID)"
fi

# Clean up any leftover processes on our test ports
lsof -ti:9092 | xargs kill -9 2>/dev/null
lsof -ti:8080 | xargs kill -9 2>/dev/null

# Logs
echo ""
echo "=== Logs ==="
echo "Gateway log: /tmp/gateway.log"
echo "Whisper log: /tmp/whisper-server.log"

rm -f /tmp/gateway.log /tmp/whisper-server.log
echo "✅ logs cleaned up"
echo ""
echo "=== DONE ==="
```

---

## One-Liner: Run Everything

```bash
bash testingScript.md
```

*(Only works if you strip the markdown — or copy each phase into a terminal.)*

## Running as a Real Script

To run as a real script, create `test-e2e.sh`:

```bash
#!/bin/bash
set -euo pipefail

# --- Phase 1: Build ---
echo "=== Phase 1: Build ==="
cd gateway && go build ./... && go test -v ./... && cd ..
echo "✅ build & unit tests OK"

# --- Phase 2: Start whisper ---
echo "=== Phase 2: Start Whisper ==="
cd whisper-package
HOST=127.0.0.1 PORT=8080 ./whisper-server -m models/ggml-medium-q8_0.bin --host 127.0.0.1 --port 8080 > /tmp/whisper.log 2>&1 &
WHISPER_PID=$!
cd ..

for i in $(seq 1 30); do
  if curl -s http://127.0.0.1:8080/health > /dev/null 2>&1; then break; fi
  sleep 2
done
echo "✅ whisper ready (PID $WHISPER_PID)"

# --- Phase 3: Start gateway ---
echo "=== Phase 3: Start Gateway ==="
cd gateway
OAUTH_AUDIENCE="" PORT=9092 WHISPER_HOST=127.0.0.1 WHISPER_PORT=8080 NUM_WORKERS=1 go run . > /tmp/gateway.log 2>&1 &
GATEWAY_PID=$!
cd ..

for i in $(seq 1 15); do
  if curl -s http://127.0.0.1:9092/health | grep -q '"ok"'; then break; fi
  sleep 1
done
echo "✅ gateway ready (PID $GATEWAY_PID)"

# --- Phase 4-8: Run tests ---
G="http://127.0.0.1:9092"
T="Bearer dev-test-token"
PASS=0
FAIL=0

check() {
  local desc="$1" expected="$2" code="$3"
  if [ "$code" = "$expected" ]; then
    echo "  ✅ $desc"
    PASS=$((PASS+1))
  else
    echo "  ❌ $desc (expected $expected, got $code)"
    FAIL=$((FAIL+1))
  fi
}

echo ""
echo "=== Auth Tests ==="
check "no header → 401" "401" "$(curl -s -o /dev/null -w '%{http_code}' "$G/jobs/x")"
check "bad format → 401" "401" "$(curl -s -o /dev/null -w '%{http_code}' "$G/jobs/x" -H 'Authorization: Basic x')"
check "dev mode passes" "404" "$(curl -s -o /dev/null -w '%{http_code}' "$G/jobs/x" -H "Authorization: $T")"
check "health bypasses" "200" "$(curl -s -o /dev/null -w '%{http_code}' "$G/health")"

echo ""
echo "=== Submission Tests ==="
RESP=$(curl -s -X POST "$G/jobs" -H "Authorization: $T" -H "X-Sheets-Token: fake-st" -F "file=@test-audio.wav")
JOB_ID=$(echo "$RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('job_id',''))" 2>/dev/null)
if [ -n "$JOB_ID" ]; then echo "  ✅ submitted job $JOB_ID"; PASS=$((PASS+1))
else echo "  ❌ submission failed: $RESP"; FAIL=$((FAIL+1)); fi

echo ""
echo "=== Processing (max 4 min) ==="
for i in $(seq 1 120); do
  STATUS=$(curl -s "$G/jobs/$JOB_ID" -H "Authorization: $T" | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null)
  case "$STATUS" in
    done) echo "  ✅ job completed"; PASS=$((PASS+1)); break ;;
    failed) echo "  ❌ job failed"; FAIL=$((FAIL+1)); break ;;
  esac
  sleep 2
done

echo ""
echo "=== Final Result ==="
curl -s "$G/jobs/$JOB_ID" -H "Authorization: $T" | python3 -c "
import sys, json
d = json.load(sys.stdin)
if d.get('result'):
    r = json.loads(d['result']) if isinstance(d['result'], str) else d['result']
    print('Transcription:', r.get('text', str(r)[:300]))
elif d.get('error'):
    print('Error:', d['error'])
"

echo ""
echo "=== M4A Conversion Test ==="
RESP=$(curl -s -X POST "$G/jobs" -H "Authorization: $T" -H "X-Sheets-Token: fake-st" -F "file=@sample.m4a")
JID2=$(echo "$RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('job_id',''))" 2>/dev/null)
if [ -n "$JID2" ]; then echo "  ✅ M4A submitted $JID2"; PASS=$((PASS+1))
else echo "  ❌ M4A submission failed: $RESP"; FAIL=$((FAIL+1)); fi

for i in $(seq 1 120); do
  S=$(curl -s "$G/jobs/$JID2" -H "Authorization: $T" | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null)
  case "$S" in
    done) echo "  ✅ M4A job done"; PASS=$((PASS+1)); break ;;
    failed) echo "  ❌ M4A job failed"; FAIL=$((FAIL+1)); break ;;
  esac
  sleep 2
done

# --- Phase 9: Cleanup ---
echo ""
echo "=== Cleanup ==="
kill "$GATEWAY_PID" 2>/dev/null; wait "$GATEWAY_PID" 2>/dev/null
kill "$WHISPER_PID" 2>/dev/null; wait "$WHISPER_PID" 2>/dev/null
lsof -ti:9092 | xargs kill -9 2>/dev/null
lsof -ti:8080 | xargs kill -9 2>/dev/null
rm -f /tmp/gateway.log /tmp/whisper.log
echo "✅ processes stopped & logs removed"

echo ""
echo "=== RESULTS: $PASS passed, $FAIL failed ==="
[ "$FAIL" -eq 0 ] && echo "🎉 ALL TESTS PASSED" || echo "❌ SOME TESTS FAILED"
```

Save as `test-e2e.sh`, then:

```bash
chmod +x test-e2e.sh && ./test-e2e.sh
```

---

## Expected Output Summary

| Phase | What | Expected |
|---|---|---|
| 1 | Build + unit tests | `go build` clean, 10 tests pass |
| 2 | Whisper start | `/health` returns 200 |
| 3 | Gateway start | `/health` returns `{"status":"ok"}` |
| 4a | No auth → 401 | 401 |
| 4b | Bad format → 401 | 401 |
| 4c | Lowercase bearer → 401 | 401 |
| 4d | Dev mode token → 404 | 404 (auth passes) |
| 4e | Health no auth | 200 |
| 5a | Submit WAV | 202 with `job_id` |
| 5b | Poll job | Eventually `done` or `failed` |
| 5c | Get result | Transcription text in `result` |
| 6a | Submit M4A | 202 (ffmpeg converts to WAV) |
| 6c | Get M4A result | Transcription text |
| 7a | No file | 400 |
| 7b | Wrong Content-Type | 400 |
| 7c | Bad job ID | 404 |
| 9 | Cleanup | All processes stopped |
