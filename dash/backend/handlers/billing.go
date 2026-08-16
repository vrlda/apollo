package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/danilrybalkin/apollo-dash/db"
	"github.com/google/uuid"
)

const vultaAPIBase = "https://vulta.one/api"

// ── Vulta API client ──────────────────────────────────────────────────────────

func vultaAPIKey() string        { return os.Getenv("VULTA_API_KEY") }
func vultaWebhookSecret() string { return os.Getenv("VULTA_WEBHOOK_SECRET") }

func vultaPost(ctx context.Context, path string, body interface{}) (map[string]interface{}, int, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, vultaAPIBase+path, bytes.NewReader(b))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+vultaAPIKey())
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return result, resp.StatusCode, nil
}

// ── POST /api/billing/checkout ────────────────────────────────────────────────

func BillingCheckoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := extractBearerToken(r)
	if token == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return
	}
	user, err := db.GetUserByToken(token)
	if err != nil || user == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return
	}

	if vultaAPIKey() == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "billing not configured"})
		return
	}

	// Use checkout sessions API with use_all_payout_destinations — no dest ID needed
	payload := map[string]interface{}{
		"amount_fiat":                 "39.00",
		"fiat_currency":               "USD",
		"external_reference_id":       "agenthq_user_" + user.ID,
		"use_all_payout_destinations": true,
	}

	result, status, err := vultaPost(r.Context(), "/checkout/sessions", payload)
	if err != nil || status >= 400 {
		errMsg := "failed to create checkout session"
		if e, ok := result["error"].(string); ok {
			errMsg = e
		}
		log.Printf("Billing: Vulta error for user %s: status=%d err=%v result=%v", user.ID, status, err, result)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{"error": errMsg})
		return
	}

	// Checkout session returns { "id": "...", "checkout_url": "..." }
	sessionID, _ := result["id"].(string)
	checkoutURL, _ := result["checkout_url"].(string)

	if sessionID == "" || checkoutURL == "" {
		log.Printf("Billing: unexpected Vulta response: %v", result)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid response from payment provider"})
		return
	}

	// Store billing session so webhook can map back to user
	_ = db.CreateBillingSession(uuid.New().String(), user.ID, sessionID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"checkout_url": checkoutURL})
}

// ── POST /api/billing/webhook ─────────────────────────────────────────────────

type vultaWebhookPayload struct {
	EventType        string `json:"event_type"`
	PaymentRequestID string `json:"payment_request_id"`
	ExternalRefID    string `json:"external_reference_id"`
	AmountFiat       string `json:"amount_fiat"`
	FiatCurrency     string `json:"fiat_currency"`
	Status           string `json:"status"`
	MerchantID       string `json:"merchant_id"`
}

func BillingWebhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	// Verify HMAC-SHA256 signature if webhook secret is configured
	secret := vultaWebhookSecret()
	if secret != "" {
		sig := r.Header.Get("X-Vulta-Signature")
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(rawBody)
		expected := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(sig), []byte(expected)) {
			log.Printf("Billing: invalid webhook signature")
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
	}

	var payload vultaWebhookPayload
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		http.Error(w, "bad payload", http.StatusBadRequest)
		return
	}

	log.Printf("Billing: webhook event_type=%s status=%s payment_request=%s ref=%s",
		payload.EventType, payload.Status, payload.PaymentRequestID, payload.ExternalRefID)

	// Only act on confirmed payments (event_type from webhook, status from payload)
	isConfirmed := payload.EventType == "payment.confirmed" || payload.Status == "payment.confirmed" || payload.Status == "CONFIRMED"
	if !isConfirmed {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Look up user by payment request ID
	userID, err := db.GetBillingSessionByPaymentRequestID(payload.PaymentRequestID)
	if err != nil {
		log.Printf("Billing: no billing session for payment_request %s: %v", payload.PaymentRequestID, err)
		w.WriteHeader(http.StatusOK) // ack anyway to prevent retries
		return
	}

	// Activate pro plan
	if err := db.ActivateProPlan(userID); err != nil {
		log.Printf("Billing: failed to activate pro for user %s: %v", userID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	_ = db.ConfirmBillingSession(payload.PaymentRequestID)

	// Send confirmation email
	user, err := db.GetUserByID(userID)
	if err == nil && user != nil {
		SendPaymentConfirmedEmail(context.Background(), user.Email, user.Name)
	}

	log.Printf("Billing: activated pro plan for user %s", userID)
	w.WriteHeader(http.StatusOK)
}
