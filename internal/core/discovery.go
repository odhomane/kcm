package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DiscoverPaths returns all kubeconfig file paths that should be loaded.
// Order of precedence:
//  1. $KUBECONFIG (colon-separated on Unix, semicolon on Windows)
//  2. ~/.kube/config
//  3. ~/.kube/configs/*.yaml and *.yml
//  4. paths stored in ~/.config/kcm/config.yaml (loaded separately via Viper)
func DiscoverPaths(extraPaths []string) []string {
	seen := make(map[string]bool)
	var paths []string

	add := func(p string) {
		p = filepath.Clean(p)
		if p == "" || p == "." {
			return
		}
		if seen[p] {
			return
		}
		if _, err := os.Stat(p); err == nil {
			seen[p] = true
			paths = append(paths, p)
		}
	}

	// 1. $KUBECONFIG
	if kc := os.Getenv("KUBECONFIG"); kc != "" {
		sep := ":"
		if runtime.GOOS == "windows" {
			sep = ";"
		}
		for _, p := range strings.Split(kc, sep) {
			add(expandHome(p))
		}
	}

	// 2. ~/.kube/config
	add(expandHome("~/.kube/config"))

	// 3. ~/.kube/configs/
	globDir := expandHome("~/.kube/configs")
	for _, pattern := range []string{"*.yaml", "*.yml"} {
		matches, _ := filepath.Glob(filepath.Join(globDir, pattern))
		for _, m := range matches {
			add(m)
		}
	}

	// 4. User-supplied extras (from kcm config or CLI flags)
	for _, p := range extraPaths {
		add(expandHome(p))
	}

	return paths
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

// KCMConfigDir returns ~/.config/kcm, creating it if necessary.
func KCMConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "kcm")
	return dir, os.MkdirAll(dir, 0o750)
}

// KCMBackupDir returns ~/.config/kcm/backups, creating it if necessary.
func KCMBackupDir() (string, error) {
	dir, err := KCMConfigDir()
	if err != nil {
		return "", err
	}
	backupDir := filepath.Join(dir, "backups")
	return backupDir, os.MkdirAll(backupDir, 0o750)
}

// KCMDBPath returns the path to the SQLite database.
func KCMDBPath() (string, error) {
	dir, err := KCMConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "kcm.db"), nil
}

// KCMAuditLogPath returns the path to the audit log.
func KCMAuditLogPath() (string, error) {
	dir, err := KCMConfigDir()
	if err != nil {
		return "", err
	}
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o750); err != nil {
		return "", err
	}
	return filepath.Join(logDir, "kcm.log"), nil
}
