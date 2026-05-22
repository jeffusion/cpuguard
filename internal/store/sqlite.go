package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cpuguard/internal/model"
	_ "modernc.org/sqlite"
)

const sqliteTimeFormat = "2006-01-02T15:04:05.000000000Z07:00"

type SQLiteStore struct {
	db *sql.DB
	mu sync.Mutex
}

func Open(path string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	dbPath := fmt.Sprintf("file:%s?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	store := &SQLiteStore{db: db}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) init() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stmts := []string{
		`PRAGMA busy_timeout = 10000;`,
		`PRAGMA journal_mode = WAL;`,
		`PRAGMA synchronous = NORMAL;`,
		`CREATE TABLE IF NOT EXISTS subjects (
			subject_id TEXT PRIMARY KEY,
			rule_id TEXT NOT NULL,
			scope TEXT NOT NULL,
			target_ref TEXT NOT NULL,
			display_name TEXT NOT NULL,
			state TEXT NOT NULL,
			last_cpu_pct REAL NOT NULL DEFAULT 0,
			current_limit_pct REAL NOT NULL DEFAULT 0,
			original_cgroup_path TEXT NOT NULL DEFAULT '',
			managed_cgroup_path TEXT NOT NULL DEFAULT '',
			host_original_cgroups TEXT NOT NULL DEFAULT '',
			docker_container_id TEXT NOT NULL DEFAULT '',
			docker_original_cpu_quota INTEGER NOT NULL DEFAULT 0,
			docker_original_cpu_period INTEGER NOT NULL DEFAULT 0,
			cooldown_until TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			subject_id TEXT NOT NULL,
			rule_id TEXT NOT NULL,
			type TEXT NOT NULL,
			message TEXT NOT NULL,
			cpu_pct REAL NOT NULL DEFAULT 0,
			limit_pct REAL NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_events_created_at ON events(created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_events_subject_created_at ON events(subject_id, created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_events_type_created_at ON events(type, created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_subjects_updated_at ON subjects(updated_at);`,
		`CREATE INDEX IF NOT EXISTS idx_subjects_last_cpu_pct ON subjects(last_cpu_pct);`,
		`CREATE INDEX IF NOT EXISTS idx_subjects_current_limit_pct ON subjects(current_limit_pct);`,
		`CREATE INDEX IF NOT EXISTS idx_subjects_state ON subjects(state);`,
		`CREATE INDEX IF NOT EXISTS idx_subjects_scope ON subjects(scope);`,
		`CREATE INDEX IF NOT EXISTS idx_subjects_target_ref ON subjects(target_ref);`,
		`CREATE INDEX IF NOT EXISTS idx_subjects_rule_id ON subjects(rule_id);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return s.ensureColumns()
}

func (s *SQLiteStore) ensureColumns() error {
	columns, err := s.subjectColumns()
	if err != nil {
		return err
	}
	if !columns["host_original_cgroups"] {
		if _, err := s.db.Exec(`ALTER TABLE subjects ADD COLUMN host_original_cgroups TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) subjectColumns() (map[string]bool, error) {
	rows, err := s.db.Query(`PRAGMA table_info(subjects)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) UpsertSubject(subject model.Subject) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.upsertSubjectExec(s.db, subject)
}

func (s *SQLiteStore) UpsertSubjects(subjects []model.Subject) error {
	if len(subjects) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	for _, subject := range subjects {
		if err := s.upsertSubjectExec(tx, subject); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) DeleteStaleObservingSubjects(cutoff time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM subjects WHERE state = ? AND updated_at < ?`, string(model.StateObserving), formatTime(cutoff))
	return err
}

func (s *SQLiteStore) upsertSubjectExec(db execer, subject model.Subject) error {
	_, err := db.Exec(`INSERT INTO subjects (
			subject_id, rule_id, scope, target_ref, display_name, state, last_cpu_pct, current_limit_pct,
			original_cgroup_path, managed_cgroup_path, host_original_cgroups, docker_container_id, docker_original_cpu_quota,
			docker_original_cpu_period, cooldown_until, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(subject_id) DO UPDATE SET
			rule_id=excluded.rule_id,
			scope=excluded.scope,
			target_ref=excluded.target_ref,
			display_name=excluded.display_name,
			state=excluded.state,
			last_cpu_pct=excluded.last_cpu_pct,
			current_limit_pct=excluded.current_limit_pct,
			original_cgroup_path=excluded.original_cgroup_path,
			managed_cgroup_path=excluded.managed_cgroup_path,
			host_original_cgroups=excluded.host_original_cgroups,
			docker_container_id=excluded.docker_container_id,
			docker_original_cpu_quota=excluded.docker_original_cpu_quota,
			docker_original_cpu_period=excluded.docker_original_cpu_period,
			cooldown_until=excluded.cooldown_until,
			updated_at=excluded.updated_at`,
		subject.ID, subject.RuleID, string(subject.Scope), subject.TargetRef, subject.DisplayName,
		string(subject.State), subject.LastCPUPct, subject.CurrentLimitPct, subject.OriginalCgroupPath,
		subject.ManagedCgroupPath, subject.HostOriginalCgroups, subject.DockerContainerID, subject.DockerOriginalCPUQuota,
		subject.DockerOriginalCPUPeriod, formatTime(subject.CooldownUntil), formatTime(subject.UpdatedAt))
	return err
}

func (s *SQLiteStore) ListSubjects() ([]model.Subject, error) {
	rows, err := s.db.Query(`SELECT
			subject_id, rule_id, scope, target_ref, display_name, state, last_cpu_pct, current_limit_pct,
			original_cgroup_path, managed_cgroup_path, host_original_cgroups, docker_container_id, docker_original_cpu_quota,
			docker_original_cpu_period, cooldown_until, updated_at
		FROM subjects ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSubjects(rows)
}

func scanSubjects(rows *sql.Rows) ([]model.Subject, error) {
	result := []model.Subject{}
	for rows.Next() {
		var subject model.Subject
		var scope, state, cooldown, updated string
		if err := rows.Scan(
			&subject.ID, &subject.RuleID, &scope, &subject.TargetRef, &subject.DisplayName,
			&state, &subject.LastCPUPct, &subject.CurrentLimitPct, &subject.OriginalCgroupPath,
			&subject.ManagedCgroupPath, &subject.HostOriginalCgroups, &subject.DockerContainerID, &subject.DockerOriginalCPUQuota,
			&subject.DockerOriginalCPUPeriod, &cooldown, &updated,
		); err != nil {
			return nil, err
		}
		subject.Scope = model.Scope(scope)
		subject.State = model.SubjectState(state)
		subject.CooldownUntil = parseTime(cooldown)
		subject.UpdatedAt = parseTime(updated)
		result = append(result, subject)
	}
	return result, rows.Err()
}

func (s *SQLiteStore) QuerySubjects(query model.SubjectQuery) (model.SubjectPage, error) {
	if query.Limit <= 0 {
		query.Limit = 100
	}
	if query.Offset < 0 {
		query.Offset = 0
	}
	orderBy := subjectOrderBy(query.Sort, query.Dir)
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM subjects`).Scan(&total); err != nil {
		return model.SubjectPage{}, err
	}
	rows, err := s.db.Query(`SELECT
			subject_id, rule_id, scope, target_ref, display_name, state, last_cpu_pct, current_limit_pct,
			original_cgroup_path, managed_cgroup_path, host_original_cgroups, docker_container_id, docker_original_cpu_quota,
			docker_original_cpu_period, cooldown_until, updated_at
		FROM subjects ORDER BY `+orderBy+` LIMIT ? OFFSET ?`, query.Limit, query.Offset)
	if err != nil {
		return model.SubjectPage{}, err
	}
	defer rows.Close()
	items, err := scanSubjects(rows)
	if err != nil {
		return model.SubjectPage{}, err
	}
	page := model.SubjectPage{
		Total:         total,
		Offset:        query.Offset,
		Limit:         query.Limit,
		Items:         items,
		SelectedIndex: -1,
	}
	if query.SelectedID == "" {
		return page, nil
	}
	selected, err := s.GetSubject(query.SelectedID)
	if err != nil {
		return model.SubjectPage{}, err
	}
	if selected == nil {
		return page, nil
	}
	page.Selected = selected
	err = s.db.QueryRow(`SELECT rn FROM (
			SELECT subject_id, ROW_NUMBER() OVER (ORDER BY `+orderBy+`) - 1 AS rn
			FROM subjects
		) WHERE subject_id = ?`, query.SelectedID).Scan(&page.SelectedIndex)
	if err == sql.ErrNoRows {
		page.SelectedIndex = -1
		return page, nil
	}
	if err != nil {
		return model.SubjectPage{}, err
	}
	return page, nil
}

func (s *SQLiteStore) SubjectSummary() (model.SubjectSummary, error) {
	var summary model.SubjectSummary
	var maxCPU, avgCPU sql.NullFloat64
	err := s.db.QueryRow(`SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN state = ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN state = ? THEN 1 ELSE 0 END), 0),
			MAX(last_cpu_pct),
			AVG(last_cpu_pct)
		FROM subjects`, string(model.StateThrottled), string(model.StateManualHold)).Scan(
		&summary.Total,
		&summary.Throttled,
		&summary.Held,
		&maxCPU,
		&avgCPU,
	)
	if err != nil {
		return model.SubjectSummary{}, err
	}
	if maxCPU.Valid {
		summary.MaxCPUPct = maxCPU.Float64
	}
	if avgCPU.Valid {
		summary.AvgCPUPct = avgCPU.Float64
	}
	return summary, nil
}

func subjectOrderBy(sortKey string, dir string) string {
	direction := "DESC"
	if dir == "asc" {
		direction = "ASC"
	}
	tieBreak := "last_cpu_pct DESC, target_ref ASC, subject_id ASC"
	switch sortKey {
	case "state":
		return "CASE state WHEN 'throttled' THEN 0 WHEN 'manual_hold' THEN 1 ELSE 2 END " + direction + ", " + tieBreak
	case "limit":
		return "CASE WHEN state IN ('throttled', 'manual_hold') THEN current_limit_pct ELSE -1 END " + direction + ", " + tieBreak
	case "scope":
		return "scope " + direction + ", " + tieBreak
	case "target":
		return "target_ref " + direction + ", " + tieBreak
	case "rule":
		return "rule_id " + direction + ", " + tieBreak
	case "cpu":
		fallthrough
	default:
		return "last_cpu_pct " + direction + ", target_ref ASC, subject_id ASC"
	}
}

func (s *SQLiteStore) GetSubject(id string) (*model.Subject, error) {
	row := s.db.QueryRow(`SELECT
			subject_id, rule_id, scope, target_ref, display_name, state, last_cpu_pct, current_limit_pct,
			original_cgroup_path, managed_cgroup_path, host_original_cgroups, docker_container_id, docker_original_cpu_quota,
			docker_original_cpu_period, cooldown_until, updated_at
		FROM subjects WHERE subject_id = ?`, id)
	var subject model.Subject
	var scope, state, cooldown, updated string
	err := row.Scan(
		&subject.ID, &subject.RuleID, &scope, &subject.TargetRef, &subject.DisplayName,
		&state, &subject.LastCPUPct, &subject.CurrentLimitPct, &subject.OriginalCgroupPath,
		&subject.ManagedCgroupPath, &subject.HostOriginalCgroups, &subject.DockerContainerID, &subject.DockerOriginalCPUQuota,
		&subject.DockerOriginalCPUPeriod, &cooldown, &updated,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	subject.Scope = model.Scope(scope)
	subject.State = model.SubjectState(state)
	subject.CooldownUntil = parseTime(cooldown)
	subject.UpdatedAt = parseTime(updated)
	return &subject, nil
}

func (s *SQLiteStore) AddEvent(event model.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	_, err := s.db.Exec(`INSERT INTO events (
			subject_id, rule_id, type, message, cpu_pct, limit_pct, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		event.SubjectID, event.RuleID, event.Type, event.Message, event.CPUPct, event.LimitPct, formatTime(event.CreatedAt))
	return err
}

func (s *SQLiteStore) AddEventAndPrune(event model.Event, maxEvents int, maxAge time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO events (
			subject_id, rule_id, type, message, cpu_pct, limit_pct, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		event.SubjectID, event.RuleID, event.Type, event.Message, event.CPUPct, event.LimitPct, formatTime(event.CreatedAt)); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := pruneEventsTx(tx, maxEvents, maxAge); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) PruneEvents(maxEvents int, maxAge time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pruneEventsLocked(maxEvents, maxAge)
}

func (s *SQLiteStore) pruneEventsLocked(maxEvents int, maxAge time.Duration) error {
	return pruneEventsExec(s.db, maxEvents, maxAge)
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func pruneEventsTx(tx *sql.Tx, maxEvents int, maxAge time.Duration) error {
	return pruneEventsExec(tx, maxEvents, maxAge)
}

func pruneEventsExec(db execer, maxEvents int, maxAge time.Duration) error {
	if maxAge > 0 {
		cutoff := time.Now().Add(-maxAge)
		if _, err := db.Exec(`DELETE FROM events WHERE created_at < ?`, formatTime(cutoff)); err != nil {
			return err
		}
	}
	if maxEvents > 0 {
		_, err := db.Exec(`DELETE FROM events
			WHERE id NOT IN (
				SELECT id FROM events ORDER BY id DESC LIMIT ?
			)`, maxEvents)
		return err
	}
	return nil
}

func (s *SQLiteStore) ListEvents(limit int) ([]model.Event, error) {
	return s.QueryEvents(model.EventQuery{Limit: limit})
}

func (s *SQLiteStore) ListEventsForSubject(subjectID string, limit int) ([]model.Event, error) {
	return s.QueryEvents(model.EventQuery{SubjectID: subjectID, Limit: limit})
}

func (s *SQLiteStore) QueryEvents(filter model.EventQuery) ([]model.Event, error) {
	if filter.Limit <= 0 {
		filter.Limit = 100
	}
	sqlQuery := `SELECT id, subject_id, rule_id, type, message, cpu_pct, limit_pct, created_at
		FROM events`
	args := []any{}
	conditions := []string{}
	if filter.SubjectID != "" {
		conditions = append(conditions, `subject_id = ?`)
		args = append(args, filter.SubjectID)
	}
	if filter.Type != "" {
		conditions = append(conditions, `type = ?`)
		args = append(args, filter.Type)
	}
	if !filter.Since.IsZero() {
		conditions = append(conditions, `created_at >= ?`)
		args = append(args, formatTime(filter.Since))
	}
	if len(conditions) > 0 {
		sqlQuery += ` WHERE ` + strings.Join(conditions, ` AND `)
	}
	sqlQuery += ` ORDER BY id DESC LIMIT ?`
	args = append(args, filter.Limit)
	rows, err := s.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []model.Event{}
	for rows.Next() {
		var event model.Event
		var created string
		if err := rows.Scan(&event.ID, &event.SubjectID, &event.RuleID, &event.Type, &event.Message, &event.CPUPct, &event.LimitPct, &created); err != nil {
			return nil, err
		}
		event.CreatedAt = parseTime(created)
		result = append(result, event)
	}
	return result, rows.Err()
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(sqliteTimeFormat)
}

func parseTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func (s *SQLiteStore) String() string {
	return fmt.Sprintf("sqlite_store(%p)", s)
}
