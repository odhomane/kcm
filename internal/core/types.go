package core

import (
	"sync"
	"time"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// NamedConfig is a kubeconfig paired with its source file path.
type NamedConfig struct {
	Path    string
	Config  *clientcmdapi.Config
	ModTime time.Time
	mu      sync.RWMutex
}

func (nc *NamedConfig) RLock()   { nc.mu.RLock() }
func (nc *NamedConfig) RUnlock() { nc.mu.RUnlock() }
func (nc *NamedConfig) Lock()    { nc.mu.Lock() }
func (nc *NamedConfig) Unlock()  { nc.mu.Unlock() }

// ContextInfo is a flattened, enriched view of a single context across all
// discovered kubeconfig files — suitable for display and sorting.
type ContextInfo struct {
	Name         string
	ClusterName  string
	UserName     string
	Namespace    string
	Server       string
	SourceFile   string
	IsCurrent    bool
	CloudProvider string
	CloudRegion   string
}

// ValidationIssue describes a single problem found in a kubeconfig.
type ValidationIssue struct {
	File     string
	Context  string
	Severity string // "error" | "warning"
	Message  string
}

// DiffEntry is one line in a context diff.
type DiffEntry struct {
	Field string
	Old   string
	New   string
}
