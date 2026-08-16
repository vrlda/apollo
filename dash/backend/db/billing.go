package db

import "time"

func CreateBillingSession(id, userID, paymentRequestID string) error {
	_, err := DB.Exec(
		`INSERT INTO billing_sessions (id, user_id, payment_request_id, status) VALUES (?, ?, ?, 'pending')`,
		id, userID, paymentRequestID,
	)
	return err
}

func GetBillingSessionByPaymentRequestID(paymentRequestID string) (userID string, err error) {
	err = DB.QueryRow(
		`SELECT user_id FROM billing_sessions WHERE payment_request_id = ?`, paymentRequestID,
	).Scan(&userID)
	return
}

func ConfirmBillingSession(paymentRequestID string) error {
	_, err := DB.Exec(
		`UPDATE billing_sessions SET status = 'confirmed', updated_at = CURRENT_TIMESTAMP WHERE payment_request_id = ?`,
		paymentRequestID,
	)
	return err
}

// ActivateProPlan upgrades a user to pro for 30 days and resets renewal warning flag.
func ActivateProPlan(userID string) error {
	endsAt := time.Now().UTC().AddDate(0, 0, 30).Format("2006-01-02 15:04:05")
	_, err := DB.Exec(
		`UPDATE users SET plan = 'pro', subscription_ends_at = ?, renewal_warning_sent = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		endsAt, userID,
	)
	return err
}
