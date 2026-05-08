package core

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const maxBackups = 50

// Backup creates a timestamped copy of path in the KCM backup directory.
// Returns the backup file path.
func Backup(path string) (string, error) {
	backupDir, err := KCMBackupDir()
	if err != nil {
		return "", fmt.Errorf("getting backup dir: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading %s for backup: %w", path, err)
	}

	ts := time.Now().UTC().Format("20060102-150405")
	base := filepath.Base(path)
	dest := filepath.Join(backupDir, fmt.Sprintf("%s-%s", ts, base))

	if err := os.WriteFile(dest, data, 0o600); err != nil {
		return "", fmt.Errorf("writing backup %s: %w", dest, err)
	}

	if err := pruneBackups(backupDir, base); err != nil {
		// Non-fatal: just log; don't fail the backup.
		fmt.Fprintf(os.Stderr, "kcm: pruning backups: %v\n", err)
	}

	return dest, nil
}

// pruneBackups deletes oldest backups for a given original filename, keeping at
// most maxBackups.
func pruneBackups(backupDir, originalBase string) error {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return err
	}

	// Collect backups for this filename (suffix match).
	var matching []string
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		if strings.HasSuffix(e.Name(), "-"+originalBase) {
			matching = append(matching, filepath.Join(backupDir, e.Name()))
		}
	}

	sort.Strings(matching) // timestamp prefix → chronological order
	if len(matching) <= maxBackups {
		return nil
	}
	for _, old := range matching[:len(matching)-maxBackups] {
		if err := os.Remove(old); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// BackupEntry describes a single backup file.
type BackupEntry struct {
	ID       string // timestamp prefix
	Original string // original filename
	Path     string
	Time     time.Time
}

// ListBackups returns all backup entries, newest first.
func ListBackups() ([]BackupEntry, error) {
	backupDir, err := KCMBackupDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return nil, err
	}

	var out []BackupEntry
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		name := e.Name()
		// Format: 20060102-150405-<original>
		if len(name) < 16 {
			continue
		}
		ts := name[:15] // "20060102-150405"
		original := name[16:]
		t, err := time.Parse("20060102-150405", ts)
		if err != nil {
			continue
		}
		out = append(out, BackupEntry{
			ID:       ts,
			Original: original,
			Path:     filepath.Join(backupDir, name),
			Time:     t,
		})
	}

	// Newest first.
	sort.Slice(out, func(i, j int) bool {
		return out[i].Time.After(out[j].Time)
	})
	return out, nil
}

// RestoreBackup copies the backup at backupPath to the destination path,
// after taking a backup of the current destination.
func RestoreBackup(backupPath, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		if _, err := Backup(dest); err != nil {
			return fmt.Errorf("backing up current file before restore: %w", err)
		}
	}
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("reading backup %s: %w", backupPath, err)
	}
	return os.WriteFile(dest, data, 0o600)
}
