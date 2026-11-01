// Package store keeps organisations, API keys, connected accounts and posts
// in one SQLite file. Writes are serialised through a single connection;
// no transaction is ever held across a network call.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS organizations (
  id TEXT PRIMARY KEY, name TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS api_keys (
  hash TEXT PRIMARY KEY, org_id TEXT NOT NULL REFERENCES organizations(id),
  created_at TEXT NOT NULL, revoked_at TEXT);
CREATE TABLE IF NOT EXISTS connect_sessions (
  id TEXT PRIMARY KEY, org_id TEXT NOT NULL REFERENCES organizations(id), platform TEXT NOT NULL,
  lookup TEXT UNIQUE, secret TEXT, created_at TEXT NOT NULL, used_at TEXT);
CREATE TABLE IF NOT EXISTS accounts (
  id TEXT PRIMARY KEY, org_id TEXT NOT NULL REFERENCES organizations(id), platform TEXT NOT NULL,
  external_id TEXT NOT NULL, handle TEXT NOT NULL, status TEXT NOT NULL, token TEXT,
  token_expires_at TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  UNIQUE (org_id, platform, external_id));
CREATE TABLE IF NOT EXISTS posts (
  id TEXT PRIMARY KEY, org_id TEXT NOT NULL REFERENCES organizations(id), account_id TEXT NOT NULL REFERENCES accounts(id),
  platform TEXT NOT NULL, idem_key TEXT, body_hash TEXT NOT NULL, text TEXT NOT NULL, title TEXT,
  scheduled_at TEXT, status TEXT NOT NULL, url TEXT, external_ids TEXT NOT NULL DEFAULT '[]',
  error_code TEXT, error_message TEXT, attempts INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL, published_at TEXT,
  UNIQUE (org_id, idem_key));
CREATE INDEX IF NOT EXISTS posts_due ON posts (status, scheduled_at);
`

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("migrating: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func parseTime(v sql.NullString) *time.Time {
	if !v.Valid || v.String == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, v.String)
	if err != nil {
		return nil
	}
	return &t
}

func ns(v string) sql.NullString { return sql.NullString{String: v, Valid: v != ""} }

// ───────── organisations and keys ─────────

type Org struct{ ID, Name string }

// CreateOrgWithKey creates an organisation and returns its first API key (shown once).
func (s *Store) CreateOrgWithKey(ctx context.Context, name string) (Org, string, error) {
	org := Org{ID: RandomID("org_"), Name: name}
	key := NewAPIKey()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Org{}, "", err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO organizations (id, name, created_at) VALUES (?, ?, ?)`, org.ID, name, now()); err != nil {
		return Org{}, "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO api_keys (hash, org_id, created_at) VALUES (?, ?, ?)`, HashKey(key), org.ID, now()); err != nil {
		return Org{}, "", err
	}
	return org, key, tx.Commit()
}

func (s *Store) OrgByKey(ctx context.Context, key string) (Org, error) {
	var o Org
	err := s.db.QueryRowContext(ctx, `SELECT o.id, o.name FROM api_keys k JOIN organizations o ON o.id = k.org_id
		WHERE k.hash = ? AND k.revoked_at IS NULL`, HashKey(key)).Scan(&o.ID, &o.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return Org{}, ErrNotFound
	}
	return o, err
}

// ───────── connect sessions ─────────

type ConnectSession struct {
	ID, OrgID, Platform, Secret string
	CreatedAt                   time.Time
	Used                        bool
}

func (s *Store) CreateConnectSession(ctx context.Context, orgID, platform string) (ConnectSession, error) {
	cs := ConnectSession{ID: RandomID("cs_"), OrgID: orgID, Platform: platform, CreatedAt: time.Now().UTC()}
	_, err := s.db.ExecContext(ctx, `INSERT INTO connect_sessions (id, org_id, platform, created_at) VALUES (?, ?, ?, ?)`,
		cs.ID, orgID, platform, cs.CreatedAt.Format(time.RFC3339Nano))
	return cs, err
}

func (s *Store) scanSession(row *sql.Row) (ConnectSession, error) {
	var cs ConnectSession
	var secret, created, used sql.NullString
	err := row.Scan(&cs.ID, &cs.OrgID, &cs.Platform, &secret, &created, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return cs, ErrNotFound
	}
	if err != nil {
		return cs, err
	}
	cs.Secret, cs.Used = secret.String, used.Valid
	if t := parseTime(created); t != nil {
		cs.CreatedAt = *t
	}
	return cs, nil
}

func (s *Store) ConnectSession(ctx context.Context, id string) (ConnectSession, error) {
	return s.scanSession(s.db.QueryRowContext(ctx, `SELECT id, org_id, platform, secret, created_at, used_at FROM connect_sessions WHERE id = ?`, id))
}

func (s *Store) ConnectSessionByLookup(ctx context.Context, lookup string) (ConnectSession, error) {
	return s.scanSession(s.db.QueryRowContext(ctx, `SELECT id, org_id, platform, secret, created_at, used_at FROM connect_sessions WHERE lookup = ?`, lookup))
}

// SetSessionLookup records how the provider callback will identify the session.
func (s *Store) SetSessionLookup(ctx context.Context, id, lookup, sealedSecret string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE connect_sessions SET lookup = ?, secret = ? WHERE id = ? AND used_at IS NULL`, lookup, ns(sealedSecret), id)
	return err
}

// UseSession marks the session consumed; false if it was already used.
func (s *Store) UseSession(ctx context.Context, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE connect_sessions SET used_at = ?, secret = NULL WHERE id = ? AND used_at IS NULL`, now(), id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// ───────── accounts ─────────

type Account struct {
	ID, OrgID, Platform, ExternalID, Handle, Status, SealedToken string
	TokenExpiresAt                                               *time.Time
}

const accountCols = `id, org_id, platform, external_id, handle, status, token, token_expires_at`

func scanAccount(sc interface{ Scan(...any) error }) (Account, error) {
	var a Account
	var token, exp sql.NullString
	if err := sc.Scan(&a.ID, &a.OrgID, &a.Platform, &a.ExternalID, &a.Handle, &a.Status, &token, &exp); err != nil {
		return a, err
	}
	a.SealedToken, a.TokenExpiresAt = token.String, parseTime(exp)
	return a, nil
}

// UpsertAccount stores (or reconnects) an account and returns it.
func (s *Store) UpsertAccount(ctx context.Context, orgID, platform, externalID, handle, sealedToken string, expiresAt time.Time) (Account, error) {
	exp := ""
	if !expiresAt.IsZero() {
		exp = expiresAt.UTC().Format(time.RFC3339Nano)
	}
	t := now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO accounts (id, org_id, platform, external_id, handle, status, token, token_expires_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'active', ?, ?, ?, ?)
		ON CONFLICT (org_id, platform, external_id) DO UPDATE SET handle = excluded.handle, status = 'active',
		token = excluded.token, token_expires_at = excluded.token_expires_at, updated_at = excluded.updated_at`,
		RandomID("acc_"), orgID, platform, externalID, handle, sealedToken, ns(exp), t, t)
	if err != nil {
		return Account{}, err
	}
	return scanAccount(s.db.QueryRowContext(ctx, `SELECT `+accountCols+` FROM accounts WHERE org_id = ? AND platform = ? AND external_id = ?`, orgID, platform, externalID))
}

func (s *Store) Account(ctx context.Context, orgID, id string) (Account, error) {
	a, err := scanAccount(s.db.QueryRowContext(ctx, `SELECT `+accountCols+` FROM accounts WHERE org_id = ? AND id = ?`, orgID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

func (s *Store) Accounts(ctx context.Context, orgID string) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+accountCols+` FROM accounts WHERE org_id = ? ORDER BY created_at`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetAccountStatus changes status; "revoked" also wipes the token.
func (s *Store) SetAccountStatus(ctx context.Context, id, status string) error {
	q := `UPDATE accounts SET status = ?, updated_at = ? WHERE id = ?`
	if status == "revoked" {
		q = `UPDATE accounts SET status = ?, updated_at = ?, token = NULL, token_expires_at = NULL WHERE id = ?`
	}
	_, err := s.db.ExecContext(ctx, q, status, now(), id)
	return err
}

// ───────── posts ─────────

type Post struct {
	ID, OrgID, AccountID, Platform, IdemKey, BodyHash, Text, Title string
	ScheduledAt, CreatedAt, PublishedAt                            *time.Time
	Status, URL, ErrorCode, ErrorMessage                           string
	ExternalIDs                                                    []string
	Attempts                                                       int
}

const postCols = `id, org_id, account_id, platform, idem_key, body_hash, text, title, scheduled_at, status, url,
	external_ids, error_code, error_message, attempts, created_at, published_at`

func scanPost(sc interface{ Scan(...any) error }) (Post, error) {
	var p Post
	var idem, title, sched, url, ext, code, msg, created, pub sql.NullString
	if err := sc.Scan(&p.ID, &p.OrgID, &p.AccountID, &p.Platform, &idem, &p.BodyHash, &p.Text, &title, &sched, &p.Status,
		&url, &ext, &code, &msg, &p.Attempts, &created, &pub); err != nil {
		return p, err
	}
	p.IdemKey, p.Title, p.URL, p.ErrorCode, p.ErrorMessage = idem.String, title.String, url.String, code.String, msg.String
	p.ScheduledAt, p.CreatedAt, p.PublishedAt = parseTime(sched), parseTime(created), parseTime(pub)
	_ = json.Unmarshal([]byte(ext.String), &p.ExternalIDs)
	return p, nil
}

// CreatePost inserts a post, or returns the existing one for the same
// idempotency key (existing=true).
func (s *Store) CreatePost(ctx context.Context, p Post) (Post, bool, error) {
	if p.IdemKey != "" {
		if existing, err := s.PostByIdemKey(ctx, p.OrgID, p.IdemKey); err == nil {
			return existing, true, nil
		} else if !errors.Is(err, ErrNotFound) {
			return Post{}, false, err
		}
	}
	p.ID = RandomID("post_")
	sched := ""
	if p.ScheduledAt != nil {
		sched = p.ScheduledAt.UTC().Format(time.RFC3339Nano)
	}
	t := now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO posts (id, org_id, account_id, platform, idem_key, body_hash, text, title, scheduled_at, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.OrgID, p.AccountID, p.Platform, ns(p.IdemKey), p.BodyHash, p.Text, ns(p.Title), ns(sched), p.Status, t, t)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") && p.IdemKey != "" {
		existing, err2 := s.PostByIdemKey(ctx, p.OrgID, p.IdemKey)
		return existing, true, err2
	}
	if err != nil {
		return Post{}, false, err
	}
	created, err := s.Post(ctx, p.OrgID, p.ID)
	return created, false, err
}

func (s *Store) Post(ctx context.Context, orgID, id string) (Post, error) {
	p, err := scanPost(s.db.QueryRowContext(ctx, `SELECT `+postCols+` FROM posts WHERE org_id = ? AND id = ?`, orgID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (s *Store) PostByIdemKey(ctx context.Context, orgID, key string) (Post, error) {
	p, err := scanPost(s.db.QueryRowContext(ctx, `SELECT `+postCols+` FROM posts WHERE org_id = ? AND idem_key = ?`, orgID, key))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// Claim moves a post to "queued" if it is in one of from; only one caller wins.
func (s *Store) Claim(ctx context.Context, id string, from ...string) (bool, error) {
	ph := strings.TrimSuffix(strings.Repeat("?,", len(from)), ",")
	args := []any{now(), id}
	for _, f := range from {
		args = append(args, f)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE posts SET status = 'queued', attempts = attempts + 1, updated_at = ?
		WHERE id = ? AND status IN (`+ph+`)`, args...)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (s *Store) SetExternalIDs(ctx context.Context, id string, ids []string) error {
	raw, _ := json.Marshal(ids)
	_, err := s.db.ExecContext(ctx, `UPDATE posts SET external_ids = ?, updated_at = ? WHERE id = ?`, string(raw), now(), id)
	return err
}

func (s *Store) MarkPublished(ctx context.Context, id, url string, ids []string) error {
	raw, _ := json.Marshal(ids)
	t := now()
	_, err := s.db.ExecContext(ctx, `UPDATE posts SET status = 'published', url = ?, external_ids = ?, error_code = NULL,
		error_message = NULL, published_at = ?, updated_at = ? WHERE id = ?`, ns(url), string(raw), t, t, id)
	return err
}

func (s *Store) MarkFailed(ctx context.Context, id, code, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE posts SET status = 'failed', error_code = ?, error_message = ?, updated_at = ? WHERE id = ?`,
		code, message, now(), id)
	return err
}

// DuePosts lists scheduled posts whose time has come.
func (s *Store) DuePosts(ctx context.Context, at time.Time) ([]Post, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+postCols+` FROM posts WHERE status = 'scheduled' AND scheduled_at <= ? ORDER BY scheduled_at LIMIT 50`,
		at.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// RecoverStuck returns posts left "queued" by a crash: scheduled ones go back
// to the schedule, the rest become a retryable failure the caller can resume.
func (s *Store) RecoverStuck(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-olderThan).Format(time.RFC3339Nano)
	r1, err := s.db.ExecContext(ctx, `UPDATE posts SET status = 'scheduled', updated_at = ? WHERE status = 'queued' AND scheduled_at IS NOT NULL AND updated_at < ?`, now(), cutoff)
	if err != nil {
		return 0, err
	}
	r2, err := s.db.ExecContext(ctx, `UPDATE posts SET status = 'failed', error_code = 'provider_unavailable',
		error_message = 'Publishing was interrupted; retry with the same Idempotency-Key to resume', updated_at = ?
		WHERE status = 'queued' AND scheduled_at IS NULL AND updated_at < ?`, now(), cutoff)
	if err != nil {
		return 0, err
	}
	a, _ := r1.RowsAffected()
	b, _ := r2.RowsAffected()
	return a + b, nil
}
