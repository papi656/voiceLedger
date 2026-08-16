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
	got := sanitizeRow([]string{"a", "=HYPERLINK(\"http://evil\")", "b"})
	want := []string{"a", "'=HYPERLINK(\"http://evil\")", "b"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sanitizeRow[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate([]byte(strings.Repeat("x", 500))); len(got) != 303 {
		t.Errorf("truncate length = %d, want 303", len(got))
	}
}
