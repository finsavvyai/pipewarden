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
