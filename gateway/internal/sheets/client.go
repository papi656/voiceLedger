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
	"sync"
	"time"
)

const baseURL = "https://sheets.googleapis.com/v4/spreadsheets"

// tabsCacheTTL is how long the tab list is cached before re-fetching metadata.
const tabsCacheTTL = 5 * time.Minute

// Client appends transcription+extraction rows to a Google Spreadsheet using
// the Sheets API v4 (spreadsheets.values.append) with a service account.
type Client struct {
	provider   *tokenProvider
	http       *http.Client
	sheetID    string
	DefaultTab string // tab used when a job does not specify one

	tabsMu sync.Mutex
	tabs   []string
	tabsAt time.Time
}

// NewClient creates a Sheets client. keyPath is a Google service account JSON
// key file; the sheet must be shared with the service account email as Editor.
func NewClient(keyPath, sheetID, defaultTab string, timeout time.Duration) (*Client, error) {
	if keyPath == "" {
		return nil, fmt.Errorf("service account key path is empty")
	}
	if sheetID == "" {
		return nil, fmt.Errorf("spreadsheet id is empty")
	}
	if defaultTab == "" {
		defaultTab = "Sheet1"
	}
	hc := &http.Client{Timeout: timeout}
	p, err := newTokenProvider(keyPath, hc)
	if err != nil {
		return nil, err
	}
	return &Client{provider: p, http: hc, sheetID: sheetID, DefaultTab: defaultTab}, nil
}

// Tabs returns the tab (sheet) names in the spreadsheet, fetched from the
// spreadsheet metadata and cached for tabsCacheTTL.
func (c *Client) Tabs(ctx context.Context) ([]string, error) {
	c.tabsMu.Lock()
	defer c.tabsMu.Unlock()
	if c.tabs != nil && time.Since(c.tabsAt) < tabsCacheTTL {
		return c.tabs, nil
	}

	tok, err := c.provider.token(ctx)
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s/%s?fields=sheets.properties.title", baseURL, url.PathEscape(c.sheetID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("building tabs request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tabs fetch: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading tabs response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tabs fetch returned %d: %s", resp.StatusCode, truncate(body))
	}

	var doc struct {
		Sheets []struct {
			Properties struct {
				Title string `json:"title"`
			} `json:"properties"`
		} `json:"sheets"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parsing tabs response: %w", err)
	}

	out := make([]string, 0, len(doc.Sheets))
	for _, s := range doc.Sheets {
		if s.Properties.Title != "" {
			out = append(out, s.Properties.Title)
		}
	}
	c.tabs = out
	c.tabsAt = time.Now()
	return out, nil
}

// TabExists reports whether name is a tab in the spreadsheet.
func (c *Client) TabExists(ctx context.Context, name string) (bool, error) {
	tabs, err := c.Tabs(ctx)
	if err != nil {
		return false, err
	}
	for _, t := range tabs {
		if t == name {
			return true, nil
		}
	}
	return false, nil
}

// AppendRowWithRetry writes one row to the given tab, deduping by the first
// column (job_id) and retrying up to maxRetries additional times with
// exponential backoff (1s, 2s, 4s, ...) on any failure.
func (c *Client) AppendRowWithRetry(ctx context.Context, tab string, row []string, maxRetries int) error {
	total := maxRetries + 1
	var lastErr error
	for attempt := 1; attempt <= total; attempt++ {
		exists, err := c.RowExists(ctx, tab, row[0])
		if err != nil {
			lastErr = err
		} else if exists {
			log.Printf("sheets: row for job %s already present in %q, skipping append", row[0], tab)
			return nil
		} else if err := c.AppendRow(ctx, tab, row); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if attempt < total {
			delay := time.Duration(1<<(attempt-1)) * time.Second
			log.Printf("sheets append to %q attempt %d/%d failed: %v; retrying in %s", tab, attempt, total, lastErr, delay)
			select {
			case <-ctx.Done():
				return fmt.Errorf("sheets append canceled during retry wait: %w", ctx.Err())
			case <-time.After(delay):
			}
		}
	}
	return fmt.Errorf("sheets append to %q failed after %d attempts: %w", tab, total, lastErr)
}

// RowExists reports whether jobID appears in the first column of the given tab.
func (c *Client) RowExists(ctx context.Context, tab, jobID string) (bool, error) {
	tok, err := c.provider.token(ctx)
	if err != nil {
		return false, err
	}
	u := fmt.Sprintf("%s/%s/values/%s", baseURL, url.PathEscape(c.sheetID), url.PathEscape(a1Range(tab, "A1:A")))
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

// AppendRow appends a single row to the given tab (spreadsheets.values.append).
// The range points at A1; the API finds the logical table and appends after
// its last row, so headers are preserved.
func (c *Client) AppendRow(ctx context.Context, tab string, row []string) error {
	tok, err := c.provider.token(ctx)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{"values": [][]string{sanitizeRow(row)}})
	if err != nil {
		return fmt.Errorf("marshaling append body: %w", err)
	}
	u := fmt.Sprintf("%s/%s/values/%s:append?valueInputOption=RAW&insertDataOption=INSERT_ROWS",
		baseURL, url.PathEscape(c.sheetID), url.PathEscape(a1Range(tab, "A1")))
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

// a1Range builds an A1 range for a tab, single-quoting the name (with embedded
// quotes doubled) so tabs with spaces, commas, slashes, etc. parse correctly.
func a1Range(tab, cells string) string {
	tab = strings.ReplaceAll(tab, "'", "''")
	return fmt.Sprintf("'%s'!%s", tab, cells)
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
