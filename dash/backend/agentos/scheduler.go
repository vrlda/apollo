package agentos

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

type SchedulerConfig struct {
	Mode         string  `json:"mode"`
	MinWorkers   int     `json:"min_workers"`
	MaxWorkers   int     `json:"max_workers"`
	MinReviewers int     `json:"min_reviewers"`
	MaxReviewers int     `json:"max_reviewers"`
	CPUSoft      float64 `json:"cpu_soft"`
	CPUHard      float64 `json:"cpu_hard"`
	RAMSoft      float64 `json:"ram_soft"`
	RAMHard      float64 `json:"ram_hard"`
}

func (s *Service) schedulerConfig() SchedulerConfig {
	cfg := SchedulerConfig{
		Mode:         s.getSettingString("agentos_scheduler_mode", "adaptive"),
		MinWorkers:   toInt(s.getSettingString("agentos_scheduler_min_workers", "1"), 1),
		MaxWorkers:   toInt(s.getSettingString("agentos_scheduler_max_workers", "4"), 4),
		MinReviewers: toInt(s.getSettingString("agentos_scheduler_min_reviewers", "1"), 1),
		MaxReviewers: toInt(s.getSettingString("agentos_scheduler_max_reviewers", "2"), 2),
		CPUSoft:      toFloat(s.getSettingString("agentos_scheduler_cpu_soft", "70"), 70),
		CPUHard:      toFloat(s.getSettingString("agentos_scheduler_cpu_hard", "90"), 90),
		RAMSoft:      toFloat(s.getSettingString("agentos_scheduler_ram_soft", "75"), 75),
		RAMHard:      toFloat(s.getSettingString("agentos_scheduler_ram_hard", "90"), 90),
	}
	if cfg.MinWorkers < 1 {
		cfg.MinWorkers = 1
	}
	if cfg.MaxWorkers < cfg.MinWorkers {
		cfg.MaxWorkers = cfg.MinWorkers
	}
	if cfg.MaxReviewers < cfg.MinReviewers {
		cfg.MaxReviewers = cfg.MinReviewers
	}
	return cfg
}

func (s *Service) runningTaskCount() int {
	var n int
	_ = s.db.QueryRow("SELECT COUNT(1) FROM agent_tasks WHERE status = 'running'").Scan(&n)
	return n
}

func (s *Service) currentResourcePressure() (cpuPercent float64, ramPercent float64) {
	cpuPercent = 25
	ramPercent = 35
	if samples, err := cpu.Percent(0, false); err == nil && len(samples) > 0 {
		cpuPercent = samples[0]
	}
	if vm, err := mem.VirtualMemory(); err == nil {
		ramPercent = vm.UsedPercent
	}
	return cpuPercent, ramPercent
}

func (s *Service) schedulerAdmission(task *AgentTask) (bool, string) {
	if strings.EqualFold(s.getSettingString("agentos_kill_switch", "false"), "true") {
		return false, "kill_switch"
	}
	cfg := s.schedulerConfig()
	cpuP, ramP := s.currentResourcePressure()
	if cpuP >= cfg.CPUHard {
		return false, "cpu_hard"
	}
	if ramP >= cfg.RAMHard {
		return false, "ram_hard"
	}
	if s.runningTaskCount() >= cfg.MaxWorkers {
		return false, "max_workers"
	}
	if task != nil {
		if task.Status == "blocked" && task.BlockedReason != "" {
			return false, task.BlockedReason
		}
	}
	return true, ""
}

func (s *Service) SchedulerState() map[string]interface{} {
	cfg := s.schedulerConfig()
	cpuP, ramP := s.currentResourcePressure()
	queued := 0
	running := 0
	blocked := 0
	_ = s.db.QueryRow("SELECT COUNT(1) FROM agent_tasks WHERE status = 'queued'").Scan(&queued)
	_ = s.db.QueryRow("SELECT COUNT(1) FROM agent_tasks WHERE status = 'running'").Scan(&running)
	_ = s.db.QueryRow("SELECT COUNT(1) FROM agent_tasks WHERE status = 'blocked'").Scan(&blocked)

	return map[string]interface{}{
		"config": cfg,
		"host_pressure": map[string]interface{}{
			"cpu_percent": cpuP,
			"ram_percent": ramP,
			"updated_at":  time.Now().UTC().Format(time.RFC3339),
		},
		"queue": map[string]interface{}{
			"queued":  queued,
			"running": running,
			"blocked": blocked,
		},
	}
}

func (s *Service) UpdateSchedulerConfig(payload map[string]interface{}) error {
	for _, key := range []string{
		"agentos_scheduler_mode",
		"agentos_scheduler_min_workers",
		"agentos_scheduler_max_workers",
		"agentos_scheduler_min_reviewers",
		"agentos_scheduler_max_reviewers",
		"agentos_scheduler_cpu_soft",
		"agentos_scheduler_cpu_hard",
		"agentos_scheduler_ram_soft",
		"agentos_scheduler_ram_hard",
	} {
		if v, ok := payload[key]; ok {
			_, _ = s.db.Exec("INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)", key, stringify(v))
		}
	}
	s.appendAudit("scheduler_updated", "scheduler", "agentos", "owner", payload)
	return nil
}

func stringify(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func toInt(v string, fallback int) int {
	i, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return fallback
	}
	return i
}

func toFloat(v string, fallback float64) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return fallback
	}
	return f
}

func parseAnyTime(value string, fallback time.Time) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	layouts := []string{time.RFC3339, time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02"}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t
		}
	}
	return fallback
}

func nextFromCron(expr string, from time.Time, loc *time.Location) time.Time {
	parts := strings.Fields(strings.TrimSpace(expr))
	if len(parts) != 5 {
		return from.Add(1 * time.Hour)
	}
	minute := parts[0]
	hour := parts[1]

	base := from.In(loc).Truncate(time.Minute).Add(time.Minute)

	if minute == "*" && hour == "*" {
		return base
	}
	if strings.HasPrefix(minute, "*/") && hour == "*" {
		n, err := strconv.Atoi(strings.TrimPrefix(minute, "*/"))
		if err != nil || n <= 0 {
			return from.Add(1 * time.Hour)
		}
		m := base.Minute()
		nextMin := ((m / n) + 1) * n
		delta := nextMin - m
		if delta <= 0 {
			delta += n
		}
		return base.Add(time.Duration(delta) * time.Minute)
	}
	if hour == "*" {
		m, err := strconv.Atoi(minute)
		if err != nil || m < 0 || m > 59 {
			return from.Add(1 * time.Hour)
		}
		t := time.Date(base.Year(), base.Month(), base.Day(), base.Hour(), m, 0, 0, loc)
		if !t.After(from.In(loc)) {
			t = t.Add(1 * time.Hour)
		}
		return t
	}

	m, errM := strconv.Atoi(minute)
	h, errH := strconv.Atoi(hour)
	if errM != nil || errH != nil || m < 0 || m > 59 || h < 0 || h > 23 {
		return from.Add(1 * time.Hour)
	}
	t := time.Date(base.Year(), base.Month(), base.Day(), h, m, 0, 0, loc)
	if !t.After(from.In(loc)) {
		t = t.Add(24 * time.Hour)
	}
	return t
}

func nextFromCalendar(rrule string, start time.Time, from time.Time) time.Time {
	rule := strings.ToUpper(strings.TrimSpace(rrule))
	if rule == "" {
		if start.After(from) {
			return start
		}
		return from.Add(24 * time.Hour)
	}

	freq := "DAILY"
	interval := 1
	for _, chunk := range strings.Split(rule, ";") {
		parts := strings.SplitN(strings.TrimSpace(chunk), "=", 2)
		if len(parts) != 2 {
			continue
		}
		switch parts[0] {
		case "FREQ":
			freq = strings.TrimSpace(parts[1])
		case "INTERVAL":
			if iv, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil && iv > 0 {
				interval = iv
			}
		}
	}

	next := start
	if next.IsZero() {
		next = from
	}
	for !next.After(from) {
		switch freq {
		case "HOURLY":
			next = next.Add(time.Duration(interval) * time.Hour)
		case "WEEKLY":
			next = next.AddDate(0, 0, 7*interval)
		case "MONTHLY":
			next = next.AddDate(0, interval, 0)
		default:
			next = next.AddDate(0, 0, interval)
		}
	}
	return next
}

func (s *Service) CreateSchedule(input Schedule) (Schedule, error) {
	if strings.TrimSpace(input.CompanyID) == "" || strings.TrimSpace(input.TargetAgentID) == "" {
		return Schedule{}, fmt.Errorf("company_id and target_agent_id are required")
	}
	input.ScheduleType = strings.ToLower(strings.TrimSpace(input.ScheduleType))
	if input.ScheduleType == "" {
		input.ScheduleType = "calendar"
	}
	if input.ScheduleType != "cron" && input.ScheduleType != "calendar" && input.ScheduleType != "once" {
		return Schedule{}, fmt.Errorf("schedule_type must be cron, calendar, or once")
	}
	if strings.TrimSpace(input.Timezone) == "" {
		input.Timezone = "UTC"
	}
	loc, err := time.LoadLocation(input.Timezone)
	if err != nil {
		loc = time.UTC
		input.Timezone = "UTC"
	}
	if strings.TrimSpace(input.PayloadJSON) == "" {
		input.PayloadJSON = `{"prompt":"Scheduled agent task"}`
	}
	if input.ID == "" {
		input.ID = uuid.NewString()
	}

	now := time.Now().In(loc)
	start := parseAnyTime(input.StartAt, now)
	nextRun := start
	if input.ScheduleType == "once" {
		if strings.TrimSpace(input.StartAt) == "" {
			return Schedule{}, fmt.Errorf("start_at is required for schedule_type once")
		}
		nextRun = start
	} else if input.ScheduleType == "cron" {
		nextRun = nextFromCron(input.CronExpr, now, loc)
	} else {
		nextRun = nextFromCalendar(input.RRule, start, now)
	}

	activeInt := 1
	if !input.IsActive && input.IsActive != true {
		activeInt = 0
	}

	_, err = s.db.Exec(`
		INSERT INTO schedules (id, company_id, department_id, target_agent_id, schedule_type, cron_expr, rrule, start_at, next_run_at, timezone, payload_json, is_active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, input.ID, input.CompanyID, input.DepartmentID, input.TargetAgentID, input.ScheduleType, input.CronExpr, input.RRule, start.UTC().Format(time.RFC3339), nextRun.UTC().Format(time.RFC3339), input.Timezone, input.PayloadJSON, activeInt)
	if err != nil {
		return Schedule{}, err
	}
	s.appendAudit("schedule_created", "schedule", input.ID, "owner", map[string]interface{}{"company_id": input.CompanyID, "target_agent_id": input.TargetAgentID})
	s.emitEvent(input.CompanyID, input.DepartmentID, input.TargetAgentID, "", "", "schedule_created", "info", map[string]interface{}{"schedule_id": input.ID})
	return s.GetSchedule(input.ID)
}

func (s *Service) GetSchedule(id string) (Schedule, error) {
	var sch Schedule
	var activeInt int
	var createdAt, updatedAt string
	err := s.db.QueryRow(`
		SELECT id, IFNULL(company_id,''), IFNULL(department_id,''), IFNULL(target_agent_id,''), IFNULL(schedule_type,'calendar'), IFNULL(cron_expr,''), IFNULL(rrule,''), IFNULL(start_at,''), IFNULL(next_run_at,''), IFNULL(timezone,'UTC'), IFNULL(payload_json,'{}'), IFNULL(is_active,1), IFNULL(created_at,''), IFNULL(updated_at,'')
		FROM schedules WHERE id = ?
	`, id).Scan(&sch.ID, &sch.CompanyID, &sch.DepartmentID, &sch.TargetAgentID, &sch.ScheduleType, &sch.CronExpr, &sch.RRule, &sch.StartAt, &sch.NextRunAt, &sch.Timezone, &sch.PayloadJSON, &activeInt, &createdAt, &updatedAt)
	if err != nil {
		return Schedule{}, err
	}
	sch.IsActive = activeInt == 1
	sch.CreatedAt = parseDBTime(createdAt)
	sch.UpdatedAt = parseDBTime(updatedAt)
	return sch, nil
}

func (s *Service) ListSchedules(companyID, departmentID string) ([]Schedule, error) {
	query := `
		SELECT id, IFNULL(company_id,''), IFNULL(department_id,''), IFNULL(target_agent_id,''), IFNULL(schedule_type,'calendar'), IFNULL(cron_expr,''), IFNULL(rrule,''), IFNULL(start_at,''), IFNULL(next_run_at,''), IFNULL(timezone,'UTC'), IFNULL(payload_json,'{}'), IFNULL(is_active,1), IFNULL(created_at,''), IFNULL(updated_at,'')
		FROM schedules WHERE 1=1`
	args := []interface{}{}
	if strings.TrimSpace(companyID) != "" {
		query += " AND company_id = ?"
		args = append(args, companyID)
	}
	if strings.TrimSpace(departmentID) != "" {
		query += " AND department_id = ?"
		args = append(args, departmentID)
	}
	query += " ORDER BY next_run_at ASC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Schedule{}
	for rows.Next() {
		var sch Schedule
		var activeInt int
		var createdAt, updatedAt string
		if err := rows.Scan(&sch.ID, &sch.CompanyID, &sch.DepartmentID, &sch.TargetAgentID, &sch.ScheduleType, &sch.CronExpr, &sch.RRule, &sch.StartAt, &sch.NextRunAt, &sch.Timezone, &sch.PayloadJSON, &activeInt, &createdAt, &updatedAt); err != nil {
			continue
		}
		sch.IsActive = activeInt == 1
		sch.CreatedAt = parseDBTime(createdAt)
		sch.UpdatedAt = parseDBTime(updatedAt)
		out = append(out, sch)
	}
	return out, nil
}

func (s *Service) UpdateSchedule(id string, patch Schedule) (Schedule, error) {
	cur, err := s.GetSchedule(id)
	if err != nil {
		return Schedule{}, err
	}
	if strings.TrimSpace(patch.ScheduleType) != "" {
		cur.ScheduleType = strings.ToLower(strings.TrimSpace(patch.ScheduleType))
	}
	if strings.TrimSpace(patch.CronExpr) != "" {
		cur.CronExpr = patch.CronExpr
	}
	if strings.TrimSpace(patch.RRule) != "" {
		cur.RRule = patch.RRule
	}
	if strings.TrimSpace(patch.StartAt) != "" {
		cur.StartAt = patch.StartAt
	}
	if strings.TrimSpace(patch.Timezone) != "" {
		cur.Timezone = patch.Timezone
	}
	if strings.TrimSpace(patch.PayloadJSON) != "" {
		cur.PayloadJSON = patch.PayloadJSON
	}

	loc, err := time.LoadLocation(cur.Timezone)
	if err != nil {
		loc = time.UTC
		cur.Timezone = "UTC"
	}
	now := time.Now().In(loc)
	start := parseAnyTime(cur.StartAt, now)
	next := start
	if cur.ScheduleType == "cron" {
		next = nextFromCron(cur.CronExpr, now, loc)
	} else {
		next = nextFromCalendar(cur.RRule, start, now)
	}

	_, err = s.db.Exec(`
		UPDATE schedules
		SET schedule_type = ?, cron_expr = ?, rrule = ?, start_at = ?, next_run_at = ?, timezone = ?, payload_json = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, cur.ScheduleType, cur.CronExpr, cur.RRule, start.UTC().Format(time.RFC3339), next.UTC().Format(time.RFC3339), cur.Timezone, cur.PayloadJSON, id)
	if err != nil {
		return Schedule{}, err
	}
	s.appendAudit("schedule_updated", "schedule", id, "owner", cur)
	return s.GetSchedule(id)
}

func (s *Service) ToggleSchedule(id string, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := s.db.Exec("UPDATE schedules SET is_active = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", v, id)
	if err == nil {
		s.appendAudit("schedule_toggled", "schedule", id, "owner", map[string]interface{}{"enabled": enabled})
	}
	return err
}

func (s *Service) runSchedules() {
	now := time.Now().UTC()
	rows, err := s.db.Query(`
		SELECT id, IFNULL(company_id,''), IFNULL(department_id,''), IFNULL(target_agent_id,''), IFNULL(schedule_type,'calendar'), IFNULL(cron_expr,''), IFNULL(rrule,''), IFNULL(start_at,''), IFNULL(next_run_at,''), IFNULL(timezone,'UTC'), IFNULL(payload_json,'{}'), IFNULL(is_active,1), IFNULL(created_at,''), IFNULL(updated_at,'')
		FROM schedules
		WHERE is_active = 1 AND next_run_at != '' AND next_run_at <= ?
		ORDER BY next_run_at ASC
		LIMIT 20
	`, now.Format(time.RFC3339))
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var sch Schedule
		var activeInt int
		var createdAt, updatedAt string
		if err := rows.Scan(&sch.ID, &sch.CompanyID, &sch.DepartmentID, &sch.TargetAgentID, &sch.ScheduleType, &sch.CronExpr, &sch.RRule, &sch.StartAt, &sch.NextRunAt, &sch.Timezone, &sch.PayloadJSON, &activeInt, &createdAt, &updatedAt); err != nil {
			continue
		}
		sch.IsActive = activeInt == 1

		payload := strings.TrimSpace(sch.PayloadJSON)
		if payload == "" {
			payload = `{"prompt":"Scheduled task"}`
		}
		task := AgentTask{
			ID:           uuid.NewString(),
			CompanyID:    sch.CompanyID,
			DepartmentID: sch.DepartmentID,
			AgentID:      sch.TargetAgentID,
			RequestedBy:  "scheduler",
			Type:         "scheduled",
			Status:       "queued",
			Priority:     40,
			InputJSON:    payload,
		}
		createdTask, err := s.CreateTask(task)
		if err != nil {
			_, _ = s.db.Exec(`INSERT INTO schedule_runs (id, schedule_id, task_id, status, triggered_at, finished_at, error) VALUES (?, ?, '', 'failed', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, ?)`, uuid.NewString(), sch.ID, err.Error())
			s.emitEvent(sch.CompanyID, sch.DepartmentID, sch.TargetAgentID, "", "", "schedule_failed", "error", map[string]interface{}{"schedule_id": sch.ID, "error": err.Error()})
			continue
		}
		_, _ = s.db.Exec(`INSERT INTO schedule_runs (id, schedule_id, task_id, status, triggered_at, finished_at, error) VALUES (?, ?, ?, 'triggered', CURRENT_TIMESTAMP, NULL, '')`, uuid.NewString(), sch.ID, createdTask.ID)

		if sch.ScheduleType == "once" {
			_, _ = s.db.Exec("UPDATE schedules SET is_active = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?", sch.ID)
			s.appendAudit("schedule_triggered", "schedule", sch.ID, "scheduler", map[string]interface{}{"task_id": createdTask.ID, "once": true})
		} else {
			loc, err := time.LoadLocation(sch.Timezone)
			if err != nil {
				loc = time.UTC
			}
			next := now.In(loc)
			if sch.ScheduleType == "cron" {
				next = nextFromCron(sch.CronExpr, now.In(loc), loc)
			} else {
				start := parseAnyTime(sch.StartAt, now.In(loc))
				next = nextFromCalendar(sch.RRule, start, now.In(loc))
			}
			_, _ = s.db.Exec("UPDATE schedules SET next_run_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", next.UTC().Format(time.RFC3339), sch.ID)
			s.appendAudit("schedule_triggered", "schedule", sch.ID, "scheduler", map[string]interface{}{"task_id": createdTask.ID, "next_run_at": next.UTC().Format(time.RFC3339)})
		}
		s.emitEvent(sch.CompanyID, sch.DepartmentID, sch.TargetAgentID, createdTask.ThreadID, createdTask.ID, "schedule_triggered", "info", map[string]interface{}{"schedule_id": sch.ID, "task_id": createdTask.ID})
	}
}
