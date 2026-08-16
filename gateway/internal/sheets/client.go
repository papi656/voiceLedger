package sheets

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const baseURL = "https://sheets.googleapis.com/v4/spreadsheets"

// Client appends transcription+extraction rows to a Google Spreadsheet using
// the Sheets API v4 (spreadsheets.values.append) with a service account.
type Client struct {
	provider *tokenProvider
	http     *http.Client
	sheetID  string
	tab      string
}

// NewClient creates a Sheets client. keyPath is a Google service account JSON
// key file; the sheet must be shared with the service account email as Editor.
func NewClient(keyPath, sheetID, tab string, timeout time.Duration) (*Client, error) {
	if keyPath == "" {
		return nil, fmt.Errorf("service account key path is empty")
	}
	if sheetID == "" {
		return nil, fmt.Errorf("spreadsheet id is empty")
	}
	if tab == "" {
		tab = "Sheet1"
	}
	hc := &http.Client{Timeout: timeout}
	p, err := newTokenProvider(keyPath, hc)
	if err != nil {
		return nil, err
	}
	return &Client{provider: p, http: hc, sheetID: sheetID, tab: tab}, nil
}

// AppendRowWithRetry writes one row to the sheet, deduping by the first column
// (job_id) and retrying up to maxRetries additional times with exponential
// backoff (1s, 2s, 4s, ...) on any failure.
func (c *Client) AppendRowWithRetry(ctx context.Context, row []string, maxRetries int) error {
	total := maxRetries + 1
	var lastErr error
	for attempt := 1; attempt <= total; attempt++ {
		exists, err := c.RowExists(ctx, row[0])
		if err != nil {
			lastErr = err
		} else if exists {
			log.Printf("sheets: row for job %s already present, skipping append", row[0])
			return nil
		} else if err := c.AppendRow(ctx, row); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if attempt < total {
			delay := time.Duration(1<<(attempt-1)) * time.Second
			log.Printf("sheets append attempt %d/%d failed: %v; retrying in %s", attempt, total, lastErr, delay)
			select {
			case <-ctx.Done():
				return fmt.Errorf("sheets append canceled during retry wait: %w", ctx.Err())
			case <-time.After(delay):
			}
		}
	}
	return fmt.Errorf("sheets append failed after %d attempts: %w", total, lastErr)
}

// RowExists reports whether jobID appears in the first column of the sheet.
func (c *Client) RowExists(ctx context.Context, jobID string) (bool, error) {
	tok, err := c.provider.token(ctx)
	if err != nil {
		return false, err
	}
	u := fmt.Sprintf("%s/%s/values/%s", baseURL, url.PathEscape(c.sheetID), url.PathEscape(c.tab+"!A1:A"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, fmt.Errorf("building dedupe request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := c.http.Do(req)
	if err != nil {
		return false, fmt.Errorf("dedupe lookup: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("reading dedupe response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("dedupe lookup returned %d: %s", resp.StatusCode, truncate(body))
	}

	var vr struct {
		Values [][]string `json:"values"`
	}
	if err := json.Unmarshal(body, &vr); err != nil {
		return false, fmt.Errorf("parsing dedupe response: %w", err)
	}
	for _, r := range vr.Values {
		if len(r) > 0 && r[0] == jobID {
			return true, nil
		}
	}
	return false, nil
}

// AppendRow appends a single row to the sheet (spreadsheets.values.append).
// The range points at A1; the API finds the logical table and appends after
// its last row, so headers are preserved.
func (c *Client) AppendRow(ctx context.Context, row []string) error {
	tok, err := c.provider.token(ctx)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{"values": [][]string{sanitizeRow(row)}})
	if err != nil {
		return fmt.Errorf("marshaling append body: %w", err)
	}
	u := fmt.Sprintf("%s/%s/values/%s:append?valueInputOption=RAW&insertDataOption=INSERT_ROWS",
		baseURL, url.PathEscape(c.sheetID), url.PathEscape(c.tab+"!A1"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building append request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("append: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading append response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("append returned %d: %s", resp.StatusCode, truncate(body))
	}
	return nil
}

// sanitizeRow prefixes any cell starting with "=" so a transcription can't be
// interpreted as a formula when the row is written.
func sanitizeRow(row []string) []string {
	out := make([]string, len(row))
	for i, v := range row {
		if strings.HasPrefix(v, "=") {
			v = "'" + v
		}
		out[i] = v
	}
	return out
}
