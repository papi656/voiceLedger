package audio

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"gateway/internal/httputil"
)

// Converter transcodes audio files to 16kHz mono WAV via ffmpeg.
type Converter struct {
	ffmpegPath string
	timeout    time.Duration
}

// NewConverter creates an audio converter with the given ffmpeg binary path and timeout.
func NewConverter(ffmpegPath string, timeout time.Duration) *Converter {
	return &Converter{
		ffmpegPath: ffmpegPath,
		timeout:    timeout,
	}
}

// Convert transcodes the given file data to 16kHz mono WAV.
func (c *Converter) Convert(filename string, fileData []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	tmpFile, err := os.CreateTemp("", "upload-*"+filepath.Ext(filename))
	if err != nil {
		return nil, &httputil.HTTPError{http.StatusInternalServerError, "internal server error"}
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if _, err := tmpFile.Write(fileData); err != nil {
		return nil, &httputil.HTTPError{http.StatusInternalServerError, "internal server error"}
	}
	tmpFile.Close()

	cmd := exec.CommandContext(ctx, c.ffmpegPath,
		"-i", tmpFile.Name(),
		"-ar", "16000",
		"-ac", "1",
		"-f", "wav",
		"pipe:1",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, &httputil.HTTPError{http.StatusGatewayTimeout, "audio conversion timed out"}
	}
	if err != nil {
		return nil, &httputil.HTTPError{http.StatusUnprocessableEntity, fmt.Sprintf("audio conversion failed: %s", stderr.String())}
	}
	if stdout.Len() == 0 {
		return nil, &httputil.HTTPError{http.StatusUnprocessableEntity, "audio conversion produced no output"}
	}

	return stdout.Bytes(), nil
}
