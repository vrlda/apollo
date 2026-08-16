package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/danilrybalkin/apollo-dash/db"
)

// ── Resend client ─────────────────────────────────────────────────────────────

type resendClient struct {
	apiKey string
	from   string
	client *http.Client
}

var emailer *resendClient

func InitEmailer() {
	key := os.Getenv("RESEND_API_KEY")
	if key == "" {
		log.Println("Email: RESEND_API_KEY not set — emails disabled")
		return
	}
	emailer = &resendClient{
		apiKey: key,
		from:   "AgentHQ <hello@vulta.one>",
		client: &http.Client{Timeout: 10 * time.Second},
	}
	log.Println("Email: Resend client ready")
}

func sendEmail(ctx context.Context, to, subject, htmlBody string) {
	if emailer == nil {
		return
	}
	go func() {
		payload := map[string]interface{}{
			"from":    emailer.from,
			"to":      []string{to},
			"subject": subject,
			"html":    htmlBody,
		}
		body, _ := json.Marshal(payload)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(body))
		if err != nil {
			log.Printf("Email: build request: %v", err)
			return
		}
		req.Header.Set("Authorization", "Bearer "+emailer.apiKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := emailer.client.Do(req)
		if err != nil {
			log.Printf("Email: send to %s: %v", to, err)
			return
		}
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			log.Printf("Email: Resend returned %d for %s", resp.StatusCode, to)
		}
	}()
}

// ── Templates ─────────────────────────────────────────────────────────────────

func appURL() string {
	if u := os.Getenv("APP_URL"); u != "" {
		return u
	}
	return "https://agenthq.ai"
}

func wrapper(content string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<body style="font-family:system-ui,sans-serif;background:#ede9e0;margin:0;padding:40px 20px">
<div style="max-width:480px;margin:0 auto;background:#ffffff;border-radius:20px;padding:40px;box-shadow:0 2px 20px rgba(0,0,0,0.07)">
  <div style="margin-bottom:28px">
    <span style="font-size:18px;font-weight:900;color:#1c1c1e;letter-spacing:-0.5px">AgentHQ</span>
  </div>
  %s
  <p style="color:#9a9a9a;font-size:12px;margin-top:32px;padding-top:20px;border-top:1px solid #f0ede8">
    AgentHQ · Questions? <a href="mailto:support@agenthq.ai" style="color:#6b6b6b">support@agenthq.ai</a>
  </p>
</div>
</body>
</html>`, content)
}

func SendWelcomeEmail(ctx context.Context, to, name string) {
	first := name
	if first == "" {
		first = "there"
	}
	body := wrapper(fmt.Sprintf(`
  <h1 style="font-size:22px;font-weight:800;color:#1c1c1e;margin:0 0 12px;letter-spacing:-0.3px">Welcome to AgentHQ</h1>
  <p style="color:#4a4a4a;font-size:15px;line-height:1.7;margin:0 0 8px">Hi %s,</p>
  <p style="color:#4a4a4a;font-size:15px;line-height:1.7;margin:0 0 24px">
    Your account is ready. You have a <strong>3-day free trial</strong> — no credit card required.
    A demo company with an AI agent is already waiting for you inside.
  </p>
  <a href="%s/dashboard" style="display:inline-block;background:#1c1c1e;color:#f5c53f;text-decoration:none;padding:12px 26px;border-radius:12px;font-weight:700;font-size:14px;margin-bottom:24px">Open dashboard →</a>
  <p style="color:#6b6b6b;font-size:13px;line-height:1.6;margin:0">
    Your trial gives you full access to all features. Add your OpenRouter API key in
    Account → API Keys to enable AI tasks.
  </p>`, first, appURL()))
	sendEmail(ctx, to, "Welcome to AgentHQ — your trial has started", body)
}

func SendTrialExpiryWarningEmail(ctx context.Context, to, name string, hoursLeft int) {
	first := name
	if first == "" {
		first = "there"
	}
	timeStr := "tomorrow"
	if hoursLeft <= 6 {
		timeStr = "in a few hours"
	}
	body := wrapper(fmt.Sprintf(`
  <h1 style="font-size:22px;font-weight:800;color:#1c1c1e;margin:0 0 12px;letter-spacing:-0.3px">Your trial ends %s</h1>
  <p style="color:#4a4a4a;font-size:15px;line-height:1.7;margin:0 0 8px">Hi %s,</p>
  <p style="color:#4a4a4a;font-size:15px;line-height:1.7;margin:0 0 24px">
    Your AgentHQ free trial expires %s. Upgrade to keep your agents running — all your data, companies, and memory stay intact.
  </p>
  <a href="%s/account" style="display:inline-block;background:#1c1c1e;color:#f5c53f;text-decoration:none;padding:12px 26px;border-radius:12px;font-weight:700;font-size:14px;margin-bottom:24px">Upgrade now →</a>
  <p style="color:#6b6b6b;font-size:13px;line-height:1.6;margin:0">
    After your trial ends, your account and all data are preserved. Agents will pause until you upgrade.
  </p>`, timeStr, first, timeStr, appURL()))
	sendEmail(ctx, to, "Your AgentHQ trial expires "+timeStr, body)
}

func SendPaymentConfirmedEmail(ctx context.Context, to, name string) {
	first := name
	if first == "" {
		first = "there"
	}
	body := wrapper(fmt.Sprintf(`
  <h1 style="font-size:22px;font-weight:800;color:#1c1c1e;margin:0 0 12px;letter-spacing:-0.3px">You're now on Pro</h1>
  <p style="color:#4a4a4a;font-size:15px;line-height:1.7;margin:0 0 8px">Hi %s,</p>
  <p style="color:#4a4a4a;font-size:15px;line-height:1.7;margin:0 0 24px">
    Payment confirmed. Your AgentHQ account is now on the <strong>Pro plan</strong> for the next 30 days.
    All agents are active and running.
  </p>
  <a href="%s/dashboard" style="display:inline-block;background:#1c1c1e;color:#f5c53f;text-decoration:none;padding:12px 26px;border-radius:12px;font-weight:700;font-size:14px;margin-bottom:24px">Go to dashboard →</a>
  <p style="color:#6b6b6b;font-size:13px;line-height:1.6;margin:0">
    To renew, visit Account → Subscription before your plan expires. Questions? Reply to this email.
  </p>`, first, appURL()))
	sendEmail(ctx, to, "Payment confirmed — AgentHQ Pro is active", body)
}

func SendRenewalReminderEmail(ctx context.Context, to, name string, daysLeft int) {
	first := name
	if first == "" {
		first = "there"
	}
	body := wrapper(fmt.Sprintf(`
  <h1 style="font-size:22px;font-weight:800;color:#1c1c1e;margin:0 0 12px;letter-spacing:-0.3px">Your Pro plan renews in %d days</h1>
  <p style="color:#4a4a4a;font-size:15px;line-height:1.7;margin:0 0 8px">Hi %s,</p>
  <p style="color:#4a4a4a;font-size:15px;line-height:1.7;margin:0 0 24px">
    Your AgentHQ Pro subscription expires in %d days. Renew now to keep your agents running without interruption.
  </p>
  <a href="%s/account" style="display:inline-block;background:#1c1c1e;color:#f5c53f;text-decoration:none;padding:12px 26px;border-radius:12px;font-weight:700;font-size:14px;margin-bottom:24px">Renew subscription →</a>`, daysLeft, first, daysLeft, appURL()))
	sendEmail(ctx, to, fmt.Sprintf("AgentHQ Pro renews in %d days", daysLeft), body)
}

// ── Trial expiry background job ───────────────────────────────────────────────

func StartEmailNotifier(ctx context.Context) {
	go func() {
		// Run once at startup after a brief delay, then every 12 hours
		time.Sleep(30 * time.Second)
		runNotifierCycle(ctx)
		ticker := time.NewTicker(12 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runNotifierCycle(ctx)
			}
		}
	}()
}

func runNotifierCycle(ctx context.Context) {
	if emailer == nil {
		return
	}
	notifyTrialExpiries(ctx)
	notifyRenewalReminders(ctx)
}

func notifyTrialExpiries(ctx context.Context) {
	users, err := db.UsersNeedingTrialWarning()
	if err != nil {
		log.Printf("Notifier: trial warning query: %v", err)
		return
	}
	for _, u := range users {
		SendTrialExpiryWarningEmail(ctx, u.Email, u.Name, 24)
		db.MarkTrialWarningSent(u.ID)
		log.Printf("Notifier: sent trial warning to %s", u.Email)
	}
}

func notifyRenewalReminders(ctx context.Context) {
	users, err := db.UsersNeedingRenewalWarning()
	if err != nil {
		log.Printf("Notifier: renewal reminder query: %v", err)
		return
	}
	for _, u := range users {
		SendRenewalReminderEmail(ctx, u.Email, u.Name, 3)
		db.MarkRenewalWarningSent(u.ID)
		log.Printf("Notifier: sent renewal reminder to %s", u.Email)
	}
}
