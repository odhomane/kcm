package ops

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/odhomane/kcm/internal/core"
	clientcmd "k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// ConflictResolution controls what happens when a name collision is detected.
type ConflictResolution int

const (
	ConflictSkip      ConflictResolution = iota // leave existing, skip incoming
	ConflictOverwrite                            // replace existing with incoming
	ConflictAutoRename                           // append suffix until unique
)

// MergeFiles merges all src configs into destPath and writes the result.
// Returns any warnings (name collisions skipped).
func (o *Ops) MergeFiles(srcPaths []string, destPath string, resolution ConflictResolution) ([]string, error) {
	// Load or create dest.
	var destCfg *clientcmdapi.Config
	if _, err := os.Stat(destPath); err == nil {
		if _, err2 := core.Backup(destPath); err2 != nil {
			return nil, fmt.Errorf("backup dest: %w", err2)
		}
		destCfg, err = core.LoadFile(destPath)
		if err != nil {
			return nil, err
		}
	} else {
		destCfg = clientcmdapi.NewConfig()
	}

	var allWarnings []string
	for _, src := range srcPaths {
		srcCfg, err := core.LoadFile(src)
		if err != nil {
			allWarnings = append(allWarnings, fmt.Sprintf("skipping %s: %v", src, err))
			continue
		}
		switch resolution {
		case ConflictOverwrite:
			mergeOverwrite(destCfg, srcCfg)
		case ConflictAutoRename:
			mergeAutoRename(destCfg, srcCfg)
		default: // ConflictSkip
			w := core.MergeConfigs(destCfg, srcCfg)
			allWarnings = append(allWarnings, w...)
		}
	}

	if err := core.WriteFile(destPath, destCfg); err != nil {
		return allWarnings, err
	}
	return allWarnings, o.Mgr.Load(destPath)
}

// mergeOverwrite merges src into dst, replacing collisions.
func mergeOverwrite(dst, src *clientcmdapi.Config) {
	for k, v := range src.Clusters {
		dst.Clusters[k] = v
	}
	for k, v := range src.AuthInfos {
		dst.AuthInfos[k] = v
	}
	for k, v := range src.Contexts {
		dst.Contexts[k] = v
	}
}

// mergeAutoRename merges src into dst, renaming collisions with numeric suffix.
func mergeAutoRename(dst, src *clientcmdapi.Config) {
	for k, v := range src.Clusters {
		dst.Clusters[uniqueName(dst.Clusters, k)] = v
	}
	for k, v := range src.AuthInfos {
		dst.AuthInfos[uniqueName(dst.AuthInfos, k)] = v
	}
	for k, v := range src.Contexts {
		dst.Contexts[uniqueName(dst.Contexts, k)] = v
	}
}

func uniqueName[V any](m map[string]V, base string) string {
	if _, exists := m[base]; !exists {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if _, exists := m[candidate]; !exists {
			return candidate
		}
	}
}

// ─── Import ───────────────────────────────────────────────────────────────────

// ImportSource is where to read a kubeconfig from.
type ImportSource struct {
	Path   string // file path
	URL    string // HTTP URL
	Stdin  bool   // read from os.Stdin
}

// Import reads a kubeconfig from src and merges it into destPath.
func (o *Ops) Import(src ImportSource, destPath string, resolution ConflictResolution) ([]string, error) {
	data, err := readImportSource(src)
	if err != nil {
		return nil, err
	}

	// Parse the incoming config.
	srcCfg, err := clientcmd.Load(data)
	if err != nil {
		return nil, fmt.Errorf("parsing imported kubeconfig: %w", err)
	}

	// Write to a temp file so MergeFiles can read it.
	tmp, err := os.CreateTemp("", "kcm-import-*.yaml")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return nil, err
	}
	tmp.Close()

	// Load into manager temporarily.
	if err := o.Mgr.Load(tmp.Name()); err != nil {
		return nil, err
	}
	defer o.Mgr.Remove(tmp.Name())

	_ = srcCfg // already used above for validation

	return o.MergeFiles([]string{tmp.Name()}, destPath, resolution)
}

func readImportSource(src ImportSource) ([]byte, error) {
	switch {
	case src.Stdin:
		return io.ReadAll(os.Stdin)
	case src.URL != "":
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Get(src.URL) //nolint:noctx
		if err != nil {
			return nil, fmt.Errorf("fetching %s: %w", src.URL, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("fetching %s: HTTP %d", src.URL, resp.StatusCode)
		}
		return io.ReadAll(resp.Body)
	case src.Path != "":
		return os.ReadFile(src.Path)
	default:
		return nil, fmt.Errorf("no import source specified")
	}
}

// ─── Validate ─────────────────────────────────────────────────────────────────

// ValidatePath loads a kubeconfig and returns all validation issues.
func (o *Ops) ValidatePath(path string) ([]core.ValidationIssue, error) {
	cfg, err := core.LoadFile(path)
	if err != nil {
		return nil, err
	}
	return core.Validate(path, cfg), nil
}

// ValidateAll validates all loaded configs and checks for cross-file conflicts.
func (o *Ops) ValidateAll() []core.ValidationIssue {
	paths := o.Mgr.Paths()
	var allIssues []core.ValidationIssue
	configs := make(map[string]*clientcmdapi.Config, len(paths))
	for _, p := range paths {
		nc := o.Mgr.Get(p)
		if nc == nil {
			continue
		}
		nc.RLock()
		issues := core.Validate(p, nc.Config)
		configs[p] = nc.Config
		nc.RUnlock()
		allIssues = append(allIssues, issues...)
	}
	// Cross-file conflict detection.
	plist := make([]string, 0, len(configs))
	for p := range configs {
		plist = append(plist, p)
	}
	for i := 0; i < len(plist); i++ {
		for j := i + 1; j < len(plist); j++ {
			issues := core.DetectConflicts(plist[i], configs[plist[i]], plist[j], configs[plist[j]])
			allIssues = append(allIssues, issues...)
		}
	}
	return allIssues
}

// ─── Diff ─────────────────────────────────────────────────────────────────────

// DiffFiles returns a unified-diff-style string between two kubeconfig files.
func (o *Ops) DiffFiles(path1, path2 string) (string, error) {
	b1, err := os.ReadFile(path1)
	if err != nil {
		return "", err
	}
	b2, err := os.ReadFile(path2)
	if err != nil {
		return "", err
	}
	lines1 := strings.Split(string(b1), "\n")
	lines2 := strings.Split(string(b2), "\n")
	return unifiedDiff(filepath.Base(path1), filepath.Base(path2), lines1, lines2), nil
}

// unifiedDiff produces a simple unified-diff text.
func unifiedDiff(aName, bName string, a, b []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", aName, bName)
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}
	for i := 0; i < maxLen; i++ {
		la, lb := "", ""
		if i < len(a) {
			la = a[i]
		}
		if i < len(b) {
			lb = b[i]
		}
		if la == lb {
			fmt.Fprintf(&sb, " %s\n", la)
		} else {
			if la != "" {
				fmt.Fprintf(&sb, "-%s\n", la)
			}
			if lb != "" {
				fmt.Fprintf(&sb, "+%s\n", lb)
			}
		}
	}
	return sb.String()
}

// Backup and WriteFile aliases keep ops self-contained without import cycle.
var Backup = core.Backup
var WriteFile = core.WriteFile
