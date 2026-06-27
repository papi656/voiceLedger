package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

type WhisperClient struct {
	targetURL string
	client    *http.Client
}

func NewWhisperClient(cfg *Config) *WhisperClient {
	return &WhisperClient{
		targetURL: fmt.Sprintf("http://%s:%s/inference", cfg.WhisperHost, cfg.WhisperPort),
		client: &http.Client{
			Timeout: time.Duration(cfg.WhisperTimeoutSec) * time.Second,
		},
	}
}

func (c *WhisperClient) Transcribe(ctx context.Context, filename string, wavData []byte) ([]byte, error) {
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
