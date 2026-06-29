package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMiddleware_DevMode_NoHeader(t *testing.T) {
	handler := AuthMiddleware("")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing header, got %d", rec.Code)
	}

	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "missing Authorization header" {
		t.Fatalf("expected 'missing Authorization header', got %q", body["error"])
	}
}

func TestAuthMiddleware_DevMode_BadFormat(t *testing.T) {
	handler := AuthMiddleware("")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Basic something")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for bad format, got %d", rec.Code)
	}

	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "invalid Authorization format, expected Bearer" {
		t.Fatalf("expected format error, got %q", body["error"])
	}
}

func TestAuthMiddleware_DevMode_EmptyToken(t *testing.T) {
	handler := AuthMiddleware("")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for empty token, got %d", rec.Code)
	}

	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "invalid Authorization format, expected Bearer" {
		t.Fatalf("expected format error for 'Bearer ' (7 chars), got %q", body["error"])
	}
}

func TestAuthMiddleware_DevMode_PassesWithAnyToken(t *testing.T) {
	handler := AuthMiddleware("")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := r.Context().Value(KeyIDKey).(UserInfo)
		if !ok {
			t.Fatal("expected UserInfo in context")
		}
		if user.Sub != "dev-user" {
			t.Fatalf("expected dev-user, got %s", user.Sub)
		}
		if user.Email != "dev@localhost" {
			t.Fatalf("expected dev@localhost, got %s", user.Email)
		}
		if user.Name != "Dev User" {
			t.Fatalf("expected 'Dev User', got %s", user.Name)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer fake-token-anything")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 in dev mode, got %d", rec.Code)
	}
}

func TestAuthMiddleware_ProdMode_InvalidTokenReturns401(t *testing.T) {
	handler := AuthMiddleware("my-client-id.apps.googleusercontent.com")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer garbage-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 in production mode for invalid token, got %d", rec.Code)
	}

	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "unauthorized" {
		t.Fatalf("expected 'unauthorized', got %q", body["error"])
	}
}

func TestAuthMiddleware_ProdMode_ValidTokenPopulatesUserInfo(t *testing.T) {
	handler := AuthMiddleware("audience")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_DevMode_NoContextWhenNoAuth(t *testing.T) {
	handler := AuthMiddleware("")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called when no auth header")
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestWriteAuthError(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteAuthError(rec, "test error message")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 status, got %d", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected application/json content type, got %s", ct)
	}

	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "test error message" {
		t.Fatalf("expected 'test error message', got %q", body["error"])
	}
}

func TestUserInfo_ContextKey(t *testing.T) {
	user := UserInfo{
		Sub:     "12345",
		Email:   "test@example.com",
		Name:    "Test User",
		Picture: "https://example.com/pic.jpg",
	}

	ctx := context.WithValue(context.Background(), KeyIDKey, user)

	got, ok := ctx.Value(KeyIDKey).(UserInfo)
	if !ok {
		t.Fatal("failed to extract UserInfo from context")
	}

	if got.Sub != "12345" {
		t.Fatalf("expected Sub=12345, got %s", got.Sub)
	}
	if got.Email != "test@example.com" {
		t.Fatalf("expected Email=test@example.com, got %s", got.Email)
	}
	if got.Name != "Test User" {
		t.Fatalf("expected Name='Test User', got %s", got.Name)
	}
	if got.Picture != "https://example.com/pic.jpg" {
		t.Fatalf("expected Picture, got %s", got.Picture)
	}
}

func TestUserFromContext(t *testing.T) {
	user := UserInfo{
		Sub:   "12345",
		Email: "test@example.com",
		Name:  "Test User",
	}

	ctx := context.WithValue(context.Background(), KeyIDKey, user)
	got, ok := UserFromContext(ctx)

	if !ok {
		t.Fatal("UserFromContext returned false")
	}
	if got.Sub != "12345" {
		t.Fatalf("expected Sub=12345, got %s", got.Sub)
	}
	if got.Email != "test@example.com" {
		t.Fatalf("expected Email, got %s", got.Email)
	}

	_, ok = UserFromContext(context.Background())
	if ok {
		t.Fatal("expected false for empty context")
	}
}

func TestAuthMiddleware_DevMode_NotBearerPrefix(t *testing.T) {
	handler := AuthMiddleware("")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "bearer fake-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for lowercase bearer, got %d", rec.Code)
	}
}

func TestAuthMiddleware_DevMode_ExactBearerPrefix(t *testing.T) {
	handler := AuthMiddleware("")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := r.Context().Value(KeyIDKey).(UserInfo)
		if !ok {
			t.Fatal("expected UserInfo in context")
		}
		if user.Sub != "dev-user" {
			t.Fatalf("expected dev-user, got %s", user.Sub)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer my-test-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 in dev mode, got %d", rec.Code)
	}
}
