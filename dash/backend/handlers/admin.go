package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/danilrybalkin/apollo-dash/db"
)

func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	uid := CurrentUserID(r)
	if uid != "admin" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "admin only"})
		return false
	}
	return true
}

// GET /api/admin/stats
func AdminStatsHandler(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(db.GetPlatformStats())
}

// GET /api/admin/users
func AdminUsersHandler(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	users, err := db.ListUsersAdmin()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(users)
}

// PATCH /api/admin/users/{id}   — update plan, block status
// DELETE /api/admin/users/{id}  — delete user
func AdminUserByIDHandler(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	// Extract user ID from path: /api/admin/users/<id>
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/admin/users/"), "/")
	userID := parts[0]
	if userID == "" {
		http.Error(w, "user id required", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodPatch:
		var body struct {
			Plan               string `json:"plan"`
			SubscriptionEndsAt string `json:"subscription_ends_at"`
			IsBlocked          *bool  `json:"is_blocked"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if body.Plan != "" {
			if err := db.SetUserPlan(userID, body.Plan, body.SubscriptionEndsAt); err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
		}
		if body.IsBlocked != nil {
			if err := db.SetUserBlocked(userID, *body.IsBlocked); err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

	case http.MethodDelete:
		if err := db.DeleteUser(userID); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
