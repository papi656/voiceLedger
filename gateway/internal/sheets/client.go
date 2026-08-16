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
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
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
	tabs   []tabInfo
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
	infos, err := c.tabInfos(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(infos))
	for _, ti := range infos {
		out = append(out, ti.Title)
	}
	return out, nil
}

// tabInfo pairs a tab title with its numeric sheet id.
type tabInfo struct {
	Title   string
	SheetID int64
}

// tabInfos fetches and caches the tab metadata.
func (c *Client) tabInfos(ctx context.Context) ([]tabInfo, error) {
	c.tabsMu.Lock()
	defer c.tabsMu.Unlock()
	if c.tabs != nil && time.Since(c.tabsAt) < tabsCacheTTL {
		return c.tabs, nil
	}

	tok, err := c.provider.token(ctx)
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s/%s?fields=sheets.properties.title,sheets.properties.sheetId", baseURL, url.PathEscape(c.sheetID))
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
				Title   string `json:"title"`
				SheetID int64  `json:"sheetId"`
			} `json:"properties"`
		} `json:"sheets"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parsing tabs response: %w", err)
	}

	out := make([]tabInfo, 0, len(doc.Sheets))
	for _, s := range doc.Sheets {
		if s.Properties.Title != "" {
			out = append(out, tabInfo{Title: s.Properties.Title, SheetID: s.Properties.SheetID})
		}
	}
	c.tabs = out
	c.tabsAt = time.Now()
	return out, nil
}

// sheetIDFor resolves a tab title to its numeric sheet id.
func (c *Client) sheetIDFor(ctx context.Context, tab string) (int64, bool) {
	infos, err := c.tabInfos(ctx)
	if err != nil {
		return 0, false
	}
	for _, ti := range infos {
		if ti.Title == tab {
			return ti.SheetID, true
		}
	}
	return 0, false
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

// BuildRow renders one append row in the spreadsheet's own style (April, 2026
// layout): Date | Type | Amount | Comments. The date is an Excel serial number
// (renders per the tab's date format), the amount is a plain number (so SUM
// works), and the job_id lives in column E for dedupe.
func BuildRow(jobID, date, price, place, category string) []any {
	return []any{
		dateSerial(date),
		capitalize(category),
		parseAmount(price),
		place,
		jobID,
	}
}

// excelEpoch is the base date of Excel/Sheets serial date numbers.
var excelEpoch = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

// dateSerial converts an ISO date (or a few common formats) to a Sheets serial
// date number; falls back to today when unparseable.
func dateSerial(s string) float64 {
	t := time.Now()
	for _, layout := range []string{"2006-01-02", "Jan 2, 2006", "2 Jan 2006", "January 2, 2006"} {
		if p, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
			t = p
			break
		}
	}
	return float64(int(t.Sub(excelEpoch).Hours() / 24))
}

var amountRe = regexp.MustCompile(`[-+]?\d[\d,]*(?:\.\d+)?`)

// parseAmount extracts the numeric value from a price string like "3000 yen"
// or "¥2,853" → 3000 / 2853. Returns an empty string when nothing numeric is
// found (the cell is left blank instead of writing junk).
func parseAmount(s string) any {
	m := amountRe.FindString(s)
	if m == "" {
		return ""
	}
	m = strings.ReplaceAll(m, ",", "")
	f, err := strconv.ParseFloat(m, 64)
	if err != nil {
		return ""
	}
	return f
}

// capitalize upper-cases the first letter ("shopping" → "Shopping").
func capitalize(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// AppendRowWithRetry writes one row to the given tab, deduping by the job_id
// column (job_id) and retrying up to maxRetries additional times with
// exponential backoff (1s, 2s, 4s, ...) on any failure.
func (c *Client) AppendRowWithRetry(ctx context.Context, tab string, row []any, maxRetries int) error {
	total := maxRetries + 1
	var lastErr error
	for attempt := 1; attempt <= total; attempt++ {
		jobID, _ := row[0].(string)
		exists, err := c.RowExists(ctx, tab, jobID)
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
	u := fmt.Sprintf("%s/%s/values/%s", baseURL, url.PathEscape(c.sheetID), url.PathEscape(a1Range(tab, "E1:E")))
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
func (c *Client) AppendRow(ctx context.Context, tab string, row []any) error {
	tok, err := c.provider.token(ctx)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{"values": [][]any{sanitizeRow(row)}})
	if err != nil {
		return fmt.Errorf("marshaling append body: %w", err)
	}
	u := fmt.Sprintf("%s/%s/values/%s:append?valueInputOption=RAW&insertDataOption=INSERT_ROWS",
		baseURL, url.PathEscape(c.sheetID), url.PathEscape(a1Range(tab, "A1:E")))
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

	// API-appended cells do not inherit the tab's column formats, so apply the
	// April-style formats (date on A, currency on C) to the appended row.
	c.applyRowFormats(ctx, tab, body)
	return nil
}

// updatedRowNumber extracts the row index from an append response's
// updates.updatedRange ("'August, 2026'!A5:E5" → 5).
func updatedRowNumber(body []byte) int {
	var ar struct {
		Updates struct {
			UpdatedRange string `json:"updatedRange"`
		} `json:"updates"`
	}
	if err := json.Unmarshal(body, &ar); err != nil || ar.Updates.UpdatedRange == "" {
		return 0
	}
	m := regexp.MustCompile(`[A-Z]+(\d+)`).FindStringSubmatch(ar.Updates.UpdatedRange)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// applyRowFormats sets the number formats (DATE on column A, CURRENCY on
// column C) for the appended row, matching the tab's own style. Best-effort:
// value correctness does not depend on it.
func (c *Client) applyRowFormats(ctx context.Context, tab string, appendBody []byte) {
	row := updatedRowNumber(appendBody)
	if row == 0 {
		log.Printf("sheets: could not parse updatedRange, skipping row formatting")
		return
	}
	sid, ok := c.sheetIDFor(ctx, tab)
	if !ok {
		log.Printf("sheets: unknown tab %q, skipping row formatting", tab)
		return
	}
	requests := []map[string]any{
		formatRequest(sid, row, 0, 1, "DATE", "M/d/yyyy"),
		formatRequest(sid, row, 2, 3, "CURRENCY", "[$¥]#,##0"),
	}
	tok, err := c.provider.token(ctx)
	if err != nil {
		log.Printf("sheets: row formatting skipped: %v", err)
		return
	}
	payload, _ := json.Marshal(map[string]any{"requests": requests})
	u := fmt.Sprintf("%s/%s:batchUpdate", baseURL, url.PathEscape(c.sheetID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		log.Printf("sheets: row formatting skipped: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		log.Printf("sheets: row formatting failed: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		log.Printf("sheets: row formatting returned %d: %s", resp.StatusCode, truncate(b))
	}
}

// formatRequest builds a repeatCell request setting a number format on one
// column of the given row (row and column indexes are 0-based for the API).
func formatRequest(sheetID int64, row, colStart, colEnd int, ftype, pattern string) map[string]any {
	return map[string]any{
		"repeatCell": map[string]any{
			"range": map[string]any{
				"sheetId":          sheetID,
				"startRowIndex":    row - 1,
				"endRowIndex":      row,
				"startColumnIndex": colStart,
				"endColumnIndex":   colEnd,
			},
			"cell": map[string]any{
				"userEnteredFormat": map[string]any{
					"numberFormat": map[string]any{
						"type":    ftype,
						"pattern": pattern,
					},
				},
			},
			"fields": "userEnteredFormat.numberFormat",
		},
	}
}

// a1Range builds an A1 range for a tab, single-quoting the name (with embedded
// quotes doubled) so tabs with spaces, commas, slashes, etc. parse correctly.
func a1Range(tab, cells string) string {
	tab = strings.ReplaceAll(tab, "'", "''")
	return fmt.Sprintf("'%s'!%s", tab, cells)
}

// sanitizeRow prefixes any string cell starting with "=" so a transcription
// can't be interpreted as a formula when the row is written. Numbers pass
// through untouched.
func sanitizeRow(row []any) []any {
	out := make([]any, len(row))
	for i, v := range row {
		if s, ok := v.(string); ok && strings.HasPrefix(s, "=") {
			out[i] = "'" + s
			continue
		}
		out[i] = v
	}
	return out
}
