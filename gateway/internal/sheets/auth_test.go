package sheets

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestBuildAssertion verifies the service-account JWT is well-formed and
// RS256-signed with the key from the JSON file.
func TestBuildAssertion(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	saJSON := fmt.Sprintf(`{
		"client_email": "test@proj.iam.gserviceaccount.com",
		"private_key":  %q,
		"token_uri":    "https://oauth2.googleapis.com/token"
	}`, string(pemKey))

	kp := filepath.Join(t.TempDir(), "key.json")
	if err := os.WriteFile(kp, []byte(saJSON), 0600); err != nil {
		t.Fatal(err)
	}

	p, err := newTokenProvider(kp, &http.Client{})
	if err != nil {
		t.Fatalf("newTokenProvider: %v", err)
	}

	ass, err := p.buildAssertion()
	if err != nil {
		t.Fatalf("buildAssertion: %v", err)
	}
	parts := strings.Split(ass, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT must have 3 parts, got %d", len(parts))
	}

	// Header + claims decode.
	hdrJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || string(hdrJSON) != `{"alg":"RS256","typ":"JWT"}` {
		t.Fatalf("bad header: %q err=%v", hdrJSON, err)
	}
	var claims map[string]any
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("claims decode: %v", err)
	}
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		t.Fatalf("claims parse: %v", err)
	}
	if claims["iss"] != "test@proj.iam.gserviceaccount.com" {
		t.Errorf("iss = %v", claims["iss"])
	}
	if claims["scope"] != scope {
		t.Errorf("scope = %v", claims["scope"])
	}
	if claims["aud"] != tokenURL {
		t.Errorf("aud = %v", claims["aud"])
	}
	if exp, ok := claims["exp"].(float64); !ok || exp <= claims["iat"].(float64) {
		t.Errorf("exp/iat invalid: %v", claims)
	}

	// Signature verifies with the public key.
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("sig decode: %v", err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatalf("signature does not verify: %v", err)
	}
}

func TestSanitizeRow(t *testing.T) {
	got := sanitizeRow([]any{"a", "=HYPERLINK(\"http://evil\")", 3000.0, "b"})
	want := []any{"a", "'=HYPERLINK(\"http://evil\")", 3000.0, "b"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sanitizeRow[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestParseAmount(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"3000 yen", 3000, true},
		{"¥2,853", 2853, true},
		{"1,234.56 usd", 1234.56, true},
		{"about 5000", 5000, true},
		{"no price here", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		got := parseAmount(c.in)
		if !c.ok {
			if got != "" {
				t.Errorf("parseAmount(%q) = %v, want empty", c.in, got)
			}
			continue
		}
		f, isNum := got.(float64)
		if !isNum || f != c.want {
			t.Errorf("parseAmount(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestDateSerial(t *testing.T) {
	// 2026-06-24: verify against a known serial (2026-06-24 = 46202 days after 1899-12-30).
	got := dateSerial("2026-06-24")
	want := float64(time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC).Sub(excelEpoch).Hours() / 24)
	if got != want {
		t.Errorf("dateSerial = %v, want %v", got, want)
	}
	// Invalid input falls back to today (still a number).
	if got := dateSerial("garbage"); got <= 40000 {
		t.Errorf("dateSerial fallback = %v, want > 40000", got)
	}
}

func TestBuildRow(t *testing.T) {
	row := BuildRow("2026-06-24", "¥2,853 yen", "Sanwa", "shopping")
	if len(row) != 4 {
		t.Fatalf("BuildRow len = %d, want 4", len(row))
	}
	if row[0] != dateSerial("2026-06-24") {
		t.Errorf("date cell = %v", row[0])
	}
	if row[1] != "Shopping" {
		t.Errorf("type cell = %v, want Shopping", row[1])
	}
	if row[2] != 2853.0 {
		t.Errorf("amount cell = %v, want 2853", row[2])
	}
	if row[3] != "Sanwa" {
		t.Errorf("comments cell = %v, want Sanwa", row[3])
	}
}

func TestCellEqual(t *testing.T) {
	cases := []struct {
		a, b any
		want bool
	}{
		{3000.0, 3000.0, true},
		{3000.0, "3000", true},
		{"3000 yen", "3000 yen", true},
		{"3000", "3000.0", true},
		{"Grocery", "Grocery", true},
		{"Grocery", "Shopping", false},
		{"", "", true},
	}
	for _, c := range cases {
		if got := cellEqual(c.a, c.b); got != c.want {
			t.Errorf("cellEqual(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate([]byte(strings.Repeat("x", 500))); len(got) != 303 {
		t.Errorf("truncate length = %d, want 303", len(got))
	}
}

func TestA1Range(t *testing.T) {
	cases := []struct{ tab, cells, want string }{
		{"Sheet1", "A1", "'Sheet1'!A1"},
		{"Jan, 2026", "A1:A", "'Jan, 2026'!A1:A"},
		{"Spendings/ Expences", "A1", "'Spendings/ Expences'!A1"},
		{"Bob's tab", "A1", "'Bob''s tab'!A1"},
	}
	for _, c := range cases {
		if got := a1Range(c.tab, c.cells); got != c.want {
			t.Errorf("a1Range(%q, %q) = %q, want %q", c.tab, c.cells, got, c.want)
		}
	}
}
