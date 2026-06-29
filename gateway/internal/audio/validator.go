package audio

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"gateway/internal/httputil"
)

// AudioFormat describes a detectable audio format by MIME type and magic bytes.
type AudioFormat struct {
	MIMEType    string
	MagicBytes  []byte
	MagicOffset int
}

// SupportedFormats maps format names to their detection signatures.
var SupportedFormats = map[string]AudioFormat{
	"wav":  {MIMEType: "audio/wav", MagicBytes: []byte("RIFF"), MagicOffset: 0},
	"mp3":  {MIMEType: "audio/mpeg", MagicBytes: []byte{0xFF, 0xFB}, MagicOffset: 0},
	"ogg":  {MIMEType: "audio/ogg", MagicBytes: []byte("OggS"), MagicOffset: 0},
	"opus": {MIMEType: "audio/ogg", MagicBytes: []byte("OggS"), MagicOffset: 0},
	"m4a":  {MIMEType: "audio/mp4", MagicBytes: []byte("ftyp"), MagicOffset: 4},
	"flac": {MIMEType: "audio/flac", MagicBytes: []byte("fLaC"), MagicOffset: 0},
}

// DetectFormat inspects the raw bytes and returns the detected audio format name.
func DetectFormat(data []byte) (string, bool) {
	for name, fmt := range SupportedFormats {
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

// IsFormatAllowed returns true if the named format is in the allow list.
func IsFormatAllowed(format string, allowed []string) bool {
	for _, a := range allowed {
		if a == format {
			return true
		}
	}
	return false
}

// ValidateAndParseFile reads and validates an uploaded audio file from the request.
// It enforces size limits, checks for path traversal, and validates the audio format.
func ValidateAndParseFile(r *http.Request, maxBodySizeMB, maxFileSizeMB int, allowedFormats []string) (*multipart.FileHeader, []byte, error) {
	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "multipart/form-data") {
		return nil, nil, &httputil.HTTPError{http.StatusBadRequest, "Content-Type must be multipart/form-data"}
	}

	r.Body = http.MaxBytesReader(nil, r.Body, int64(maxBodySizeMB)*1024*1024)

	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, r.Body); err != nil {
		if strings.Contains(err.Error(), "http: request body too large") {
			return nil, nil, &httputil.HTTPError{http.StatusRequestEntityTooLarge, fmt.Sprintf("request body exceeds maximum size of %d MB", maxBodySizeMB)}
		}
		return nil, nil, &httputil.HTTPError{http.StatusBadRequest, "failed to read request body"}
	}

	reader := multipart.NewReader(bytes.NewReader(buf.Bytes()), extractBoundary(ct))
	form, err := reader.ReadForm(int64(maxFileSizeMB) * 1024 * 1024)
	if err != nil {
		return nil, nil, &httputil.HTTPError{http.StatusBadRequest, "failed to parse multipart form"}
	}

	fileHeaders, ok := form.File["file"]
	if !ok || len(fileHeaders) == 0 {
		form.RemoveAll()
		return nil, nil, &httputil.HTTPError{http.StatusBadRequest, "no 'file' field in request"}
	}

	fh := fileHeaders[0]

	if isPathTraversal(fh.Filename) {
		form.RemoveAll()
		return nil, nil, &httputil.HTTPError{http.StatusBadRequest, "invalid filename"}
	}

	if fh.Size > int64(maxFileSizeMB)*1024*1024 {
		form.RemoveAll()
		return nil, nil, &httputil.HTTPError{http.StatusRequestEntityTooLarge, fmt.Sprintf("file exceeds maximum size of %d MB", maxFileSizeMB)}
	}

	f, err := fh.Open()
	if err != nil {
		form.RemoveAll()
		return nil, nil, &httputil.HTTPError{http.StatusBadRequest, "failed to open uploaded file"}
	}

	fileData := make([]byte, fh.Size)
	if _, err := io.ReadFull(f, fileData); err != nil {
		f.Close()
		form.RemoveAll()
		return nil, nil, &httputil.HTTPError{http.StatusBadRequest, "failed to read file data"}
	}
	f.Close()

	format, ok := DetectFormat(fileData)
	if !ok || !IsFormatAllowed(format, allowedFormats) {
		form.RemoveAll()
		return nil, nil, &httputil.HTTPError{http.StatusUnsupportedMediaType, fmt.Sprintf("unsupported audio format, allowed: %s", strings.Join(allowedFormats, ", "))}
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

func isPathTraversal(name string) bool {
	return strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..")
}
