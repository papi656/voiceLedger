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
│ • auth      │     │ • ggml-medium-q8  │
│ • rate limit│     │ • 16kHz mono WAV  │
│ • ffmpeg    │     │ • JSON response   │
│ • job queue │     │                   │
└─────────────┘     └──────────────────┘
```

## Quick start

```bash
# First time only — compiles whisper-server for Linux (~5 min)
./scripts/build-whisper-linux.sh

# Start both services
docker compose up -d

# Verify
curl http://localhost:9090/health
# → {"service":"whisper-gateway","status":"ok","version":"1.0.0"}

# Transcribe an audio file
curl -X POST http://localhost:9090/jobs \
  -H "Authorization: Bearer anything" \
  -F "file=@test-fixtures/sample.m4a"
# → {"job_id":"abc123...","status":"queued"}

# Poll for result
curl http://localhost:9090/jobs/abc123... \
  -H "Authorization: Bearer anything"
# → {"job_id":"abc123...","status":"done","result":{...}}
```

## API

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/health` | None | Health check |
| `POST` | `/jobs` | `Bearer <token>` | Submit audio (multipart/form-data, `file` field) |
| `GET` | `/jobs/{id}` | `Bearer <token>` | Poll job status and result |

**Auth modes**: set `OAUTH_AUDIENCE` (Google client ID) for production OAuth, or
leave empty for dev mode (any token accepted).

**Rate limits** (defaults): 60 req/min per user, 30 req/min per IP. Headers:
`X-RateLimit-Remaining-Key`, `X-RateLimit-Remaining-IP`, `Retry-After`.

## Project structure

```
.
├── gateway/                    # Go API gateway (cmd/ + internal/ layout)
│   ├── cmd/gateway/main.go     # Entry point — wiring, signals, shutdown
│   ├── internal/
│   │   ├── config/             # Environment-based configuration
│   │   ├── server/             # Route registration, CORS, body limits
│   │   ├── auth/               # Google OAuth middleware
│   │   ├── ratelimit/          # Per-key + per-IP token bucket
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
├── scripts/
│   └── build-whisper-linux.sh  # One-time: compile whisper for Linux in Docker
│
├── test-fixtures/              # Sample audio files for testing
│   ├── sample.m4a
│   └── test-audio.wav
│
├── docs/
│   ├── whisper-build-explanation.md   # How the build system works
│   └── expose-internet.md             # Public internet via Tailscale Funnel
│
└── docker-compose.yml          # Orchestrates gateway + whisper
```

## Expose to the internet

See [`docs/expose-internet.md`](docs/expose-internet.md) for Tailscale Funnel setup.
One command to get a public HTTPS URL.

## Supported audio formats

wav, mp3, ogg, opus, m4a, flac — validated by magic bytes, not extension.
Configurable via `ALLOWED_FORMATS` env var.
