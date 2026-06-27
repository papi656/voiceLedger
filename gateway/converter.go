package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Converter struct {
	ffmpegPath string
	timeout    time.Duration
}

func NewConverter(cfg *Config) *Converter {
	return &Converter{
		ffmpegPath: cfg.FFMPEGPath,
		timeout:    time.Duration(cfg.ConvertTimeoutSec) * time.Second,
	}
}

func (c *Converter) Convert(filename string, fileData []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	tmpFile, err := os.CreateTemp("", "upload-*"+filepath.Ext(filename))
	if err != nil {
		return nil, &httpError{http.StatusInternalServerError, "internal server error"}
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if _, err := tmpFile.Write(fileData); err != nil {
		return nil, &httpError{http.StatusInternalServerError, "internal server error"}
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
		return nil, &httpError{http.StatusGatewayTimeout, "audio conversion timed out"}
	}
	if err != nil {
		return nil, &httpError{http.StatusUnprocessableEntity, "audio conversion failed, file may be corrupt"}
	}
	if stdout.Len() == 0 {
		return nil, &httpError{http.StatusUnprocessableEntity, "audio conversion produced no output"}
	}

	return stdout.Bytes(), nil
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

func replaceRequestBody(r *http.Request, body []byte, contentType string) {
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.Header.Set("Content-Type", contentType)
	r.ContentLength = int64(len(body))
}
