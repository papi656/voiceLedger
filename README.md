# Packaged Audio Transcription

A self-hosted audio transcription service — accepts audio files via HTTP, queues
them, converts to WAV, runs [whisper.cpp](https://github.com/ggerganov/whisper.cpp)
inference, and returns the transcript. Ships as two Docker containers behind a single
API gateway.

```
Browser / Client
      │
      ▼
┌─────────────┐     ┌──────────────────┐
│   gateway   │────▶│  whisper-server  │
│  (Go, :9090)│     │ (C++, :8080)     │
│             │     │                   │
│ • rate limit│     │ • ggml-medium-q8  │
│ • ffmpeg    │     │ • 16kHz mono WAV  │
│ • job queue │     │ • JSON response   │
│             │     └──────────────────┘
│             │
│             │     ┌──────────────────┐
│             │────▶│  llm-server      │
└─────────────┘     │ (llama.cpp,:8081)│
                    │ • structured     │
                    │   extraction    │
                    └──────────────────┘
```

> **MVP — No authentication.** All endpoints are open. IP-based rate limiting only.

## Quick start

```bash
# First time only — compiles whisper-server and llm-server for Linux (~5-10 min)
./scripts/build-whisper-linux.sh
./scripts/build-llm-linux.sh

# Start both services
docker compose up -d

# Verify
curl http://localhost:9090/health
# → {"service":"whisper-gateway","status":"ok","version":"1.0.0"}

# Transcribe an audio file
curl -X POST http://localhost:9090/jobs \
  -F "file=@test-fixtures/sample.m4a"
# → {"job_id":"abc123...","status":"queued"}

# Poll for result
curl http://localhost:9090/jobs/abc123...
# → {"job_id":"abc123...","status":"done","result":{...}}
```

After transcription, the gateway sends the text to an LLM server (llama.cpp, gemma model) for structured extraction: `result.extraction` contains `{price, place, category, date}`. The date is extracted from the transcription text (e.g. "yesterday", "last Monday") and falls back to today's date when none is mentioned. If you know what kind of purchase it is (e.g. the receipt came from a store whose category the model can't guess), pass a `category` field with the upload and the LLM will use it as a fallback whenever the text doesn't make the category clear:

```bash
curl -X POST http://localhost:9090/jobs \
  -F "file=@test-fixtures/sample.m4a" \
  -F "category=groceries"
```

Extraction is retried up to `LLM_MAX_RETRIES` times (default 3, exponential backoff); if it still fails, the job is marked `failed` with the error.

## API

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/health` | None | Health check |
| `POST` | `/jobs` | None | Submit audio (multipart/form-data, `file` field, optional `category` hint) |
| `GET` | `/jobs/{id}` | None | Poll job status and result |
| `GET` | `/sheets` | None | List Google Sheets tabs + Type categories (`enabled`, `default`, `tabs`, `categories`) |

**Rate limits** (default): 30 req/min per IP. Headers:
`X-RateLimit-Remaining-IP`, `Retry-After`.

## Project structure

```
.
├── gateway/                    # Go API gateway (cmd/ + internal/ layout)
│   ├── cmd/gateway/main.go     # Entry point — wiring, signals, shutdown
│   ├── internal/
│   │   ├── config/             # Environment-based configuration
│   │   ├── server/             # Route registration, CORS, body limits
│   │   ├── ratelimit/          # Per-IP token bucket rate limiting
│   │   ├── transcription/      # Job model, store, worker queue, handlers
│   │   ├── whisper/            # HTTP client for whisper-server
│   │   ├── audio/              # ffmpeg converter, format validation
│   │   └── httputil/           # HTTP error types and helpers
│   ├── Dockerfile
│   ├── go.mod
│   └── go.sum
│
├── whisper-package/            # Whisper inference server
│   ├── Dockerfile              # Slim runtime — just COPY + ldconfig
│   ├── whisper-server-linux   # Pre-compiled ELF binary
│   ├── lib*.so.*               # Shared libraries for inference
│   ├── models/                 # Model files (ggml-medium-q8_0.bin)
│   └── start.sh               # Local macOS test script
│
├── llm-package/             # LLM extraction server
│   ├── Dockerfile           # Slim runtime — just COPY + ldconfig
│   ├── llama-server-linux   # Pre-compiled ELF binary
│   ├── lib*.so.*            # Shared libraries for inference
│   ├── models/              # Model files (gemma-4-E4B-it-Q4_0.gguf)
│   └── start.sh             # Local macOS test script
│
├── scripts/
│   ├── build-whisper-linux.sh  # One-time: compile whisper for Linux in Docker
│   └── build-llm-linux.sh      # One-time: compile llm server for Linux in Docker
│
├── test-fixtures/              # Sample audio files for testing
│   ├── sample.m4a
│   └── test-audio.wav
│
├── docs/
│   ├── whisper-build-explanation.md   # How the build system works
│   └── expose-internet.md             # Public internet via Tailscale Funnel
│
└── docker-compose.yml          # Orchestrates gateway + whisper + llm
```

## Google Sheets export (optional)

After a successful job, the gateway appends the extracted details to a Google
Spreadsheet as a row in the spreadsheet's own style (the April, 2026 layout):

`Date | Type | Amount | Comments` — no job metadata is written to the sheet

- **Date** — a real date (Excel serial), rendered per the tab's date format
- **Type** — the extracted category, constrained to the sheet's own Type
  dropdown values (read from the tab's data validation at startup — e.g.
  `Grocery, Shopping, Utilities, Travel, Household`), so rows always satisfy
  the sheet's strict validation
- **Amount** — the **numeric value only, no currency symbol** (`3000 yen` → `3000`,
  `¥2,853` → `2853`), written as a plain number so SUM-style formulas work
- **Comments** — the extracted place (e.g. `Sanwa`)

The appended row gets the date format applied on A (matching rows entered by
hand); the amount stays a bare number with no currency symbol.

Setup (one-time, ~10 min):

1. Google Cloud console → create a project (any Google account works, no billing needed)
2. Enable the **Google Sheets API**
3. IAM & Admin → **Service Accounts** → create one → **Add Key → JSON** → save as `secrets/service-account.json` (gitignored)
4. Share your spreadsheet with the service account email (`...@...iam.gserviceaccount.com`) as **Editor**
5. In `docker-compose.yml`, uncomment the `GOOGLE_SHEET_*` env vars and the volume mount

| Variable | Default | Description |
|---|---|---|
| `GOOGLE_SHEET_ENABLED` | `false` | Master switch |
| `GOOGLE_SHEET_KEY_FILE` | — | Path to the service account JSON (inside the container) |
| `GOOGLE_SHEET_ID` | — | Spreadsheet ID from the sheet URL |
| `GOOGLE_SHEET_TAB` | `Sheet1` | Tab to append to |
| `GOOGLE_SHEET_TIMEOUT_SEC` | `15` | Per-request timeout |
| `GOOGLE_SHEET_MAX_RETRIES` | `3` | Retries (4 attempts, backoff 1s/2s/4s) |

Sheet writes are **best-effort with retries**, deduped by matching the row's own
values (date/type/amount/comments), so re-submitting the same recording never duplicates a row. A Google
outage never fails the transcription job. Rows are appended only when extraction
succeeds. Uses the Sheets API v4 directly (service-account JWT, stdlib-only).

Choose the target tab per job with the optional `sheet` form field (multipart):

```bash
curl -X POST http://localhost:9090/jobs \
  -F "file=@recording.m4a" \
  -F "sheet=August, 2026"          # must be a tab from GET /sheets
```

- No `sheet` field → rows go to `GOOGLE_SHEET_TAB`
- Unknown tab → `400 {"error":"unknown sheet tab: ..."}`
- The job response includes the chosen tab: `"sheet": "August, 2026"`

## Expose to the internet

See [`docs/expose-internet.md`](docs/expose-internet.md) for Tailscale Funnel setup.
One command to get a public HTTPS URL.

## Supported audio formats

wav, mp3, ogg, opus, m4a, flac — validated by magic bytes, not extension.
Configurable via `ALLOWED_FORMATS` env var.
