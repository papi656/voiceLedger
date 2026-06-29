# E2E Testing Instructions

> For an AI agent to execute step-by-step. Use bash tool for commands. Read output of each step before proceeding.

## Architecture

```
Client (curl)
  │
  ▼
Gateway (:9092) ──auth──▶ Google ID token verification
  │
  ▼
Whisper Server (:8080) ──▶ ggml-medium-q8_0.bin model
```

## Prerequisites (verify first)

Run these checks before starting any servers:

```bash
# 1. ffmpeg available
which ffmpeg
```
Expected: a path like `/opt/homebrew/bin/ffmpeg`

```bash
# 2. Model file exists
ls -lh whisper-package/models/ggml-medium-q8_0.bin
```
Expected: file exists (~785 MB)

```bash
# 3. Whisper server binary exists
file whisper-package/whisper-server
```
Expected: shows executable path

```bash
# 4. Test audio files exist
ls -lh test-audio.wav sample.m4a
```
Expected: both files exist

```bash
# 5. Python3 available (for JSON parsing)
which python3
```
Expected: a path

```bash
# 6. Gateway compiles
cd gateway && go build ./... && cd ..
```
Expected: no output (clean build)

```bash
# 7. Unit tests pass
cd gateway && go test -v ./... && cd ..
```
Expected: all tests pass, exit code 0

---

## Phase 1: Start Whisper Server

Start the whisper server in the background on port 8080.

```bash
cd whisper-package && ./whisper-server -m models/ggml-medium-q8_0.bin --host 127.0.0.1 --port 8080 > /tmp/whisper.log 2>&1 &
echo "whisper PID: $!"
```

Wait for it to be ready by polling the health endpoint:

```bash
for i in $(seq 1 30); do
  if curl -sf http://127.0.0.1:8080/health > /dev/null 2>&1; then
    echo "whisper ready after ${i}s"
    break
  fi
  sleep 1
done
```

Verify:

```bash
curl -s http://127.0.0.1:8080/health
```

Expected: any HTTP 200 response (whisper health endpoint exists).

---

## Phase 2: Start Gateway

Start the gateway in dev mode (`OAUTH_AUDIENCE=""`) on port 9092.

```bash
cd gateway && OAUTH_AUDIENCE="" PORT=9092 WHISPER_HOST=127.0.0.1 WHISPER_PORT=8080 NUM_WORKERS=1 go run . > /tmp/gateway.log 2>&1 &
echo "gateway PID: $!"
```

Wait for it to be ready:

```bash
for i in $(seq 1 15); do
  if curl -sf http://127.0.0.1:9092/health | grep -q '"ok"'; then
    echo "gateway ready after ${i}s"
    break
  fi
  sleep 1
done
```

Verify:

```bash
curl -s http://127.0.0.1:9092/health
```

Expected: `{"status":"ok","service":"whisper-gateway","version":"1.0.0"}`

---

## Phase 3: Auth Tests

Set a convenience variable first:

```bash
G="http://127.0.0.1:9092"
T="Bearer dev-test-token"
```

### 3a. No Authorization header → 401

```bash
curl -s -o /dev/null -w "%{http_code}" "$G/jobs/nonexistent"
```

Expected: `401`

### 3b. Bad format ("Basic" instead of "Bearer") → 401

```bash
curl -s -o /dev/null -w "%{http_code}" "$G/jobs/nonexistent" -H "Authorization: Basic something"
```

Expected: `401`

### 3c. Lowercase "bearer" → 401

```bash
curl -s -o /dev/null -w "%{http_code}" "$G/jobs/nonexistent" -H "Authorization: bearer fake-token"
```

Expected: `401` (the code checks for exact prefix `"Bearer "`, not case-insensitive)

### 3d. Dev mode: any token passes auth → 404

```bash
curl -s "$G/jobs/nonexistent" -H "Authorization: $T"
```

Expected: `{"error":"job not found"}` — this means auth passed, the 404 is because no such job exists.

### 3e. Health endpoint bypasses auth

```bash
curl -s "$G/health"
```

Expected: `{"status":"ok","service":"whisper-gateway","version":"1.0.0"}`

---

## Phase 4: WAV Transcription (end-to-end)

### 4a. Submit a WAV file for transcription

```bash
curl -s -X POST "$G/jobs" \
  -H "Authorization: $T" \
  -H "X-Sheets-Token: fake-access-token-123" \
  -F "file=@test-audio.wav"
```

Expected: `{"job_id":"<hex>","status":"queued"}` — HTTP 202. Save the `job_id` for the next step.

### 4b. Poll the job status until completed

Replace `<JOB_ID>` with the actual ID from step 4a:

```bash
JOB_ID="<JOB_ID>"
for i in $(seq 1 60); do
  STATUS=$(curl -s "$G/jobs/$JOB_ID" -H "Authorization: $T" | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))")
  echo "poll $i: $STATUS"
  if [ "$STATUS" = "done" ] || [ "$STATUS" = "failed" ]; then
    break
  fi
  sleep 2
done
```

Expected: status transitions `queued` → `processing` → `done`. Should complete within 2 minutes for the short test-audio.wav.

### 4c. Inspect the result

```bash
curl -s "$G/jobs/$JOB_ID" -H "Authorization: $T"
```

Expected JSON:
```json
{
  "job_id": "...",
  "status": "done",
  "filename": "test-audio.wav",
  "result": "...",
  "created_at": "...",
  "updated_at": "..."
}
```

The `result` field contains the transcription text. Verify it's non-empty and matches the audio content. The test-audio.wav contains a high-pitched tone — the model may transcribe it as silence or a tone description.

---

## Phase 5: M4A Conversion + Transcription

M4A files need ffmpeg conversion to 16kHz mono WAV before sending to whisper.

### 5a. Submit sample.m4a

```bash
curl -s -X POST "$G/jobs" \
  -H "Authorization: $T" \
  -H "X-Sheets-Token: fake-sheets-token" \
  -F "file=@sample.m4a"
```

Expected: `{"job_id":"<hex>","status":"queued"}`

### 5b. Poll until done

Same pattern as 4b with the new job ID. Expected to complete with status `done` and a transcription of the audio content in `result`.

---

## Phase 6: Error Cases

### 6a. Wrong Content-Type → 400

```bash
curl -s -o /dev/null -w "%{http_code}" -X POST "$G/jobs" \
  -H "Authorization: $T" \
  -H "Content-Type: application/json" \
  -d '{}'
```

Expected: `400`

### 6b. Non-existent job ID → 404

```bash
curl -s "$G/jobs/0000000000000000" -H "Authorization: $T"
```

Expected: `{"error":"job not found"}`

### 6c. Unknown route → 404

```bash
curl -s -o /dev/null -w "%{http_code}" "$G/nonexistent" -H "Authorization: $T"
```

Expected: `404`

---

## Phase 7: Cleanup

Stop all processes and remove temp logs.

```bash
# Stop gateway
pkill -f "gateway.*9092" 2>/dev/null || true
# Ensure port is freed
lsof -ti:9092 | xargs kill -9 2>/dev/null || true

# Stop whisper server
pkill -f "whisper-server.*8080" 2>/dev/null || true
# Ensure port is freed
lsof -ti:8080 | xargs kill -9 2>/dev/null || true

# Remove temp logs
rm -f /tmp/gateway.log /tmp/whisper.log
```

Verify nothing is left running:

```bash
lsof -ti:9092 -ti:8080
```

Expected: no output (no processes on those ports).

---

## Summary: Pass Criteria

| # | Test | Expected Result |
|---|---|---|
| P1 | ffmpeg exists | path printed |
| P2 | Model exists | ~785 MB file |
| P3 | Gateway builds | no errors |
| P4 | Unit tests | all pass |
| P5 | Whisper starts | health returns 200 |
| P6 | Gateway starts | health returns `{"status":"ok"}` |
| 3a | No auth header | 401 |
| 3b | Bad format | 401 |
| 3c | Lowercase bearer | 401 |
| 3d | Dev mode token | 404 (auth passes) |
| 3e | Health bypasses auth | 200 |
| 4a | WAV submission | 202 with job_id |
| 4b | WAV processing | status becomes `done` |
| 4c | WAV result | `result` contains transcription text |
| 5a | M4A submission | 202 with job_id |
| 5b | M4A processing | status becomes `done` |
| 6a | Wrong content-type | 400 |
| 6b | Bad job ID | 404 |
| 6c | Unknown route | 404 |
| 7 | Cleanup | no processes on 9092 or 8080 |
