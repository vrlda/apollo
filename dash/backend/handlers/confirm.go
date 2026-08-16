package handlers

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// ─── Pending Confirmation Registry ──────────────────────────────────────────
// Maps confirmation ID → response channel. The tool loop blocks on the channel.
var (
	pendingConfirms   = map[string]chan bool{}
	pendingConfirmsMu sync.Mutex
)

// RegisterConfirmation creates a channel for the given ID. The tool loop reads it.
func RegisterConfirmation(id string) chan bool {
	ch := make(chan bool, 1)
	pendingConfirmsMu.Lock()
	pendingConfirms[id] = ch
	pendingConfirmsMu.Unlock()
	// Auto-reject after 5 minutes
	go func() {
		time.Sleep(5 * time.Minute)
		pendingConfirmsMu.Lock()
		if ch, ok := pendingConfirms[id]; ok {
			select {
			case ch <- false: // auto-reject
			default:
			}
			delete(pendingConfirms, id)
		}
		pendingConfirmsMu.Unlock()
	}()
	return ch
}

// ConfirmHandler: POST /api/confirm?id=xxx&action=approve|reject
func ConfirmHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	action := r.URL.Query().Get("action")
	if id == "" || action == "" {
		http.Error(w, "id and action required", http.StatusBadRequest)
		return
	}

	pendingConfirmsMu.Lock()
	ch, ok := pendingConfirms[id]
	if ok {
		delete(pendingConfirms, id)
	}
	pendingConfirmsMu.Unlock()

	if !ok {
		http.Error(w, "confirmation not found or expired", http.StatusNotFound)
		return
	}

	approved := action == "approve"
	ch <- approved
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"approved": approved})
}
