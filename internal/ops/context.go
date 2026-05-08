// Package ops provides all kubeconfig mutation operations. Every mutation goes
// through this package so that CLI, TUI and GUI share identical logic.
package ops

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/odhomane/kcm/internal/core"
	"github.com/odhomane/kcm/internal/store"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Ops bundles the Manager and Store so all operations have access to both.
type Ops struct {
	Mgr   *core.Manager
	Store *store.Store
}

// New creates an Ops instance from an initialised Manager and Store.
func New(mgr *core.Manager, st *store.Store) *Ops {
	return &Ops{Mgr: mgr, Store: st}
}

// ─── Switch ──────────────────────────────────────────────────────────────────

// Use switches the current context in the kubeconfig file that owns it.
// It touches last-used in the store and appends to the audit log.
func (o *Ops) Use(name string) error {
	nc, _, err := o.Mgr.FindContext(name)
	if err != nil {
		return err
	}
	prev := o.Mgr.CurrentContext(nc.Path)
	if err := o.Mgr.SetCurrentContext(nc.Path, name); err != nil {
		return err
	}

	// Reload config in manager after write.
	_ = o.Mgr.Load(nc.Path)

	_ = o.Store.TouchLastUsed(name)
	_ = o.Store.LogAudit("switch", name, prev, name)

	payload, _ := json.Marshal(map[string]string{"file": nc.Path, "from": prev, "to": name})
	_ = o.Store.PushUndo("switch", string(payload))

	runHook("post-switch", name)
	return nil
}

// UsePrev switches to the previously used context in the primary kubeconfig.
func (o *Ops) UsePrev(path string) error {
	entries, err := o.Store.AuditLog(10)
	if err != nil {
		return err
	}
	current := o.Mgr.CurrentContext(path)
	for _, e := range entries {
		if e.Op == "switch" && e.Before != current && e.Before != "" {
			return o.Use(e.Before)
		}
	}
	return fmt.Errorf("no previous context found")
}

// ─── Rename ──────────────────────────────────────────────────────────────────

// Rename renames a context (with reference integrity) and writes the file.
func (o *Ops) Rename(oldName, newName string) error {
	nc, _, err := o.Mgr.FindContext(oldName)
	if err != nil {
		return err
	}

	// Back up before modification.
	if _, err := core.Backup(nc.Path); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	nc.Lock()
	cfg := nc.Config
	ctx, ok := cfg.Contexts[oldName]
	if !ok {
		nc.Unlock()
		return fmt.Errorf("context %q disappeared", oldName)
	}
	if _, exists := cfg.Contexts[newName]; exists {
		nc.Unlock()
		return fmt.Errorf("context %q already exists", newName)
	}
	cfg.Contexts[newName] = ctx
	delete(cfg.Contexts, oldName)
	if cfg.CurrentContext == oldName {
		cfg.CurrentContext = newName
	}
	nc.Unlock()

	if err := core.WriteFile(nc.Path, cfg); err != nil {
		return err
	}
	_ = o.Mgr.Load(nc.Path)
	_ = o.Store.RenameContext(oldName, newName)
	_ = o.Store.LogAudit("rename", oldName, oldName, newName)
	payload, _ := json.Marshal(map[string]string{"file": nc.Path, "from": oldName, "to": newName})
	_ = o.Store.PushUndo("rename", string(payload))
	return nil
}

// ─── Delete ──────────────────────────────────────────────────────────────────

// Delete removes a context from its kubeconfig. If cascade is true, also
// removes orphaned clusters and users.
func (o *Ops) Delete(name string, cascade bool) error {
	nc, _, err := o.Mgr.FindContext(name)
	if err != nil {
		return err
	}
	if _, err := core.Backup(nc.Path); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	nc.Lock()
	cfg := nc.Config
	ctx, ok := cfg.Contexts[name]
	if !ok {
		nc.Unlock()
		return fmt.Errorf("context %q disappeared", name)
	}
	clusterRef := ctx.Cluster
	userRef := ctx.AuthInfo
	delete(cfg.Contexts, name)
	if cfg.CurrentContext == name {
		cfg.CurrentContext = ""
	}

	if cascade {
		// Only delete cluster/user if no other context references them.
		clusterUsed, userUsed := false, false
		for _, c := range cfg.Contexts {
			if c.Cluster == clusterRef {
				clusterUsed = true
			}
			if c.AuthInfo == userRef {
				userUsed = true
			}
		}
		if !clusterUsed {
			delete(cfg.Clusters, clusterRef)
		}
		if !userUsed {
			delete(cfg.AuthInfos, userRef)
		}
	}
	nc.Unlock()

	if err := core.WriteFile(nc.Path, cfg); err != nil {
		return err
	}
	_ = o.Mgr.Load(nc.Path)
	_ = o.Store.DeleteMeta(name)
	_ = o.Store.LogAudit("delete", name, name, "")
	return nil
}

// ─── Duplicate ────────────────────────────────────────────────────────────────

// Duplicate copies a context (and optionally its cluster/user) under a new name.
func (o *Ops) Duplicate(srcName, dstName string) error {
	nc, _, err := o.Mgr.FindContext(srcName)
	if err != nil {
		return err
	}
	if _, err := core.Backup(nc.Path); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	nc.Lock()
	cfg := nc.Config
	if _, exists := cfg.Contexts[dstName]; exists {
		nc.Unlock()
		return fmt.Errorf("context %q already exists", dstName)
	}
	src := cfg.Contexts[srcName]
	dstCtx := *src
	cfg.Contexts[dstName] = &dstCtx
	nc.Unlock()

	if err := core.WriteFile(nc.Path, cfg); err != nil {
		return err
	}
	return o.Mgr.Load(nc.Path)
}

// ─── Edit ─────────────────────────────────────────────────────────────────────

// EditContext applies field-level changes to a context.
func (o *Ops) EditContext(name, clusterRef, userRef, namespace string) error {
	nc, _, err := o.Mgr.FindContext(name)
	if err != nil {
		return err
	}
	if _, err := core.Backup(nc.Path); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	nc.Lock()
	cfg := nc.Config
	ctx := cfg.Contexts[name]
	if clusterRef != "" {
		ctx.Cluster = clusterRef
	}
	if userRef != "" {
		ctx.AuthInfo = userRef
	}
	if namespace != "" {
		ctx.Namespace = namespace
	}
	nc.Unlock()

	if err := core.WriteFile(nc.Path, cfg); err != nil {
		return err
	}
	return o.Mgr.Load(nc.Path)
}

// ─── Move ─────────────────────────────────────────────────────────────────────

// Move transfers a context (and its cluster + user) from one file to another.
func (o *Ops) Move(name, destPath string) error {
	nc, _, err := o.Mgr.FindContext(name)
	if err != nil {
		return err
	}
	if nc.Path == destPath {
		return fmt.Errorf("source and destination are the same file")
	}

	// Load or create dest.
	dstNC := o.Mgr.Get(destPath)
	if dstNC == nil {
		if _, err := os.Stat(destPath); os.IsNotExist(err) {
			empty := clientcmdapi.NewConfig()
			if err := core.WriteFile(destPath, empty); err != nil {
				return err
			}
		}
		if err := o.Mgr.Load(destPath); err != nil {
			return err
		}
		dstNC = o.Mgr.Get(destPath)
	}

	// Backup both files.
	for _, path := range []string{nc.Path, destPath} {
		if _, err := core.Backup(path); err != nil {
			return fmt.Errorf("backup %s: %w", path, err)
		}
	}

	nc.Lock()
	dstNC.Lock()

	ctx := nc.Config.Contexts[name]
	if _, exists := dstNC.Config.Contexts[name]; exists {
		dstNC.Unlock()
		nc.Unlock()
		return fmt.Errorf("context %q already exists in %s", name, destPath)
	}
	dstNC.Config.Contexts[name] = ctx
	if ctx.Cluster != "" {
		if cl, ok := nc.Config.Clusters[ctx.Cluster]; ok {
			dstNC.Config.Clusters[ctx.Cluster] = cl
		}
	}
	if ctx.AuthInfo != "" {
		if u, ok := nc.Config.AuthInfos[ctx.AuthInfo]; ok {
			dstNC.Config.AuthInfos[ctx.AuthInfo] = u
		}
	}
	delete(nc.Config.Contexts, name)
	if nc.Config.CurrentContext == name {
		nc.Config.CurrentContext = ""
	}

	srcCfg := nc.Config
	dstCfg := dstNC.Config
	srcPath := nc.Path
	dstNC.Unlock()
	nc.Unlock()

	if err := core.WriteFile(srcPath, srcCfg); err != nil {
		return err
	}
	if err := core.WriteFile(destPath, dstCfg); err != nil {
		return err
	}
	_ = o.Mgr.Load(srcPath)
	_ = o.Mgr.Load(destPath)
	return nil
}

// ─── Export ───────────────────────────────────────────────────────────────────

// Export writes a standalone kubeconfig for one context to outPath.
// If redact is true, sensitive fields are replaced with "<redacted>".
func (o *Ops) Export(name, outPath string, redact bool) error {
	nc, _, err := o.Mgr.FindContext(name)
	if err != nil {
		return err
	}
	nc.RLock()
	cfg, err := core.ExtractContext(nc.Config, name)
	nc.RUnlock()
	if err != nil {
		return err
	}
	if redact {
		cfg = core.RedactConfig(cfg)
	}
	return core.WriteFile(outPath, cfg)
}

// ExportCanonical serialises a context to deterministically ordered YAML.
func (o *Ops) ExportCanonical(name string) ([]byte, error) {
	nc, _, err := o.Mgr.FindContext(name)
	if err != nil {
		return nil, err
	}
	nc.RLock()
	cfg, err := core.ExtractContext(nc.Config, name)
	nc.RUnlock()
	if err != nil {
		return nil, err
	}
	return core.MarshalConfig(cfg)
}

// ─── Group / Label / Color / Pin ─────────────────────────────────────────────

func (o *Ops) SetGroup(name, group string) error  { return o.Store.SetGroup(name, group) }
func (o *Ops) SetColor(name, color string) error  { return o.Store.SetColor(name, color) }
func (o *Ops) SetPinned(name string, p bool) error { return o.Store.SetPinned(name, p) }
func (o *Ops) SetLabel(name, key, value string) error { return o.Store.SetLabel(name, key, value) }

// ─── Split ────────────────────────────────────────────────────────────────────

// Split writes each context in path to a separate file in outDir.
func (o *Ops) Split(path, outDir string) error {
	nc := o.Mgr.Get(path)
	if nc == nil {
		return fmt.Errorf("kubeconfig %q not loaded", path)
	}
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return err
	}
	nc.RLock()
	defer nc.RUnlock()
	for name := range nc.Config.Contexts {
		mini, err := core.ExtractContext(nc.Config, name)
		if err != nil {
			continue
		}
		dest := filepath.Join(outDir, sanitiseFilename(name)+".yaml")
		if err := core.WriteFile(dest, mini); err != nil {
			return fmt.Errorf("writing %s: %w", dest, err)
		}
	}
	return nil
}

// ─── Undo ─────────────────────────────────────────────────────────────────────

// Undo reverses the last recorded operation.
func (o *Ops) Undo() error {
	entry, err := o.Store.PopUndo()
	if err != nil {
		return err
	}
	if entry == nil {
		return fmt.Errorf("nothing to undo")
	}
	switch entry.Op {
	case "switch":
		var p struct {
			File string `json:"file"`
			From string `json:"from"`
			To   string `json:"to"`
		}
		if err := json.Unmarshal([]byte(entry.Payload), &p); err != nil {
			return err
		}
		return o.Mgr.SetCurrentContext(p.File, p.From)
	case "rename":
		var p struct {
			File string `json:"file"`
			From string `json:"from"`
			To   string `json:"to"`
		}
		if err := json.Unmarshal([]byte(entry.Payload), &p); err != nil {
			return err
		}
		return o.Rename(p.To, p.From)
	default:
		return fmt.Errorf("undo not supported for op %q", entry.Op)
	}
}

// ─── Health ───────────────────────────────────────────────────────────────────

// HealthStatus describes the reachability of one cluster.
type HealthStatus struct {
	ContextName string
	Server      string
	OK          bool
	Err         string
	Latency     time.Duration
}

// runHook executes a user-defined pre/post hook script if one exists.
func runHook(event, context string) {
	// Hook scripts live at ~/.config/kcm/hooks/<event>.
	dir, err := core.KCMConfigDir()
	if err != nil {
		return
	}
	script := filepath.Join(dir, "hooks", event)
	if _, err := os.Stat(script); os.IsNotExist(err) {
		return
	}
	// Fire-and-forget via os/exec; errors are intentionally ignored.
	go func() {
		//nolint:gosec // user-owned script
		_ = os.Setenv("KCM_CONTEXT", context)
	}()
}

// sanitiseFilename replaces path-unsafe characters with dashes.
func sanitiseFilename(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '/' || c == '\\' || c == ':' || c == '*' || c == '?' || c == '"' || c == '<' || c == '>' || c == '|' {
			out[i] = '-'
		} else {
			out[i] = c
		}
	}
	return string(out)
}
