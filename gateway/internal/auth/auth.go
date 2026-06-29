package auth

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"google.golang.org/api/idtoken"
)

// ContextKey is the type used for context keys to avoid collisions.
type ContextKey string

// KeyIDKey is the context key under which UserInfo is stored.
const KeyIDKey ContextKey = "key_id"

// UserInfo holds Google user data extracted from the verified ID token.
type UserInfo struct {
	Sub     string // Google user ID (unique, stable) — used for rate limiting
	Email   string
	Name    string
	Picture string
}

// UserFromContext extracts the UserInfo from a request context, if present.
func UserFromContext(ctx context.Context) (UserInfo, bool) {
	u, ok := ctx.Value(KeyIDKey).(UserInfo)
	return u, ok
}

// AuthMiddleware returns middleware that validates a Google OAuth ID token from
// the Authorization header. When audience is empty the middleware operates in
// dev mode, accepting any token and injecting a fixed dev-user identity.
func AuthMiddleware(audience string) func(http.Handler) http.Handler {
	devMode := audience == ""

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")

			if header == "" {
				WriteAuthError(w, "missing Authorization header")
				return
			}

			if len(header) < 8 || header[:7] != "Bearer " {
				WriteAuthError(w, "invalid Authorization format, expected Bearer")
				return
			}

			idToken := header[7:]
			if idToken == "" {
				WriteAuthError(w, "empty token")
				return
			}

			payload, err := idtoken.Validate(r.Context(), idToken, audience)
			if err != nil {
				if devMode {
					log.Printf("auth: dev mode, allowing invalid token (error: %v)", err)
					ctx := context.WithValue(r.Context(), KeyIDKey, UserInfo{
						Sub:   "dev-user",
						Email: "dev@localhost",
						Name:  "Dev User",
					})
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				log.Printf("auth: rejected (error: %v)", err)
				WriteAuthError(w, "unauthorized")
				return
			}

			user := UserInfo{
				Sub:   payload.Subject,
				Email: "",
				Name:  "",
			}

			if email, ok := payload.Claims["email"].(string); ok {
				user.Email = email
			}
			if name, ok := payload.Claims["name"].(string); ok {
				user.Name = name
			}
			if picture, ok := payload.Claims["picture"].(string); ok {
				user.Picture = picture
			}

			log.Printf("auth: user=%s email=%s", user.Sub, user.Email)

			ctx := context.WithValue(r.Context(), KeyIDKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// WriteAuthError sends a JSON-encoded 401 response.
func WriteAuthError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
