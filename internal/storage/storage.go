package storage

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// ConnectionRecord is a persisted connection configuration.
type ConnectionRecord struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Platform    string    `json:"platform"`
	Token       string    `json:"-"`
	Username    string    `json:"-"`
	AppPassword string    `json:"-"`
	BaseURL     string    `json:"base_url"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// DB wraps a SQLite database for connection persistence.
type DB struct {
	db *sql.DB
}

// New opens (or creates) a SQLite database at the given path.
func New(dbPath string) (*DB, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	s := &DB{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return s, nil
}

func (s *DB) migrate() error {
	query := `
	CREATE TABLE IF NOT EXISTS connections (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		name         TEXT NOT NULL UNIQUE,
		platform     TEXT NOT NULL,
		token        TEXT NOT NULL DEFAULT '',
		username     TEXT NOT NULL DEFAULT '',
		app_password TEXT NOT NULL DEFAULT '',
		base_url     TEXT NOT NULL DEFAULT '',
		created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_connections_platform ON connections(platform);
	CREATE INDEX IF NOT EXISTS idx_connections_name ON connections(name);

	CREATE TABLE IF NOT EXISTS security_findings (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		connection_name TEXT NOT NULL,
		run_id          TEXT NOT NULL,
		severity        TEXT NOT NULL,
		category        TEXT NOT NULL,
		title           TEXT NOT NULL,
		description     TEXT NOT NULL,
		remediation     TEXT NOT NULL DEFAULT '',
		file            TEXT NOT NULL DEFAULT '',
		line            INTEGER NOT NULL DEFAULT 0,
		confidence      REAL NOT NULL DEFAULT 0.0,
		false_positive  INTEGER NOT NULL DEFAULT 0,
		status          TEXT NOT NULL DEFAULT 'open',
		created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_findings_connection ON security_findings(connection_name);
	CREATE INDEX IF NOT EXISTS idx_findings_run ON security_findings(run_id);
	CREATE INDEX IF NOT EXISTS idx_findings_severity ON security_findings(severity);

	CREATE TABLE IF NOT EXISTS analysis_history (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		connection_name TEXT NOT NULL,
		run_id          TEXT NOT NULL,
		summary         TEXT NOT NULL,
		risk_score      INTEGER NOT NULL DEFAULT 0,
		findings_count  INTEGER NOT NULL DEFAULT 0,
		tokens_used     INTEGER NOT NULL DEFAULT 0,
		model           TEXT NOT NULL,
		duration_ms     INTEGER NOT NULL DEFAULT 0,
		analyzed_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_history_connection ON analysis_history(connection_name);
	`
	_, err := s.db.Exec(query)
	return err
}

// Create inserts a new connection record.
func (s *DB) Create(rec *ConnectionRecord) error {
	now := time.Now().UTC()
	result, err := s.db.Exec(
		`INSERT INTO connections (name, platform, token, username, app_password, base_url, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.Name, rec.Platform, rec.Token, rec.Username, rec.AppPassword, rec.BaseURL, now, now,
	)
	if err != nil {
		return fmt.Errorf("failed to insert connection: %w", err)
	}
	id, _ := result.LastInsertId()
	rec.ID = id
	rec.CreatedAt = now
	rec.UpdatedAt = now
	return nil
}

// GetByName retrieves a connection by its unique name.
func (s *DB) GetByName(name string) (*ConnectionRecord, error) {
	row := s.db.QueryRow(
		`SELECT id, name, platform, token, username, app_password, base_url, created_at, updated_at
		 FROM connections WHERE name = ?`, name,
	)
	return scanRow(row)
}

// List returns all connection records, ordered by creation time.
func (s *DB) List() ([]ConnectionRecord, error) {
	rows, err := s.db.Query(
		`SELECT id, name, platform, token, username, app_password, base_url, created_at, updated_at
		 FROM connections ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list connections: %w", err)
	}
	defer rows.Close()

	var records []ConnectionRecord
	for rows.Next() {
		var r ConnectionRecord
		if err := rows.Scan(&r.ID, &r.Name, &r.Platform, &r.Token, &r.Username, &r.AppPassword, &r.BaseURL, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// ListByPlatform returns connections filtered by platform.
func (s *DB) ListByPlatform(platform string) ([]ConnectionRecord, error) {
	rows, err := s.db.Query(
		`SELECT id, name, platform, token, username, app_password, base_url, created_at, updated_at
		 FROM connections WHERE platform = ? ORDER BY created_at ASC`, platform,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list connections: %w", err)
	}
	defer rows.Close()

	var records []ConnectionRecord
	for rows.Next() {
		var r ConnectionRecord
		if err := rows.Scan(&r.ID, &r.Name, &r.Platform, &r.Token, &r.Username, &r.AppPassword, &r.BaseURL, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// Update modifies an existing connection by name.
func (s *DB) Update(rec *ConnectionRecord) error {
	now := time.Now().UTC()
	result, err := s.db.Exec(
		`UPDATE connections SET platform=?, token=?, username=?, app_password=?, base_url=?, updated_at=?
		 WHERE name=?`,
		rec.Platform, rec.Token, rec.Username, rec.AppPassword, rec.BaseURL, now, rec.Name,
	)
	if err != nil {
		return fmt.Errorf("failed to update connection: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("connection %q not found", rec.Name)
	}
	rec.UpdatedAt = now
	return nil
}

// Delete removes a connection by name.
func (s *DB) Delete(name string) error {
	result, err := s.db.Exec(`DELETE FROM connections WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("failed to delete connection: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("connection %q not found", name)
	}
	return nil
}

// Count returns the total number of stored connections.
func (s *DB) Count() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM connections`).Scan(&count)
	return count, err
}

// FindingRecord represents a persisted security finding.
type FindingRecord struct {
	ID             int64     `json:"id"`
	ConnectionName string    `json:"connection_name"`
	RunID          string    `json:"run_id"`
	Severity       string    `json:"severity"`
	Category       string    `json:"category"`
	Title          string    `json:"title"`
	Description    string    `json:"description"`
	Remediation    string    `json:"remediation"`
	File           string    `json:"file,omitempty"`
	Line           int       `json:"line,omitempty"`
	Confidence     float64   `json:"confidence"`
	FalsePositive  bool      `json:"false_positive"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

// AnalysisRecord represents a persisted analysis run.
type AnalysisRecord struct {
	ID             int64     `json:"id"`
	ConnectionName string    `json:"connection_name"`
	RunID          string    `json:"run_id"`
	Summary        string    `json:"summary"`
	RiskScore      int       `json:"risk_score"`
	FindingsCount  int       `json:"findings_count"`
	TokensUsed     int       `json:"tokens_used"`
	Model          string    `json:"model"`
	DurationMS     int64     `json:"duration_ms"`
	AnalyzedAt     time.Time `json:"analyzed_at"`
}

// CreateFinding inserts a security finding.
func (s *DB) CreateFinding(f *FindingRecord) error {
	falsePos := 0
	if f.FalsePositive {
		falsePos = 1
	}
	result, err := s.db.Exec(
		`INSERT INTO security_findings (connection_name, run_id, severity, category, title, description, remediation, file, line, confidence, false_positive, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ConnectionName, f.RunID, f.Severity, f.Category, f.Title, f.Description, f.Remediation, f.File, f.Line, f.Confidence, falsePos, f.Status, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("failed to insert finding: %w", err)
	}
	f.ID, _ = result.LastInsertId()
	return nil
}

// ListFindings returns findings, optionally filtered by connection name.
func (s *DB) ListFindings(connectionName string) ([]FindingRecord, error) {
	var rows *sql.Rows
	var err error
	if connectionName != "" {
		rows, err = s.db.Query(
			`SELECT id, connection_name, run_id, severity, category, title, description, remediation, file, line, confidence, false_positive, status, created_at
			 FROM security_findings WHERE connection_name = ? ORDER BY created_at DESC`, connectionName)
	} else {
		rows, err = s.db.Query(
			`SELECT id, connection_name, run_id, severity, category, title, description, remediation, file, line, confidence, false_positive, status, created_at
			 FROM security_findings ORDER BY created_at DESC`)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list findings: %w", err)
	}
	defer rows.Close()

	var findings []FindingRecord
	for rows.Next() {
		var f FindingRecord
		var falsePos int
		if err := rows.Scan(&f.ID, &f.ConnectionName, &f.RunID, &f.Severity, &f.Category, &f.Title, &f.Description, &f.Remediation, &f.File, &f.Line, &f.Confidence, &falsePos, &f.Status, &f.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan finding: %w", err)
		}
		f.FalsePositive = falsePos != 0
		findings = append(findings, f)
	}
	return findings, rows.Err()
}

// CreateAnalysisRecord inserts an analysis history entry.
func (s *DB) CreateAnalysisRecord(r *AnalysisRecord) error {
	result, err := s.db.Exec(
		`INSERT INTO analysis_history (connection_name, run_id, summary, risk_score, findings_count, tokens_used, model, duration_ms, analyzed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ConnectionName, r.RunID, r.Summary, r.RiskScore, r.FindingsCount, r.TokensUsed, r.Model, r.DurationMS, r.AnalyzedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert analysis record: %w", err)
	}
	r.ID, _ = result.LastInsertId()
	return nil
}

// ListAnalysisHistory returns analysis history, optionally filtered by connection.
func (s *DB) ListAnalysisHistory(connectionName string) ([]AnalysisRecord, error) {
	var rows *sql.Rows
	var err error
	if connectionName != "" {
		rows, err = s.db.Query(
			`SELECT id, connection_name, run_id, summary, risk_score, findings_count, tokens_used, model, duration_ms, analyzed_at
			 FROM analysis_history WHERE connection_name = ? ORDER BY analyzed_at DESC`, connectionName)
	} else {
		rows, err = s.db.Query(
			`SELECT id, connection_name, run_id, summary, risk_score, findings_count, tokens_used, model, duration_ms, analyzed_at
			 FROM analysis_history ORDER BY analyzed_at DESC`)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list analysis history: %w", err)
	}
	defer rows.Close()

	var records []AnalysisRecord
	for rows.Next() {
		var r AnalysisRecord
		if err := rows.Scan(&r.ID, &r.ConnectionName, &r.RunID, &r.Summary, &r.RiskScore, &r.FindingsCount, &r.TokensUsed, &r.Model, &r.DurationMS, &r.AnalyzedAt); err != nil {
			return nil, fmt.Errorf("failed to scan analysis record: %w", err)
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// Close closes the database.
func (s *DB) Close() error {
	return s.db.Close()
}

func scanRow(row *sql.Row) (*ConnectionRecord, error) {
	var r ConnectionRecord
	if err := row.Scan(&r.ID, &r.Name, &r.Platform, &r.Token, &r.Username, &r.AppPassword, &r.BaseURL, &r.CreatedAt, &r.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("connection not found")
		}
		return nil, fmt.Errorf("failed to scan connection: %w", err)
	}
	return &r, nil
}
