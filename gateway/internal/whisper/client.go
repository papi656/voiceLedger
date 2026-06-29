package whisper

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// Client communicates with the external whisper transcription service.
type Client struct {
	targetURL string
	client    *http.Client
}

// NewClient creates a whisper client pointed at the given host and port.
func NewClient(host, port string, timeout time.Duration) *Client {
	return &Client{
		targetURL: fmt.Sprintf("http://%s:%s/inference", host, port),
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// Transcribe sends WAV data to the whisper service and returns the transcription result.
func (c *Client) Transcribe(ctx context.Context, filename string, wavData []byte) ([]byte, error) {
	body, contentType, err := buildMultipartBody(filename, wavData)
	if err != nil {
		return nil, fmt.Errorf("building multipart body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating whisper request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("whisper request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading whisper response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("whisper returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

func buildMultipartBody(filename string, wavData []byte) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	ext := strings.ToLower(filepath.Ext(filename))
	baseName := strings.TrimSuffix(filename, ext)
	if baseName == "" {
		baseName = "audio"
	}

	part, err := w.CreateFormFile("file", baseName+".wav")
	if err != nil {
		return nil, "", fmt.Errorf("creating form file: %w", err)
	}
	if _, err := part.Write(wavData); err != nil {
		return nil, "", fmt.Errorf("writing wav data: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, "", fmt.Errorf("closing multipart writer: %w", err)
	}

	return buf.Bytes(), w.FormDataContentType(), nil
}
