package db

import (
	"database/sql"
	"time"
)

type User struct {
	ID                 string    `json:"id"`
	Email              string    `json:"email"`
	Name               string    `json:"name"`
	OpenrouterAPIKey   string    `json:"openrouter_api_key,omitempty"`
	TrialEndsAt        string    `json:"trial_ends_at,omitempty"`
	IsBlocked          int       `json:"is_blocked"`
	Plan               string    `json:"plan"`
	SubscriptionEndsAt string    `json:"subscription_ends_at,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
}

type UserAdminView struct {
	User
	CompanyCount int `json:"company_count"`
	AgentCount   int `json:"agent_count"`
	TaskCount    int `json:"task_count"`
}

func scanUser(row *sql.Row) (*User, error) {
	u := &User{}
	var createdAtStr string
	err := row.Scan(
		&u.ID, &u.Email, &u.Name,
		&u.OpenrouterAPIKey, &u.TrialEndsAt,
		&u.IsBlocked, &u.Plan, &u.SubscriptionEndsAt,
		&createdAtStr,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr)
	return u, nil
}

// NOTE: prefix with table alias (e.g. "u.") when used in a JOIN to avoid ambiguous column errors.
// The "created_at" at the end must be qualified at the call site when there are other tables in the query.
const userSelectCols = `id, email, name,
	COALESCE(openrouter_api_key,''), COALESCE(trial_ends_at,''),
	COALESCE(is_blocked,0), COALESCE(plan,'trial'), COALESCE(subscription_ends_at,''),
	created_at`

// userSelectColsQualified is the same as userSelectCols but with all columns qualified with alias "u".
const userSelectColsQualified = `u.id, u.email, u.name,
	COALESCE(u.openrouter_api_key,''), COALESCE(u.trial_ends_at,''),
	COALESCE(u.is_blocked,0), COALESCE(u.plan,'trial'), COALESCE(u.subscription_ends_at,''),
	u.created_at`

func CreateUser(id, email, passwordHash, name string) (*User, error) {
	_, err := DB.Exec(
		`INSERT INTO users (id, email, password_hash, name, trial_ends_at, plan)
		 VALUES (?, ?, ?, ?, datetime('now', '+3 days'), 'trial')`,
		id, email, passwordHash, name,
	)
	if err != nil {
		return nil, err
	}
	return GetUserByID(id)
}

func GetUserByEmail(email string) (id, emailOut, passwordHash, name string, isBlocked int, err error) {
	row := DB.QueryRow(
		`SELECT id, email, password_hash, name, COALESCE(is_blocked,0) FROM users WHERE email = ?`, email,
	)
	err = row.Scan(&id, &emailOut, &passwordHash, &name, &isBlocked)
	return
}

func GetUserByID(id string) (*User, error) {
	row := DB.QueryRow(`SELECT `+userSelectCols+` FROM users WHERE id = ?`, id)
	return scanUser(row)
}

func CreateUserToken(token, userID string) error {
	_, err := DB.Exec(
		`INSERT INTO user_tokens (token, user_id) VALUES (?, ?)`,
		token, userID,
	)
	return err
}

func GetUserByToken(token string) (*User, error) {
	row := DB.QueryRow(
		`SELECT `+userSelectColsQualified+`
		 FROM user_tokens t
		 JOIN users u ON u.id = t.user_id
		 WHERE t.token = ?`, token,
	)
	return scanUser(row)
}

func DeleteUserToken(token string) error {
	_, err := DB.Exec(`DELETE FROM user_tokens WHERE token = ?`, token)
	return err
}

func UpdateUserName(id, name string) error {
	_, err := DB.Exec(
		`UPDATE users SET name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		name, id,
	)
	return err
}

func UpdateUserOpenrouterKey(id, key string) error {
	_, err := DB.Exec(
		`UPDATE users SET openrouter_api_key = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		key, id,
	)
	return err
}

func UpdateUserPassword(id, hash string) error {
	_, err := DB.Exec(
		`UPDATE users SET password_hash = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		hash, id,
	)
	return err
}

func GetUserOpenrouterKey(id string) string {
	var key string
	DB.QueryRow(`SELECT COALESCE(openrouter_api_key,'') FROM users WHERE id = ?`, id).Scan(&key)
	return key
}

func GetUserPasswordHash(id string) (string, error) {
	var hash string
	err := DB.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, id).Scan(&hash)
	return hash, err
}

func EmailExists(email string) bool {
	var count int
	DB.QueryRow(`SELECT COUNT(1) FROM users WHERE email = ?`, email).Scan(&count)
	return count > 0
}

func DeleteUser(id string) error {
	_, err := DB.Exec(`DELETE FROM users WHERE id = ?`, id)
	return err
}

func SetUserBlocked(id string, blocked bool) error {
	val := 0
	if blocked {
		val = 1
	}
	_, err := DB.Exec(
		`UPDATE users SET is_blocked = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		val, id,
	)
	return err
}

func SetUserPlan(id, plan, subscriptionEndsAt string) error {
	_, err := DB.Exec(
		`UPDATE users SET plan = ?, subscription_ends_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		plan, subscriptionEndsAt, id,
	)
	return err
}

func ListUsersAdmin() ([]UserAdminView, error) {
	rows, err := DB.Query(`
		SELECT ` + userSelectColsQualified + `,
			(SELECT COUNT(*) FROM companies c WHERE c.owner_user_id = u.id) AS company_count,
			(SELECT COUNT(*) FROM agents a JOIN companies c ON a.company_id = c.id WHERE c.owner_user_id = u.id) AS agent_count,
			(SELECT COUNT(*) FROM agent_tasks t JOIN companies c ON t.company_id = c.id WHERE c.owner_user_id = u.id) AS task_count
		FROM users u
		ORDER BY u.created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []UserAdminView
	for rows.Next() {
		var u UserAdminView
		var createdAtStr string
		if err := rows.Scan(
			&u.ID, &u.Email, &u.Name,
			&u.OpenrouterAPIKey, &u.TrialEndsAt,
			&u.IsBlocked, &u.Plan, &u.SubscriptionEndsAt,
			&createdAtStr,
			&u.CompanyCount, &u.AgentCount, &u.TaskCount,
		); err != nil {
			continue
		}
		u.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr)
		u.OpenrouterAPIKey = "" // never expose in admin list
		users = append(users, u)
	}
	if users == nil {
		users = []UserAdminView{}
	}
	return users, nil
}

// UserNotifyRow is used by the email notifier background job.
type UserNotifyRow struct {
	ID                 string
	Email              string
	Name               string
	Plan               string
	TrialEndsAt        string
	SubscriptionEndsAt string
	TrialWarningSent   int
	RenewalWarningSent int
}

// UsersNeedingTrialWarning returns trial users whose trial ends within 28 hours
// and haven't been sent a warning yet.
func UsersNeedingTrialWarning() ([]UserNotifyRow, error) {
	rows, err := DB.Query(`
		SELECT id, email, COALESCE(name,''), COALESCE(plan,'trial'),
		       COALESCE(trial_ends_at,''), COALESCE(subscription_ends_at,''),
		       COALESCE(trial_warning_sent,0), COALESCE(renewal_warning_sent,0)
		FROM users
		WHERE plan = 'trial'
		  AND is_blocked = 0
		  AND TRIM(IFNULL(trial_ends_at,'')) != ''
		  AND datetime(trial_ends_at) > datetime('now')
		  AND datetime(trial_ends_at) <= datetime('now', '+28 hours')
		  AND COALESCE(trial_warning_sent,0) = 0
	`)
	return scanNotifyRows(rows, err)
}

// UsersNeedingRenewalWarning returns pro users whose subscription ends within 4 days
// and haven't been sent a renewal warning yet.
func UsersNeedingRenewalWarning() ([]UserNotifyRow, error) {
	rows, err := DB.Query(`
		SELECT id, email, COALESCE(name,''), COALESCE(plan,'trial'),
		       COALESCE(trial_ends_at,''), COALESCE(subscription_ends_at,''),
		       COALESCE(trial_warning_sent,0), COALESCE(renewal_warning_sent,0)
		FROM users
		WHERE plan = 'pro'
		  AND is_blocked = 0
		  AND TRIM(IFNULL(subscription_ends_at,'')) != ''
		  AND datetime(subscription_ends_at) > datetime('now')
		  AND datetime(subscription_ends_at) <= datetime('now', '+4 days')
		  AND COALESCE(renewal_warning_sent,0) = 0
	`)
	return scanNotifyRows(rows, err)
}

func scanNotifyRows(rows interface {
	Scan(...interface{}) error
	Next() bool
	Close() error
}, err error) ([]UserNotifyRow, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserNotifyRow
	for rows.Next() {
		var r UserNotifyRow
		if e := rows.Scan(&r.ID, &r.Email, &r.Name, &r.Plan, &r.TrialEndsAt, &r.SubscriptionEndsAt, &r.TrialWarningSent, &r.RenewalWarningSent); e != nil {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func MarkTrialWarningSent(id string) {
	DB.Exec(`UPDATE users SET trial_warning_sent = 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
}

func MarkRenewalWarningSent(id string) {
	DB.Exec(`UPDATE users SET renewal_warning_sent = 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
}

type PlatformStats struct {
	TotalUsers       int `json:"total_users"`
	ActiveTrials     int `json:"active_trials"`
	PaidUsers        int `json:"paid_users"`
	BlockedUsers     int `json:"blocked_users"`
	TotalCompanies   int `json:"total_companies"`
	TotalDepartments int `json:"total_departments"`
	TotalAgents      int `json:"total_agents"`
	TotalTasks       int `json:"total_tasks"`
	TotalMemories    int `json:"total_memories"`
}

func GetPlatformStats() PlatformStats {
	var s PlatformStats
	DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&s.TotalUsers)
	DB.QueryRow(`SELECT COUNT(*) FROM users WHERE plan = 'trial' AND is_blocked = 0`).Scan(&s.ActiveTrials)
	DB.QueryRow(`SELECT COUNT(*) FROM users WHERE plan = 'pro' AND is_blocked = 0`).Scan(&s.PaidUsers)
	DB.QueryRow(`SELECT COUNT(*) FROM users WHERE is_blocked = 1`).Scan(&s.BlockedUsers)
	DB.QueryRow(`SELECT COUNT(*) FROM companies`).Scan(&s.TotalCompanies)
	DB.QueryRow(`SELECT COUNT(*) FROM departments`).Scan(&s.TotalDepartments)
	DB.QueryRow(`SELECT COUNT(*) FROM agents`).Scan(&s.TotalAgents)
	DB.QueryRow(`SELECT COUNT(*) FROM agent_tasks`).Scan(&s.TotalTasks)
	DB.QueryRow(`SELECT COUNT(*) FROM memory_entries`).Scan(&s.TotalMemories)
	return s
}
