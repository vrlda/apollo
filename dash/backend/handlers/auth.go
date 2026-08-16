package handlers

import (
	"context"
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"github.com/danilrybalkin/apollo-dash/db"
)

type contextKey string

const currentUserIDKey contextKey = "agenthq_user_id"

func CurrentUserID(r *http.Request) string {
	if v, ok := r.Context().Value(currentUserIDKey).(string); ok {
		return v
	}
	return ""
}

func BasicAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// Public endpoints — no auth check
		if strings.HasPrefix(r.URL.Path, "/api/auth/") {
			next.ServeHTTP(w, r)
			return
		}
		// Vulta webhook — authenticated by HMAC signature in handler itself
		if r.URL.Path == "/api/billing/webhook" {
			next.ServeHTTP(w, r)
			return
		}

		// Let the React app and static assets load. Protected pages still require a
		// user token client-side, and all API/WebSocket routes remain guarded here.
		if !strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/ws/") {
			next.ServeHTTP(w, r)
			return
		}

		// User session token check
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token := strings.TrimPrefix(authHeader, "Bearer ")
			// Try user token first
			user, err := db.GetUserByToken(token)
			if err == nil && user != nil {
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), currentUserIDKey, user.ID)))
				return
			}
			// Then try admin password
			envPass := os.Getenv("DASHBOARD_PASSWORD")
			if envPass != "" && subtle.ConstantTimeCompare([]byte(token), []byte(envPass)) == 1 {
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), currentUserIDKey, "admin")))
				return
			}
		}

		// Basic Auth fallback
		envPass := os.Getenv("DASHBOARD_PASSWORD")
		if envPass == "" {
			next.ServeHTTP(w, r)
			return
		}
		user, pass, ok := r.BasicAuth()
		if ok && subtle.ConstantTimeCompare([]byte(pass), []byte(envPass)) == 1 && user == "admin" {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), currentUserIDKey, "admin")))
			return
		}

		w.Header().Set("WWW-Authenticate", `Basic realm="AgentHQ"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
}
