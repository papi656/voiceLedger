# Auth Plan — Google OAuth (ID Token + Access Token)

> Target agent: implement these changes in order. Each step builds on the previous. Verify after each step before moving on.

## Architecture

```
Client signs in with Google
    │
    │  Google returns: ID token + access token
    │
    ▼
POST /jobs
Authorization: Bearer <id-token>
X-Sheets-Token: <access-token>
    │
    ▼
Gateway:
  1. Verify ID token against Google JWKS (local crypto, cached)
  2. Extract sub (user ID), email, name
  3. Rate limit per sub
  4. Store access token with job (for writing results to Sheets)
  5. All subsequent requests authenticated same way
```

| Component | Fate |
|---|---|
| `keys.txt` / `keys.example.txt` | ❌ Removed |
| `KeysFile` config, `APIKeys` slice | ❌ Removed |
| `loadKeys` function | ❌ Removed |
| `idtoken.Validate()` (Google library) | ✅ Added |
| Access token storage in Job | ✅ Added |
| Rate limiting per Google `sub` | ✅ Updated |

Zero keys to manage. Zero files to rotate. User identity comes from Google.

---

## Dependencies to Add

```bash
cd gateway && go get golang.org/x/oauth2@latest google.golang.org/api@latest && go mod tidy
```

- `google.golang.org/api/idtoken` — validates Google ID tokens (JWKS fetch, cache, signature verify, expiry check, issuer check)
- `golang.org/x/oauth2` — pulled in as transitive dep, used later for Sheets API

---

## Files to Modify (in order)

| # | File | What |
|---|---|---|
| 1 | `gateway/go.mod` | Add dependencies |
| 2 | `gateway/config.go` | Remove key fields + func, add OAuthAudience |
| 3 | `gateway/auth.go` | Complete rewrite to Google OAuth |
| 4 | `gateway/middleware.go` | Update authMiddleware calls |
| 5 | `gateway/ratelimit.go` | Change keyID extraction from string to UserInfo |
| 6 | `gateway/jobhandler.go` | Extract X-Sheets-Token, update keyID extraction |
| 7 | `gateway/queue.go` | Add AccessToken to Job struct |
| 8 | `docker-compose.yml` | Remove KEYS_FILE env + keys volume |
| 9 | `keys.example.txt` | Delete |
| 10 | `keys.txt` | Delete |
| 11 | `.gitignore` | Remove keys.txt line |

---

## Step 1: Add dependencies

```bash
cd gateway
go get golang.org/x/oauth2@latest google.golang.org/api@latest
go mod tidy
```

**Verify:**
```bash
go build ./...
```
Expect: compile errors from old `cfg.APIKeys` references. That's fine — we fix them next.

---

## Step 2: Update `gateway/config.go`

**File:** `gateway/config.go`

### Edit 2a: Remove `KeysFile` and `APIKeys` from Config struct

Delete these two lines:
```go
	KeysFile              string
	APIKeys               []string
```

### Edit 2b: Remove `KeysFile` init from the struct literal in `LoadConfig()`

Delete this line:
```go
		KeysFile:              envStr("KEYS_FILE", "keys.txt"),
```

### Edit 2c: Remove the keys-loading block from `LoadConfig()`

Delete these lines:
```go
	cfg.APIKeys = loadKeys(cfg.KeysFile)
	if len(cfg.APIKeys) == 0 {
		log.Println("WARNING: no API keys loaded — auth is disabled")
	} else {
		log.Printf("loaded %d API key(s) from %s", len(cfg.APIKeys), cfg.KeysFile)
	}
```

### Edit 2d: Delete the `loadKeys` function

Delete the entire `loadKeys` function (starts with `func loadKeys(path string) []string {`).

### Edit 2e: Remove unused `"bufio"` import

Remove `"bufio"` from the import block. (`"os"` stays — used by `envStr`.)

### Edit 2f: Add `OAuthAudience` field

In the `Config` struct, add:
```go
	OAuthAudience string
```

In the `LoadConfig()` struct literal, add:
```go
		OAuthAudience:         envStr("OAUTH_AUDIENCE", ""),
```

`OAUTH_AUDIENCE` = your Google OAuth client ID. Empty = dev mode (validator accepts any audience, invalid tokens get a warning but pass through).

### Edit 2g: Also remove `FFMPEGPath` — it's unused

While here: the `Converter` struct has `ffmpegPath` but `LoadConfig` reads it... actually, it IS used in `NewConverter`. Keep it.

**Verify:**
```bash
cd gateway && go build ./...
```
Expect: errors in auth.go and middleware.go (references to removed `cfg.APIKeys`). Proceed to step 3.

---

## Step 3: Rewrite `gateway/auth.go`

**File:** `gateway/auth.go`

Replace entire contents. This is a full rewrite — the file goes from Bearer token + `cfg.APIKeys` to Google ID token verification.

```go
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"google.golang.org/api/idtoken"
)

type contextKey string

const keyIDKey contextKey = "key_id"

// UserInfo holds Google user data extracted from the verified ID token.
type UserInfo struct {
	Sub     string // Google user ID (unique, stable) — used for rate limiting
	Email   string
	Name    string
	Picture string
}

func authMiddleware(audience string) func(http.Handler) http.Handler {
	devMode := audience == ""

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")

			if header == "" {
				writeAuthError(w, "missing Authorization header")
				return
			}

			if len(header) < 8 || header[:7] != "Bearer " {
				writeAuthError(w, "invalid Authorization format, expected Bearer")
				return
			}

			idToken := header[7:]
			if idToken == "" {
				writeAuthError(w, "empty token")
				return
			}

			payload, err := idtoken.Validate(r.Context(), idToken, audience)
			if err != nil {
				if devMode {
					log.Printf("auth: dev mode, allowing invalid token (error: %v)", err)
					ctx := context.WithValue(r.Context(), keyIDKey, UserInfo{
						Sub:   "dev-user",
						Email: "dev@localhost",
						Name:  "Dev User",
					})
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				log.Printf("auth: rejected (error: %v)", err)
				writeAuthError(w, "unauthorized")
				return
			}

			user := UserInfo{
				Sub:   payload.Subject,
				Email: "",
				Name:  "",
			}

			if email, ok := payload.Claims["email"].(string); ok {
				user.Email = email
			}
			if name, ok := payload.Claims["name"].(string); ok {
				user.Name = name
			}
			if picture, ok := payload.Claims["picture"].(string); ok {
				user.Picture = picture
			}

			log.Printf("auth: user=%s email=%s", user.Sub, user.Email)

			ctx := context.WithValue(r.Context(), keyIDKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeAuthError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
```

Key behaviors:
- `audience == ""` → dev mode: token validation failures log a warning, request proceeds with fake `dev-user` identity
- `audience == "your-client-id.apps.googleusercontent.com"` → production: invalid = 401, no fallback
- Context value is now a `UserInfo` struct (was `string`)
- Audit log on every successful auth

**Verify:**
```bash
cd gateway && go build ./...
```
Expect: errors in middleware.go (authMiddleware sig changed) and ratelimit.go + jobhandler.go (keyID type changed). Proceed to step 4.

---

## Step 4: Update `gateway/middleware.go`

**File:** `gateway/middleware.go`

### Edit 4a: Replace `authMiddleware(cfg)` with `authMiddleware(cfg.OAuthAudience)`

There are exactly 3 calls. Replace each one.

Find:
```go
	jobHandler = authMiddleware(cfg)(jobHandler)
```
Replace with:
```go
	jobHandler = authMiddleware(cfg.OAuthAudience)(jobHandler)
```

Find:
```go
	statusHandler = authMiddleware(cfg)(statusHandler)
```
Replace with:
```go
	statusHandler = authMiddleware(cfg.OAuthAudience)(statusHandler)
```

Find:
```go
	mux.Handle("/", rateLimitMiddleware(limiter)(authMiddleware(cfg)(catchAll)))
```
Replace with:
```go
	mux.Handle("/", rateLimitMiddleware(limiter)(authMiddleware(cfg.OAuthAudience)(catchAll)))
```

The `buildHandler` function signature stays the same — it already takes `cfg *Config`.

**Verify:**
```bash
cd gateway && go build ./...
```
Expect: errors only in ratelimit.go and jobhandler.go. Proceed.

---

## Step 5: Update `gateway/ratelimit.go`

**File:** `gateway/ratelimit.go`

### Edit 5a: Change keyID extraction from `string` to `UserInfo.Sub`

Find:
```go
		keyID := "anonymous"
		if v := r.Context().Value(keyIDKey); v != nil {
			keyID = v.(string)
		}
```

Replace with:
```go
		keyID := "anonymous"
		if v := r.Context().Value(keyIDKey); v != nil {
			if user, ok := v.(UserInfo); ok {
				keyID = user.Sub
			}
		}
```

**Verify:**
```bash
cd gateway && go build ./...
```
Expect: error only in jobhandler.go. Proceed.

---

## Step 6: Update `gateway/jobhandler.go`

**File:** `gateway/jobhandler.go`

### Edit 6a: Update keyID extraction (same pattern as ratelimit.go)

Find:
```go
		keyID := "anonymous"
		if v := r.Context().Value(keyIDKey); v != nil {
			keyID = v.(string)
		}
```

Replace with:
```go
		keyID := "anonymous"
		if v := r.Context().Value(keyIDKey); v != nil {
			if user, ok := v.(UserInfo); ok {
				keyID = user.Sub
			}
		}

		accessToken := r.Header.Get("X-Sheets-Token")
```

### Edit 6b: Add `AccessToken` to the Job struct literal

Find:
```go
		job := &Job{
			ID:        generateJobID(),
			Status:    JobQueued,
			KeyID:     keyID,
			Filename:  fh.Filename,
			WAVData:   wavData,
			CreatedAt: now,
			UpdatedAt: now,
		}
```

Replace with:
```go
		job := &Job{
			ID:          generateJobID(),
			Status:      JobQueued,
			KeyID:       keyID,
			Filename:    fh.Filename,
			WAVData:     wavData,
			AccessToken: accessToken,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
```

---

## Step 7: Update `gateway/queue.go`

**File:** `gateway/queue.go`

### Edit 7a: Add `AccessToken` field to Job struct

Add this line inside the `Job` struct, near `KeyID`:
```go
	AccessToken string          `json:"-"`
```

`json:"-"` because we never send the access token back to the client.

**Verify:**
```bash
cd gateway && go build ./...
```
Expect: clean build, zero errors. This is the first full green build.

---

## Step 8: Update `docker-compose.yml`

**File:** `docker-compose.yml`

### Edit 8a: Remove KEYS_FILE env var

Delete this line:
```yaml
      - KEYS_FILE=/run/secrets/keys.txt
```

### Edit 8b: Remove keys volume mount

Delete these lines:
```yaml
    volumes:
      - ./keys.txt:/run/secrets/keys.txt:ro
```

(Just the keys volume block — no other volumes existed.)

### Edit 8c: Add OAUTH_AUDIENCE env var (commented out)

Add this line to the `environment:` block:
```yaml
      # - OAUTH_AUDIENCE=your-client-id.apps.googleusercontent.com  # uncomment in production
```

---

## Step 9: Delete `keys.example.txt`

```bash
rm keys.example.txt
```

---

## Step 10: Delete `keys.txt`

```bash
rm -f keys.txt
```

(It's gitignored, won't show in git status.)

---

## Step 11: Update `.gitignore`

**File:** `.gitignore`

### Edit 11a: Remove the `keys.txt` line

Delete:
```
keys.txt
```

The secrets section should be empty after this (or remove the `# secrets` comment header too).

**Note:** `.gitignore` also has `*.md` which blocks `authPlan.md` and `PLAN.md` from being tracked. If you want these tracked, remove that line as well. Not part of this auth change, but worth fixing separately.

---

## Verification

### Build check
```bash
cd gateway && go build ./...
```
Expected: clean build, zero errors.

### Run in dev mode
```bash
cd gateway
OAUTH_AUDIENCE="" go run . &
sleep 1

# No auth header → 401
curl -s -o /dev/null -w "%{http_code}" http://localhost:9090/jobs/nonexistent
# → 401

# Any token in dev mode → passes
curl -s http://localhost:9090/jobs/nonexistent \
  -H "Authorization: Bearer fake-token-anything"
# → 404 {"error":"job not found"}   (auth passed, just no job)

# Health bypasses auth
curl -s http://localhost:9090/health
# → {"status":"ok","service":"whisper-gateway","version":"1.0.0"}

kill %1
```

### Run in production mode (requires gcloud / real Google token)
```bash
export OAUTH_AUDIENCE="YOUR_CLIENT_ID.apps.googleusercontent.com"

cd gateway && go run . &
sleep 1

# Valid Google token → passes
curl -s http://localhost:9090/jobs/nonexistent \
  -H "Authorization: Bearer $(gcloud auth print-identity-token)"
# → 404 {"error":"job not found"}

# Invalid token → 401
curl -s -o /dev/null -w "%{http_code}" http://localhost:9090/jobs/nonexistent \
  -H "Authorization: Bearer garbage"
# → 401

# Job submission with both tokens
curl -s -X POST http://localhost:9090/jobs \
  -H "Authorization: Bearer $(gcloud auth print-identity-token)" \
  -H "X-Sheets-Token: ya29.fake-access-token" \
  -F "file=@test-audio.wav"
# → 202 {"job_id":"abc...","status":"queued"}

kill %1
```

---

## Summary: What changes

| File | Change |
|---|---|
| `gateway/go.mod` | Add `golang.org/x/oauth2`, `google.golang.org/api` |
| `gateway/config.go` | Remove `KeysFile`, `APIKeys`, `loadKeys`; add `OAuthAudience` |
| `gateway/auth.go` | Full rewrite: `idtoken.Validate()`, dev mode support, `UserInfo` struct |
| `gateway/middleware.go` | `authMiddleware(cfg)` → `authMiddleware(cfg.OAuthAudience)` (3 places) |
| `gateway/ratelimit.go` | Context value type `string` → `UserInfo.Sub` |
| `gateway/jobhandler.go` | Context value type change + extract `X-Sheets-Token` header |
| `gateway/queue.go` | Add `AccessToken string` to Job struct |
| `docker-compose.yml` | Remove `KEYS_FILE` env, remove keys volume mount |
| `keys.example.txt` | Delete |
| `keys.txt` | Delete |
| `.gitignore` | Remove `keys.txt` line |

## Untouched files

`main.go`, `proxy.go`, `converter.go`, `filevalidator.go`, `health.go` — zero changes needed.

## What the client needs to send

```
POST /jobs
Authorization: Bearer <google-id-token>
X-Sheets-Token: <google-access-token>
Content-Type: multipart/form-data
  file: <audio-file>

GET /jobs/{id}
Authorization: Bearer <google-id-token>
```

The client obtains both tokens from Google OAuth with scopes:
- `openid email profile` → ID token
- `https://www.googleapis.com/auth/spreadsheets` → access token (for Sheets writes)