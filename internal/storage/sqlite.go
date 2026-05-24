package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BUAASubnet/UBAA.Server/internal/dto"
	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	*sql.DB
}

func OpenSQLite(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?_busy_timeout=5000&_foreign_keys=on", path))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &DB{DB: db}
	if err := store.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (db *DB) migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			username TEXT PRIMARY KEY,
			user_data TEXT NOT NULL,
			authenticated_at TEXT NOT NULL,
			last_activity TEXT NOT NULL,
			portal_type TEXT NOT NULL DEFAULT 'UNKNOWN',
			generation INTEGER NOT NULL DEFAULT 1,
			revision INTEGER NOT NULL DEFAULT 1
		)`,
		`CREATE TABLE IF NOT EXISTS refresh_tokens (
			username TEXT PRIMARY KEY,
			token_hash TEXT NOT NULL UNIQUE,
			issued_at TEXT NOT NULL,
			expires_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS prelogin_sessions (
			client_id TEXT PRIMARY KEY,
			touched_at TEXT NOT NULL,
			metadata TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS login_stats (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL,
			success_mode TEXT NOT NULL,
			connection_mode TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS cookies (
			subject TEXT NOT NULL,
			host TEXT NOT NULL,
			path TEXT NOT NULL,
			name TEXT NOT NULL,
			value TEXT NOT NULL,
			raw TEXT,
			expires_at TEXT,
			secure INTEGER NOT NULL DEFAULT 0,
			http_only INTEGER NOT NULL DEFAULT 0,
			same_site TEXT,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(subject, host, path, name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_refresh_tokens_hash ON refresh_tokens(token_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_login_stats_user_time ON login_stats(username, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_cookies_subject ON cookies(subject)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

type SessionRecord struct {
	Username        string       `json:"username"`
	UserData        dto.UserData `json:"userData"`
	AuthenticatedAt time.Time    `json:"authenticatedAt"`
	LastActivity    time.Time    `json:"lastActivity"`
	PortalType      string       `json:"portalType"`
	Generation      int64        `json:"generation"`
	Revision        int64        `json:"revision"`
}

func (db *DB) SaveSession(ctx context.Context, record SessionRecord) error {
	raw, err := json.Marshal(record.UserData)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO sessions (
		username, user_data, authenticated_at, last_activity, portal_type, generation, revision
	) VALUES (?, ?, ?, ?, ?, 1, 1)
	ON CONFLICT(username) DO UPDATE SET
		user_data = excluded.user_data,
		authenticated_at = excluded.authenticated_at,
		last_activity = excluded.last_activity,
		portal_type = excluded.portal_type,
		generation = sessions.generation + 1,
		revision = sessions.revision + 1`,
		record.Username,
		string(raw),
		record.AuthenticatedAt.Format(time.RFC3339Nano),
		record.LastActivity.Format(time.RFC3339Nano),
		blankDefault(record.PortalType, "UNKNOWN"),
	)
	return err
}

func (db *DB) LoadSession(ctx context.Context, username string) (*SessionRecord, error) {
	var rawUser, authenticatedAt, lastActivity string
	record := SessionRecord{Username: username}
	err := db.QueryRowContext(ctx, `SELECT user_data, authenticated_at, last_activity, portal_type, generation, revision
		FROM sessions WHERE username = ?`, username).
		Scan(&rawUser, &authenticatedAt, &lastActivity, &record.PortalType, &record.Generation, &record.Revision)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(rawUser), &record.UserData); err != nil {
		return nil, err
	}
	record.AuthenticatedAt, err = time.Parse(time.RFC3339Nano, authenticatedAt)
	if err != nil {
		return nil, err
	}
	record.LastActivity, err = time.Parse(time.RFC3339Nano, lastActivity)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (db *DB) TouchSession(ctx context.Context, username string, at time.Time) error {
	_, err := db.ExecContext(ctx, `UPDATE sessions SET last_activity = ?, revision = revision + 1 WHERE username = ?`,
		at.Format(time.RFC3339Nano), username)
	return err
}

func (db *DB) DeleteSession(ctx context.Context, username string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE username = ?`, username)
	return err
}

func (db *DB) DeleteExpiredSessions(ctx context.Context, cutoff time.Time) (int64, error) {
	result, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE last_activity < ?`, cutoff.Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (db *DB) CountActiveSessions(ctx context.Context, cutoff time.Time) (int, error) {
	return db.countWhere(ctx, `SELECT COUNT(*) FROM sessions WHERE last_activity >= ?`, cutoff.Format(time.RFC3339Nano))
}

type RefreshTokenRecord struct {
	Username  string
	TokenHash string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

func (db *DB) SaveRefreshToken(ctx context.Context, record RefreshTokenRecord) error {
	_, err := db.ExecContext(ctx, `INSERT INTO refresh_tokens (username, token_hash, issued_at, expires_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(username) DO UPDATE SET
			token_hash = excluded.token_hash,
			issued_at = excluded.issued_at,
			expires_at = excluded.expires_at`,
		record.Username,
		record.TokenHash,
		record.IssuedAt.Format(time.RFC3339Nano),
		record.ExpiresAt.Format(time.RFC3339Nano),
	)
	return err
}

func (db *DB) FindRefreshToken(ctx context.Context, tokenHash string) (*RefreshTokenRecord, error) {
	var issuedAt, expiresAt string
	record := RefreshTokenRecord{TokenHash: tokenHash}
	err := db.QueryRowContext(ctx, `SELECT username, issued_at, expires_at FROM refresh_tokens WHERE token_hash = ?`, tokenHash).
		Scan(&record.Username, &issuedAt, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var parseErr error
	record.IssuedAt, parseErr = time.Parse(time.RFC3339Nano, issuedAt)
	if parseErr != nil {
		return nil, parseErr
	}
	record.ExpiresAt, parseErr = time.Parse(time.RFC3339Nano, expiresAt)
	if parseErr != nil {
		return nil, parseErr
	}
	return &record, nil
}

func (db *DB) DeleteRefreshTokenByUsername(ctx context.Context, username string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE username = ?`, username)
	return err
}

func (db *DB) DeleteExpiredRefreshTokens(ctx context.Context, now time.Time) (int64, error) {
	result, err := db.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE expires_at < ?`, now.Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (db *DB) DeleteExpiredPreLoginSessions(ctx context.Context, cutoff time.Time) (int64, error) {
	result, err := db.ExecContext(ctx, `DELETE FROM prelogin_sessions WHERE touched_at < ?`, cutoff.Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (db *DB) CountActivePreLoginSessions(ctx context.Context, cutoff time.Time) (int, error) {
	return db.countWhere(ctx, `SELECT COUNT(*) FROM prelogin_sessions WHERE touched_at >= ?`, cutoff.Format(time.RFC3339Nano))
}

func (db *DB) SaveLoginStat(ctx context.Context, username, successMode, connectionMode string, at time.Time) error {
	_, err := db.ExecContext(ctx, `INSERT INTO login_stats (username, success_mode, connection_mode, created_at)
		VALUES (?, ?, ?, ?)`, username, successMode, connectionMode, at.Format(time.RFC3339Nano))
	return err
}

type CookieRecord struct {
	Subject  string
	Host     string
	Path     string
	Name     string
	Value    string
	Raw      string
	Expires  *time.Time
	Secure   bool
	HTTPOnly bool
	SameSite string
	Updated  time.Time
}

func (db *DB) SaveCookie(ctx context.Context, record CookieRecord) error {
	expires := sql.NullString{}
	if record.Expires != nil && !record.Expires.IsZero() {
		expires.Valid = true
		expires.String = record.Expires.Format(time.RFC3339Nano)
	}
	_, err := db.ExecContext(ctx, `INSERT INTO cookies (
			subject, host, path, name, value, raw, expires_at, secure, http_only, same_site, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(subject, host, path, name) DO UPDATE SET
			value = excluded.value,
			raw = excluded.raw,
			expires_at = excluded.expires_at,
			secure = excluded.secure,
			http_only = excluded.http_only,
			same_site = excluded.same_site,
			updated_at = excluded.updated_at`,
		record.Subject,
		record.Host,
		record.Path,
		record.Name,
		record.Value,
		record.Raw,
		expires,
		boolInt(record.Secure),
		boolInt(record.HTTPOnly),
		record.SameSite,
		record.Updated.Format(time.RFC3339Nano),
	)
	return err
}

func (db *DB) LoadCookies(ctx context.Context, subject string) ([]CookieRecord, error) {
	rows, err := db.QueryContext(ctx, `SELECT host, path, name, value, raw, expires_at, secure, http_only, same_site, updated_at
		FROM cookies WHERE subject = ?`, subject)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []CookieRecord
	for rows.Next() {
		var expires sql.NullString
		var secure, httpOnly int
		var updated string
		record := CookieRecord{Subject: subject}
		if err := rows.Scan(&record.Host, &record.Path, &record.Name, &record.Value, &record.Raw, &expires, &secure, &httpOnly, &record.SameSite, &updated); err != nil {
			return nil, err
		}
		if expires.Valid {
			parsed, err := time.Parse(time.RFC3339Nano, expires.String)
			if err == nil {
				record.Expires = &parsed
			}
		}
		record.Secure = secure == 1
		record.HTTPOnly = httpOnly == 1
		record.Updated, _ = time.Parse(time.RFC3339Nano, updated)
		records = append(records, record)
	}
	return records, rows.Err()
}

func (db *DB) DeleteCookie(ctx context.Context, subject, host, path, name string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM cookies WHERE subject = ? AND host = ? AND path = ? AND name = ?`,
		subject, host, path, name)
	return err
}

func (db *DB) DeleteCookiesBySubject(ctx context.Context, subject string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM cookies WHERE subject = ?`, subject)
	return err
}

func (db *DB) MigrateCookiesSubject(ctx context.Context, from, to string) error {
	_, err := db.ExecContext(ctx, `UPDATE OR REPLACE cookies SET subject = ?, updated_at = ? WHERE subject = ?`,
		to, time.Now().Format(time.RFC3339Nano), from)
	return err
}

func blankDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (db *DB) countWhere(ctx context.Context, query string, args ...any) (int, error) {
	var count int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
