package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"google.golang.org/api/idtoken"
)

type contextKey string

const keyIDKey contextKey = "key_id"

// UserInfo holds Google user data extracted from the verified ID token.
type UserInfo struct {
	Sub     string // Google user ID (unique, stable) — used for rate limiting
	Email   string
	Name    string
	Picture string
}

func authMiddleware(audience string) func(http.Handler) http.Handler {
	devMode := audience == ""

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")

			if header == "" {
				writeAuthError(w, "missing Authorization header")
				return
			}

			if len(header) < 8 || header[:7] != "Bearer " {
				writeAuthError(w, "invalid Authorization format, expected Bearer")
				return
			}

			idToken := header[7:]
			if idToken == "" {
				writeAuthError(w, "empty token")
				return
			}

			payload, err := idtoken.Validate(r.Context(), idToken, audience)
			if err != nil {
				if devMode {
					log.Printf("auth: dev mode, allowing invalid token (error: %v)", err)
					ctx := context.WithValue(r.Context(), keyIDKey, UserInfo{
						Sub:   "dev-user",
						Email: "dev@localhost",
						Name:  "Dev User",
					})
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				log.Printf("auth: rejected (error: %v)", err)
				writeAuthError(w, "unauthorized")
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

			ctx := context.WithValue(r.Context(), keyIDKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeAuthError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
