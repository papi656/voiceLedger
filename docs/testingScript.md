# E2E Testing Instructions

> For an AI agent to execute step-by-step. Use bash tool for commands. Read output of each step before proceeding.

## Architecture

```
Client (curl)
  │
  ▼
┌──────────────────────────────────────┐
│  Docker Compose (bridge network)     │
│                                      │
│  Gateway (:9090) ──▶ Whisper (:8080) │
│                                      │
│  ┌─ ffmpeg conversion                │
│  ├─ in-memory job queue              │
│  ├─ IP-based rate limiting           │
│  └─ no auth (MVP)                    │
│                                      │
│  Whisper uses ggml-medium-q8_0.bin   │
└──────────────────────────────────────┘
```

## Prerequisites (verify first)

Run these checks before starting any containers. All paths relative to the project root (`packagedAudioTranscription/`).

```bash
# 1. Docker is running
docker info > /dev/null 2>&1 && echo "Docker: OK" || echo "Docker: NOT RUNNING"
```
Expected: `Docker: OK`

```bash
# 2. docker compose (or docker-compose) available
docker compose version
```
Expected: version string (v2.x+)

```bash
# 3. Model file exists
ls -lh whisper-package/models/ggml-medium-q8_0.bin
```
Expected: file exists (~785 MB)

```bash
# 4. Whisper server binary exists (Linux ELF for Docker)
file whisper-package/whisper-server-linux
```
Expected: shows ELF 64-bit executable (x86-64 or ARM aarch64 depending on host)

```bash
# 5. Test audio files exist
ls -lh test-fixtures/test-audio.wav test-fixtures/sample.m4a
```
Expected: both files exist

```bash
# 6. Python3 available (for JSON parsing in polling loop)
which python3
```
Expected: a path

---

## Phase 1: Start Both Services with Docker Compose

Build images (if not already built) and start both containers. The gateway depends on
whisper being healthy, so `docker compose` handles the startup order automatically.

```bash
docker compose up -d --build
```

Wait for both services to be healthy (gateway depends on whisper, so once gateway
healthcheck passes, both are ready):

```bash
for i in $(seq 1 60); do
  if curl -sf http://127.0.0.1:9090/health | grep -q '"ok"'; then
    echo "services ready after ${i}s"
    break
  fi
  sleep 1
done
```

Verify gateway health:

```bash
curl -s http://127.0.0.1:9090/health
```

Expected: `{"status":"ok","service":"whisper-gateway","version":"1.0.0"}`

Verify container status:

```bash
docker compose ps
```

Expected: both `whisper` and `gateway` show as `Up` (healthy).

---

## Phase 3: WAV Transcription (end-to-end)

### 3a. Submit a WAV file for transcription

```bash
G="http://127.0.0.1:9090"
curl -s -X POST "$G/jobs" -F "file=@test-fixtures/test-audio.wav"
```

Expected: `{"job_id":"<hex>","status":"queued"}` — HTTP 202. Save the `job_id` for the next step.

### 3b. Poll the job status until completed

Replace `<JOB_ID>` with the actual ID from step 3a:

```bash
JOB_ID="<JOB_ID>"
for i in $(seq 1 60); do
  STATUS=$(curl -s "$G/jobs/$JOB_ID" | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))")
  echo "poll $i: $STATUS"
  if [ "$STATUS" = "done" ] || [ "$STATUS" = "failed" ]; then
    break
  fi
  sleep 2
done
```

Expected: status transitions `queued` → `processing` → `done`. Should complete within 2 minutes for the short test-audio.wav.

### 3c. Inspect the result

```bash
curl -s "$G/jobs/$JOB_ID"
```

Expected JSON:
```json
{
  "job_id": "...",
  "status": "done",
  "filename": "test-audio.wav",
  "result": "...",
  "error": "",
  "created_at": "...",
  "updated_at": "..."
}
```

The `result` field contains the transcription text. Verify it's non-empty and matches the audio content. The test-audio.wav contains a high-pitched tone — the model may transcribe it as silence or a tone description.

---

## Phase 4: M4A Conversion + Transcription

M4A files need ffmpeg conversion to 16kHz mono WAV before sending to whisper.

### 4a. Submit sample.m4a

```bash
curl -s -X POST "$G/jobs" -F "file=@test-fixtures/sample.m4a"
```

Expected: `{"job_id":"<hex>","status":"queued"}`

### 4b. Poll until done

Same pattern as 3b with the new job ID. Expected to complete with status `done` and a transcription of the audio content in `result`.

---

## Phase 5: Error Cases

### 5a. Wrong Content-Type → 400

```bash
curl -s -o /dev/null -w "%{http_code}" -X POST "$G/jobs" \
  -H "Content-Type: application/json" \
  -d '{}'
```

Expected: `400`

### 5b. Non-existent job ID → 404

```bash
curl -s "$G/jobs/0000000000000000"
```

Expected: `{"error":"job not found"}`

### 5c. Unknown route → 404

```bash
curl -s -o /dev/null -w "%{http_code}" "$G/nonexistent"
```

Expected: `404`

---

## Phase 6: Cleanup

Stop and remove containers, networks, and volumes created by docker compose.

```bash
docker compose down
```

Verify nothing is left running (no containers, port 9090 freed):

```bash
docker compose ps -a
lsof -ti:9090
```

Expected:
- `docker compose ps -a`: no containers listed (or all "exited")
- `lsof -ti:9090`: no output (port freed)

---

## Summary: Pass Criteria

| # | Test | Expected Result |
|---|---|---|
| P1 | Docker running | `Docker: OK` |
| P2 | docker compose | version string (v2.x+) |
| P3 | Model exists | ~785 MB file |
| P4 | Whisper binary exists (Linux ELF) | `ELF 64-bit` (x86-64 or ARM) |
| P5 | Test audio files exist | both files listed |
| P6 | Python3 available | a path |
| 1 | Docker compose up | both containers healthy |
| 3a | WAV submission | 202 with `job_id` |
| 3b | WAV processing | status becomes `done` |
| 3c | WAV result | `result` contains transcription text |
| 4a | M4A submission | 202 with `job_id` |
| 4b | M4A processing | status becomes `done` |
| 4c | M4A result | `result` contains transcription text |
| 5a | Wrong Content-Type | 400 |
| 5b | Bad job ID | 404 |
| 5c | Unknown route | 404 |
| 6 | Cleanup | no containers, port 9090 freed |
