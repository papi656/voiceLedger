package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

type AudioFormat struct {
	MIMEType    string
	MagicBytes  []byte
	MagicOffset int
}

var supportedFormats = map[string]AudioFormat{
	"wav":  {MIMEType: "audio/wav", MagicBytes: []byte("RIFF"), MagicOffset: 0},
	"mp3":  {MIMEType: "audio/mpeg", MagicBytes: []byte{0xFF, 0xFB}, MagicOffset: 0},
	"ogg":  {MIMEType: "audio/ogg", MagicBytes: []byte("OggS"), MagicOffset: 0},
	"opus": {MIMEType: "audio/ogg", MagicBytes: []byte("OggS"), MagicOffset: 0},
	"m4a":  {MIMEType: "audio/mp4", MagicBytes: []byte("ftyp"), MagicOffset: 4},
	"flac": {MIMEType: "audio/flac", MagicBytes: []byte("fLaC"), MagicOffset: 0},
}

func detectFormat(data []byte) (string, bool) {
	for name, fmt := range supportedFormats {
		offset := fmt.MagicOffset
		if offset+len(fmt.MagicBytes) > len(data) {
			continue
		}
		if bytes.Equal(data[offset:offset+len(fmt.MagicBytes)], fmt.MagicBytes) {
			if name == "wav" {
				if len(data) >= 12 && string(data[8:12]) == "WAVE" {
					return name, true
				}
				continue
			}
			if name == "mp3" && len(data) >= 2 {
				b0, b1 := data[0], data[1]
				if b0 == 0xFF && (b1&0xE0) == 0xE0 {
					return name, true
				}
				continue
			}
			return name, true
		}
	}
	return "", false
}

func isFormatAllowed(format string, allowed []string) bool {
	for _, a := range allowed {
		if a == format {
			return true
		}
	}
	return false
}

func isPathTraversal(name string) bool {
	return strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..")
}

func validateAndParseFile(r *http.Request, cfg *Config) (*multipart.FileHeader, []byte, error) {
	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "multipart/form-data") {
		return nil, nil, &httpError{http.StatusBadRequest, "Content-Type must be multipart/form-data"}
	}

	r.Body = http.MaxBytesReader(nil, r.Body, int64(cfg.MaxBodySizeMB)*1024*1024)

	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, r.Body); err != nil {
		if strings.Contains(err.Error(), "http: request body too large") {
			return nil, nil, &httpError{http.StatusRequestEntityTooLarge, fmt.Sprintf("request body exceeds maximum size of %d MB", cfg.MaxBodySizeMB)}
		}
		return nil, nil, &httpError{http.StatusBadRequest, "failed to read request body"}
	}

	reader := multipart.NewReader(bytes.NewReader(buf.Bytes()), extractBoundary(ct))
	form, err := reader.ReadForm(int64(cfg.MaxFileSizeMB) * 1024 * 1024)
	if err != nil {
		return nil, nil, &httpError{http.StatusBadRequest, "failed to parse multipart form"}
	}

	fileHeaders, ok := form.File["file"]
	if !ok || len(fileHeaders) == 0 {
		form.RemoveAll()
		return nil, nil, &httpError{http.StatusBadRequest, "no 'file' field in request"}
	}

	fh := fileHeaders[0]

	if isPathTraversal(fh.Filename) {
		form.RemoveAll()
		return nil, nil, &httpError{http.StatusBadRequest, "invalid filename"}
	}

	if fh.Size > int64(cfg.MaxFileSizeMB)*1024*1024 {
		form.RemoveAll()
		return nil, nil, &httpError{http.StatusRequestEntityTooLarge, fmt.Sprintf("file exceeds maximum size of %d MB", cfg.MaxFileSizeMB)}
	}

	f, err := fh.Open()
	if err != nil {
		form.RemoveAll()
		return nil, nil, &httpError{http.StatusBadRequest, "failed to open uploaded file"}
	}

	fileData := make([]byte, fh.Size)
	if _, err := io.ReadFull(f, fileData); err != nil {
		f.Close()
		form.RemoveAll()
		return nil, nil, &httpError{http.StatusBadRequest, "failed to read file data"}
	}
	f.Close()

	format, ok := detectFormat(fileData)
	if !ok || !isFormatAllowed(format, cfg.AllowedFormats) {
		form.RemoveAll()
		return nil, nil, &httpError{http.StatusUnsupportedMediaType, fmt.Sprintf("unsupported audio format, allowed: %s", strings.Join(cfg.AllowedFormats, ", "))}
	}

	form.RemoveAll()

	return fh, fileData, nil
}

func extractBoundary(ct string) string {
	for _, part := range strings.Split(ct, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "boundary=") {
			return strings.Trim(part[9:], `"`)
		}
	}
	return ""
}

type httpError struct {
	Code    int
	Message string
}

func (e *httpError) Error() string {
	return e.Message
}

func writeHTTPError(w http.ResponseWriter, err *httpError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(err.Code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Message})
}
