// Package core provides kubeconfig parsing, writing, merging, and validation
// on top of k8s.io/client-go/tools/clientcmd.
package core

import (
	"fmt"
	"os"
	"strings"
	"sync"

	clientcmd "k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/yaml"
)

// Manager holds all discovered kubeconfig files and provides a unified view.
type Manager struct {
	configs map[string]*NamedConfig // keyed by absolute path
	mu      sync.RWMutex
}

// NewManager creates an empty Manager.
func NewManager() *Manager {
	return &Manager{configs: make(map[string]*NamedConfig)}
}

// Load loads a kubeconfig file and registers it in the manager.
// Subsequent calls with the same path reload the file.
func (m *Manager) Load(path string) error {
	cfg, err := LoadFile(path)
	if err != nil {
		return err
	}
	st, _ := os.Stat(path)
	nc := &NamedConfig{Path: path, Config: cfg}
	if st != nil {
		nc.ModTime = st.ModTime()
	}
	m.mu.Lock()
	m.configs[path] = nc
	m.mu.Unlock()
	return nil
}

// Remove removes a kubeconfig from the manager's view (does not delete the file).
func (m *Manager) Remove(path string) {
	m.mu.Lock()
	delete(m.configs, path)
	m.mu.Unlock()
}

// Paths returns all registered kubeconfig paths.
func (m *Manager) Paths() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	paths := make([]string, 0, len(m.configs))
	for p := range m.configs {
		paths = append(paths, p)
	}
	return paths
}

// Get returns the NamedConfig for the given path, or nil.
func (m *Manager) Get(path string) *NamedConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.configs[path]
}

// AllContexts returns a flat slice of ContextInfo across all loaded configs.
func (m *Manager) AllContexts() []ContextInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []ContextInfo
	for _, nc := range m.configs {
		nc.RLock()
		for name, ctx := range nc.Config.Contexts {
			ci := buildContextInfo(name, ctx, nc)
			ci.IsCurrent = nc.Config.CurrentContext == name
			out = append(out, ci)
		}
		nc.RUnlock()
	}
	return out
}

// FindContext searches all configs for a context by name.
// Returns (NamedConfig, contextName) or an error.
func (m *Manager) FindContext(name string) (*NamedConfig, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, nc := range m.configs {
		nc.RLock()
		_, ok := nc.Config.Contexts[name]
		nc.RUnlock()
		if ok {
			return nc, name, nil
		}
	}
	return nil, "", fmt.Errorf("context %q not found in any kubeconfig", name)
}

// CurrentContext returns the active context name from the primary kubeconfig.
func (m *Manager) CurrentContext(path string) string {
	m.mu.RLock()
	nc := m.configs[path]
	m.mu.RUnlock()
	if nc == nil {
		return ""
	}
	nc.RLock()
	defer nc.RUnlock()
	return nc.Config.CurrentContext
}

// SetCurrentContext sets the current-context field in the given file and writes it.
func (m *Manager) SetCurrentContext(path, name string) error {
	m.mu.RLock()
	nc := m.configs[path]
	m.mu.RUnlock()
	if nc == nil {
		return fmt.Errorf("kubeconfig %q not loaded", path)
	}
	nc.Lock()
	_, ok := nc.Config.Contexts[name]
	if !ok {
		nc.Unlock()
		return fmt.Errorf("context %q does not exist in %s", name, path)
	}
	nc.Config.CurrentContext = name
	cfg := nc.Config
	nc.Unlock()
	return WriteFile(path, cfg)
}

// LoadFile parses a single kubeconfig file.
func LoadFile(path string) (*clientcmdapi.Config, error) {
	cfg, err := clientcmd.LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", path, err)
	}
	return cfg, nil
}

// WriteFile atomically writes a kubeconfig to path using a temp file + rename.
func WriteFile(path string, cfg *clientcmdapi.Config) error {
	tmp := path + ".kcm.tmp"
	if err := clientcmd.WriteToFile(*cfg, tmp); err != nil {
		return fmt.Errorf("writing temp file %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming %s → %s: %w", tmp, path, err)
	}
	return nil
}

// MarshalConfig serialises a Config to canonical YAML (deterministic key order).
func MarshalConfig(cfg *clientcmdapi.Config) ([]byte, error) {
	b, err := clientcmd.Write(*cfg)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// MarshalContext serialises a single Context + its Cluster + AuthInfo to YAML.
func MarshalContext(cfg *clientcmdapi.Config, ctxName string) ([]byte, error) {
	ctx, ok := cfg.Contexts[ctxName]
	if !ok {
		return nil, fmt.Errorf("context %q not found", ctxName)
	}
	mini := clientcmdapi.NewConfig()
	mini.Contexts[ctxName] = ctx
	if ctx.Cluster != "" {
		if cl, ok := cfg.Clusters[ctx.Cluster]; ok {
			mini.Clusters[ctx.Cluster] = cl
		}
	}
	if ctx.AuthInfo != "" {
		if u, ok := cfg.AuthInfos[ctx.AuthInfo]; ok {
			mini.AuthInfos[ctx.AuthInfo] = u
		}
	}
	mini.CurrentContext = ctxName
	return clientcmd.Write(*mini)
}

// MergeConfigs merges src into dst in-memory (no write). Existing keys in dst
// are left untouched; collisions are skipped with a warning returned as []string.
func MergeConfigs(dst, src *clientcmdapi.Config) []string {
	var warnings []string
	for k, v := range src.Clusters {
		if _, exists := dst.Clusters[k]; exists {
			warnings = append(warnings, fmt.Sprintf("cluster %q already exists in destination, skipping", k))
			continue
		}
		dst.Clusters[k] = v
	}
	for k, v := range src.AuthInfos {
		if _, exists := dst.AuthInfos[k]; exists {
			warnings = append(warnings, fmt.Sprintf("user %q already exists in destination, skipping", k))
			continue
		}
		dst.AuthInfos[k] = v
	}
	for k, v := range src.Contexts {
		if _, exists := dst.Contexts[k]; exists {
			warnings = append(warnings, fmt.Sprintf("context %q already exists in destination, skipping", k))
			continue
		}
		dst.Contexts[k] = v
	}
	return warnings
}

// ExtractContext returns a standalone Config containing only the named context
// (plus its referenced Cluster and AuthInfo).
func ExtractContext(src *clientcmdapi.Config, ctxName string) (*clientcmdapi.Config, error) {
	ctx, ok := src.Contexts[ctxName]
	if !ok {
		return nil, fmt.Errorf("context %q not found", ctxName)
	}
	out := clientcmdapi.NewConfig()
	out.CurrentContext = ctxName
	out.Contexts[ctxName] = ctx
	if ctx.Cluster != "" {
		if cl, ok := src.Clusters[ctx.Cluster]; ok {
			out.Clusters[ctx.Cluster] = cl
		}
	}
	if ctx.AuthInfo != "" {
		if u, ok := src.AuthInfos[ctx.AuthInfo]; ok {
			out.AuthInfos[ctx.AuthInfo] = u
		}
	}
	return out, nil
}

// RedactConfig returns a deep copy with sensitive fields replaced by "<redacted>".
func RedactConfig(cfg *clientcmdapi.Config) *clientcmdapi.Config {
	b, _ := clientcmd.Write(*cfg)
	// Crude but reliable: deserialise, zero sensitive fields.
	out := clientcmdapi.NewConfig()
	_ = yaml.Unmarshal(b, out)
	for _, u := range out.AuthInfos {
		if len(u.Token) > 0 {
			u.Token = "<redacted>"
		}
		if len(u.Password) > 0 {
			u.Password = "<redacted>"
		}
		u.ClientCertificateData = nil
		u.ClientKeyData = nil
	}
	for _, cl := range out.Clusters {
		cl.CertificateAuthorityData = nil
	}
	return out
}

// DiffContexts compares two contexts (by name) across the manager and returns
// a list of field-level differences.
func (m *Manager) DiffContexts(name1, name2 string) ([]DiffEntry, error) {
	nc1, _, err := m.FindContext(name1)
	if err != nil {
		return nil, err
	}
	nc2, _, err := m.FindContext(name2)
	if err != nil {
		return nil, err
	}
	nc1.RLock()
	b1, _ := MarshalContext(nc1.Config, name1)
	nc1.RUnlock()
	nc2.RLock()
	b2, _ := MarshalContext(nc2.Config, name2)
	nc2.RUnlock()

	lines1 := strings.Split(string(b1), "\n")
	lines2 := strings.Split(string(b2), "\n")
	var diffs []DiffEntry
	maxLen := len(lines1)
	if len(lines2) > maxLen {
		maxLen = len(lines2)
	}
	for i := 0; i < maxLen; i++ {
		l1, l2 := "", ""
		if i < len(lines1) {
			l1 = lines1[i]
		}
		if i < len(lines2) {
			l2 = lines2[i]
		}
		if l1 != l2 {
			diffs = append(diffs, DiffEntry{Field: fmt.Sprintf("line %d", i+1), Old: l1, New: l2})
		}
	}
	return diffs, nil
}

// buildContextInfo constructs a ContextInfo from raw kubeconfig data.
func buildContextInfo(name string, ctx *clientcmdapi.Context, nc *NamedConfig) ContextInfo {
	ci := ContextInfo{
		Name:      name,
		Namespace: ctx.Namespace,
		SourceFile: nc.Path,
	}
	if ctx.Cluster != "" {
		ci.ClusterName = ctx.Cluster
		if cl, ok := nc.Config.Clusters[ctx.Cluster]; ok {
			ci.Server = cl.Server
			ci.CloudProvider, ci.CloudRegion = detectCloud(cl.Server)
		}
	}
	if ctx.AuthInfo != "" {
		ci.UserName = ctx.AuthInfo
	}
	return ci
}

// detectCloud guesses cloud provider/region from a cluster server URL.
func detectCloud(server string) (provider, region string) {
	switch {
	case strings.Contains(server, ".eks.amazonaws.com"):
		// arn:aws:eks:<region>:<acct>:cluster/<name> or https://<hash>.gr7.<region>.eks.amazonaws.com
		parts := strings.Split(server, ".")
		if len(parts) >= 5 {
			return "aws", parts[len(parts)-4]
		}
		return "aws", ""
	case strings.Contains(server, ".azmk8s.io"):
		return "azure", ""
	case strings.Contains(server, "container.googleapis.com"):
		return "gcp", ""
	case strings.Contains(server, "k8s.ondigitalocean.com"):
		return "digitalocean", ""
	default:
		return "", ""
	}
}
