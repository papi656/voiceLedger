package transcription

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"time"
)

// Job status constants.
const (
	JobQueued     = "queued"
	JobProcessing = "processing"
	JobDone       = "done"
	JobFailed     = "failed"
)

// Job represents a transcription request flowing through the system.
type Job struct {
	ID          string          `json:"job_id"`
	Status      string          `json:"status"`
	KeyID       string          `json:"-"`
	AccessToken string          `json:"-"`
	Filename    string          `json:"filename"`
	WAVData     []byte          `json:"-"`
	Result      json.RawMessage `json:"result"`
	Error       string          `json:"error"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// GenerateJobID produces a cryptographically random 16-character hex job ID.
func GenerateJobID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		log.Printf("WARNING: crypto/rand failed, falling back to timestamp: %v", err)
		return hex.EncodeToString([]byte(time.Now().String()))[:16]
	}
	return hex.EncodeToString(b)
}
