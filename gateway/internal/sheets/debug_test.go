package sheets

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
)

func TestDebugValidation(t *testing.T) {
	keyPath := os.Getenv("GS_DEBUG_KEY")
	sheetID := os.Getenv("GS_DEBUG_SHEET")
	if keyPath == "" || sheetID == "" {
		t.Skip("set GS_DEBUG_KEY, GS_DEBUG_SHEET")
	}
	p, err := newTokenProvider(keyPath, &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	tok, err := p.token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Data validation on the Type column of April + a couple other tabs.
	for _, rng := range []string{"'April, 2026'!B1:B15", "'August, 2026'!B1:B5"} {
		u := fmt.Sprintf("https://sheets.googleapis.com/v4/spreadsheets/%s?ranges=%s&fields=sheets.data.rowData.values.dataValidation,sheets.data.rowData.values.formattedValue",
			sheetID, urlQueryEscape(rng))
		req, _ := http.NewRequest(http.MethodGet, u, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Logf("VALIDATION %s:\n%s", rng, string(b))
	}
}
