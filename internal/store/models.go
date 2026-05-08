// Package store persists context metadata (groups, labels, colors, last-used,
// audit log, undo stack) in a local SQLite database via modernc.org/sqlite.
package store

import "time"

// ContextMeta holds user-defined metadata for a single context.
type ContextMeta struct {
	Name       string
	Group      string
	Color      string
	Labels     map[string]string
	Pinned     bool
	Namespace  string // per-context default namespace override
	LastUsedAt *time.Time
}

// AuditEntry is one row in the audit log.
type AuditEntry struct {
	ID        int64
	Timestamp time.Time
	Op        string // "switch", "rename", "delete", "merge", etc.
	Context   string
	Before    string // YAML snapshot before change
	After     string // YAML snapshot after change
}

// UndoEntry describes a reversible operation stored in the undo stack.
type UndoEntry struct {
	ID        int64
	Timestamp time.Time
	Op        string
	Payload   string // JSON blob sufficient to reverse the operation
}

// Profile is a named set of kubeconfig file paths.
type Profile struct {
	Name  string
	Paths []string // paths separated by newline in DB
}

// FavoriteNS is a namespace marked as favourite for a specific context.
type FavoriteNS struct {
	Context   string
	Namespace string
}
