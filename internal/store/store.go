package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// Store wraps the SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_journal=WAL&_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("opening db %s: %w", path, err)
	}
	s := &Store{db: db}
	return s, s.migrate()
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// migrate creates all tables if they don't exist.
func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS context_meta (
    name        TEXT PRIMARY KEY,
    grp         TEXT NOT NULL DEFAULT '',
    color       TEXT NOT NULL DEFAULT '',
    labels      TEXT NOT NULL DEFAULT '{}',
    pinned      INTEGER NOT NULL DEFAULT 0,
    namespace   TEXT NOT NULL DEFAULT '',
    last_used   INTEGER
);

CREATE TABLE IF NOT EXISTS audit_log (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    ts          INTEGER NOT NULL,
    op          TEXT NOT NULL,
    context     TEXT NOT NULL DEFAULT '',
    before_yaml TEXT NOT NULL DEFAULT '',
    after_yaml  TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS undo_stack (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    ts          INTEGER NOT NULL,
    op          TEXT NOT NULL,
    payload     TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS profiles (
    name        TEXT PRIMARY KEY,
    paths       TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS favorite_ns (
    context     TEXT NOT NULL,
    namespace   TEXT NOT NULL,
    PRIMARY KEY (context, namespace)
);

CREATE INDEX IF NOT EXISTS idx_audit_ts ON audit_log(ts DESC);
CREATE INDEX IF NOT EXISTS idx_undo_ts  ON undo_stack(ts DESC);
`)
	return err
}

// ─── Context metadata ────────────────────────────────────────────────────────

// GetMeta returns metadata for a context, or a zero value if none exists.
func (s *Store) GetMeta(name string) (ContextMeta, error) {
	row := s.db.QueryRow(
		`SELECT name, grp, color, labels, pinned, namespace, last_used
		 FROM context_meta WHERE name = ?`, name)
	return scanMeta(row)
}

// UpsertMeta inserts or replaces the metadata for a context.
func (s *Store) UpsertMeta(m ContextMeta) error {
	labels, err := json.Marshal(m.Labels)
	if err != nil {
		labels = []byte("{}")
	}
	var lastUsed *int64
	if m.LastUsedAt != nil {
		t := m.LastUsedAt.Unix()
		lastUsed = &t
	}
	pinned := 0
	if m.Pinned {
		pinned = 1
	}
	_, err = s.db.Exec(`
		INSERT INTO context_meta (name, grp, color, labels, pinned, namespace, last_used)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			grp=excluded.grp, color=excluded.color, labels=excluded.labels,
			pinned=excluded.pinned, namespace=excluded.namespace,
			last_used=excluded.last_used`,
		m.Name, m.Group, m.Color, string(labels), pinned, m.Namespace, lastUsed)
	return err
}

// TouchLastUsed records "now" as the last-used time for a context.
func (s *Store) TouchLastUsed(name string) error {
	now := time.Now().Unix()
	_, err := s.db.Exec(`
		INSERT INTO context_meta (name, last_used)
		VALUES (?, ?)
		ON CONFLICT(name) DO UPDATE SET last_used=excluded.last_used`, name, now)
	return err
}

// SetGroup updates only the group field.
func (s *Store) SetGroup(name, group string) error {
	_, err := s.db.Exec(`
		INSERT INTO context_meta (name, grp) VALUES (?, ?)
		ON CONFLICT(name) DO UPDATE SET grp=excluded.grp`, name, group)
	return err
}

// SetColor updates only the color field.
func (s *Store) SetColor(name, color string) error {
	_, err := s.db.Exec(`
		INSERT INTO context_meta (name, color) VALUES (?, ?)
		ON CONFLICT(name) DO UPDATE SET color=excluded.color`, name, color)
	return err
}

// SetPinned updates the pinned flag.
func (s *Store) SetPinned(name string, pinned bool) error {
	p := 0
	if pinned {
		p = 1
	}
	_, err := s.db.Exec(`
		INSERT INTO context_meta (name, pinned) VALUES (?, ?)
		ON CONFLICT(name) DO UPDATE SET pinned=excluded.pinned`, name, p)
	return err
}

// SetLabel adds or updates a single label key=value on a context.
func (s *Store) SetLabel(name, key, value string) error {
	m, err := s.GetMeta(name)
	if err != nil {
		return err
	}
	if m.Labels == nil {
		m.Labels = make(map[string]string)
	}
	m.Labels[key] = value
	return s.UpsertMeta(m)
}

// AllMeta returns metadata for all contexts that have any stored metadata.
func (s *Store) AllMeta() (map[string]ContextMeta, error) {
	rows, err := s.db.Query(
		`SELECT name, grp, color, labels, pinned, namespace, last_used FROM context_meta`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]ContextMeta)
	for rows.Next() {
		m, err := scanMeta(rows)
		if err != nil {
			return nil, err
		}
		out[m.Name] = m
	}
	return out, rows.Err()
}

// RenameContext updates metadata keys when a context is renamed.
func (s *Store) RenameContext(oldName, newName string) error {
	_, err := s.db.Exec(`UPDATE context_meta SET name=? WHERE name=?`, newName, oldName)
	return err
}

// DeleteMeta removes metadata for a context.
func (s *Store) DeleteMeta(name string) error {
	_, err := s.db.Exec(`DELETE FROM context_meta WHERE name=?`, name)
	return err
}

// scanMeta scans a row into ContextMeta. Works for *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanMeta(row scanner) (ContextMeta, error) {
	var m ContextMeta
	var labels string
	var lastUsed *int64
	var pinned int
	err := row.Scan(&m.Name, &m.Group, &m.Color, &labels, &pinned, &m.Namespace, &lastUsed)
	if err == sql.ErrNoRows {
		return ContextMeta{}, nil
	}
	if err != nil {
		return m, err
	}
	_ = json.Unmarshal([]byte(labels), &m.Labels)
	if m.Labels == nil {
		m.Labels = make(map[string]string)
	}
	m.Pinned = pinned == 1
	if lastUsed != nil {
		t := time.Unix(*lastUsed, 0)
		m.LastUsedAt = &t
	}
	return m, nil
}

// ─── Audit log ───────────────────────────────────────────────────────────────

// LogAudit appends an entry to the audit log.
func (s *Store) LogAudit(op, context, before, after string) error {
	_, err := s.db.Exec(`
		INSERT INTO audit_log (ts, op, context, before_yaml, after_yaml)
		VALUES (?, ?, ?, ?, ?)`,
		time.Now().Unix(), op, context, before, after)
	return err
}

// AuditLog returns the last N audit entries, newest first.
func (s *Store) AuditLog(limit int) ([]AuditEntry, error) {
	rows, err := s.db.Query(`
		SELECT id, ts, op, context, before_yaml, after_yaml
		FROM audit_log ORDER BY ts DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var ts int64
		if err := rows.Scan(&e.ID, &ts, &e.Op, &e.Context, &e.Before, &e.After); err != nil {
			return nil, err
		}
		e.Timestamp = time.Unix(ts, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ─── Undo stack ──────────────────────────────────────────────────────────────

// PushUndo pushes a reversible operation onto the undo stack.
func (s *Store) PushUndo(op, payload string) error {
	_, err := s.db.Exec(`INSERT INTO undo_stack (ts, op, payload) VALUES (?, ?, ?)`,
		time.Now().Unix(), op, payload)
	return err
}

// PopUndo removes and returns the most recent undo entry.
func (s *Store) PopUndo() (*UndoEntry, error) {
	row := s.db.QueryRow(`SELECT id, ts, op, payload FROM undo_stack ORDER BY id DESC LIMIT 1`)
	var e UndoEntry
	var ts int64
	if err := row.Scan(&e.ID, &ts, &e.Op, &e.Payload); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	e.Timestamp = time.Unix(ts, 0)
	_, err := s.db.Exec(`DELETE FROM undo_stack WHERE id=?`, e.ID)
	return &e, err
}

// ─── Profiles ────────────────────────────────────────────────────────────────

// SaveProfile persists a named profile.
func (s *Store) SaveProfile(p Profile) error {
	_, err := s.db.Exec(`
		INSERT INTO profiles (name, paths) VALUES (?, ?)
		ON CONFLICT(name) DO UPDATE SET paths=excluded.paths`,
		p.Name, strings.Join(p.Paths, "\n"))
	return err
}

// GetProfile retrieves a profile by name.
func (s *Store) GetProfile(name string) (Profile, error) {
	var p Profile
	var paths string
	err := s.db.QueryRow(`SELECT name, paths FROM profiles WHERE name=?`, name).
		Scan(&p.Name, &paths)
	if err == sql.ErrNoRows {
		return Profile{}, fmt.Errorf("profile %q not found", name)
	}
	if err != nil {
		return p, err
	}
	if paths != "" {
		p.Paths = strings.Split(paths, "\n")
	}
	return p, nil
}

// ListProfiles returns all profiles.
func (s *Store) ListProfiles() ([]Profile, error) {
	rows, err := s.db.Query(`SELECT name, paths FROM profiles ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Profile
	for rows.Next() {
		var p Profile
		var paths string
		if err := rows.Scan(&p.Name, &paths); err != nil {
			return nil, err
		}
		if paths != "" {
			p.Paths = strings.Split(paths, "\n")
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ─── Favourite namespaces ─────────────────────────────────────────────────────

// AddFavoriteNS marks a namespace as favourite for a context.
func (s *Store) AddFavoriteNS(ctx, ns string) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO favorite_ns (context, namespace) VALUES (?,?)`, ctx, ns)
	return err
}

// RemoveFavoriteNS removes a favourite namespace.
func (s *Store) RemoveFavoriteNS(ctx, ns string) error {
	_, err := s.db.Exec(`DELETE FROM favorite_ns WHERE context=? AND namespace=?`, ctx, ns)
	return err
}

// FavoriteNamespaces lists favourite namespaces for a context.
func (s *Store) FavoriteNamespaces(ctx string) ([]string, error) {
	rows, err := s.db.Query(`SELECT namespace FROM favorite_ns WHERE context=? ORDER BY namespace`, ctx)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var ns string
		if err := rows.Scan(&ns); err != nil {
			return nil, err
		}
		out = append(out, ns)
	}
	return out, rows.Err()
}
