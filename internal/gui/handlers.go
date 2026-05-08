package gui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/odhomane/kcm/internal/core"
	"github.com/odhomane/kcm/internal/ops"
	"github.com/odhomane/kcm/internal/store"
)

// handleIndex serves the single-page dashboard shell.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFS(staticFS, "static/index.html")
	if err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(w, map[string]interface{}{
		"Title":   "kcm — Kubeconfig Manager",
		"Version": "0.1.0",
	})
}

// ─── /api/contexts ────────────────────────────────────────────────────────────

type apiContext struct {
	Name          string            `json:"name"`
	ClusterName   string            `json:"cluster"`
	UserName      string            `json:"user"`
	Namespace     string            `json:"namespace"`
	Server        string            `json:"server"`
	SourceFile    string            `json:"source"`
	IsCurrent     bool              `json:"current"`
	Group         string            `json:"group"`
	Color         string            `json:"color"`
	Labels        map[string]string `json:"labels"`
	Pinned        bool              `json:"pinned"`
	LastUsed      *time.Time        `json:"lastUsed"`
	CloudProvider string            `json:"cloudProvider"`
	CloudRegion   string            `json:"cloudRegion"`
}

func (s *Server) handleContexts(w http.ResponseWriter, r *http.Request) {
	contexts := s.mgr.AllContexts()
	metaMap, _ := s.st.AllMeta()

	// Filter params.
	group := r.URL.Query().Get("group")
	search := strings.ToLower(r.URL.Query().Get("q"))
	pinnedOnly := r.URL.Query().Get("pinned") == "1"

	rows := make([]apiContext, 0, len(contexts))
	for _, ci := range contexts {
		m := metaMap[ci.Name]
		if group != "" && m.Group != group {
			continue
		}
		if pinnedOnly && !m.Pinned {
			continue
		}
		if search != "" {
			haystack := strings.ToLower(ci.Name + " " + ci.ClusterName + " " + ci.Server + " " + m.Group)
			if !strings.Contains(haystack, search) {
				continue
			}
		}
		rows = append(rows, apiContext{
			Name:          ci.Name,
			ClusterName:   ci.ClusterName,
			UserName:      ci.UserName,
			Namespace:     ci.Namespace,
			Server:        ci.Server,
			SourceFile:    ci.SourceFile,
			IsCurrent:     ci.IsCurrent,
			Group:         m.Group,
			Color:         m.Color,
			Labels:        m.Labels,
			Pinned:        m.Pinned,
			LastUsed:      m.LastUsedAt,
			CloudProvider: ci.CloudProvider,
			CloudRegion:   ci.CloudRegion,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })

	jsonResp(w, map[string]interface{}{
		"contexts": rows,
		"total":    len(rows),
	})
}

// ─── Context mutations ────────────────────────────────────────────────────────

func (s *Server) handleUse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	name := r.FormValue("name")
	if name == "" {
		jsonError(w, "name is required", http.StatusBadRequest)
		return
	}
	if err := s.op.Use(name); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.broadcast("context-switched")
	jsonResp(w, map[string]string{"status": "ok", "context": name})
}

func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	old := r.FormValue("old")
	new_ := r.FormValue("new")
	if old == "" || new_ == "" {
		jsonError(w, "old and new are required", http.StatusBadRequest)
		return
	}
	if err := s.op.Rename(old, new_); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.broadcast("context-renamed")
	jsonResp(w, map[string]string{"status": "ok"})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		http.Error(w, "POST or DELETE only", http.StatusMethodNotAllowed)
		return
	}
	name := r.FormValue("name")
	cascade := r.FormValue("cascade") == "true" || r.FormValue("cascade") == "1"
	if name == "" {
		jsonError(w, "name is required", http.StatusBadRequest)
		return
	}
	if err := s.op.Delete(name, cascade); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.broadcast("context-deleted")
	jsonResp(w, map[string]string{"status": "ok"})
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	redact := r.URL.Query().Get("redact") == "1"
	if name == "" {
		jsonError(w, "name is required", http.StatusBadRequest)
		return
	}

	nc, _, err := s.mgr.FindContext(name)
	if err != nil {
		jsonError(w, err.Error(), http.StatusNotFound)
		return
	}
	nc.RLock()
	cfg, err := core.ExtractContext(nc.Config, name)
	nc.RUnlock()
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if redact {
		cfg = core.RedactConfig(cfg)
	}
	b, err := core.MarshalConfig(cfg)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/yaml")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.yaml"`, name))
	_, _ = w.Write(b)
}

func (s *Server) handleSetGroup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	name := r.FormValue("name")
	group := r.FormValue("group")
	if name == "" {
		jsonError(w, "name is required", http.StatusBadRequest)
		return
	}
	if err := s.op.SetGroup(name, group); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.broadcast("context-updated")
	jsonResp(w, map[string]string{"status": "ok"})
}

func (s *Server) handleSetLabel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	name := r.FormValue("name")
	key := r.FormValue("key")
	value := r.FormValue("value")
	if name == "" || key == "" {
		jsonError(w, "name and key are required", http.StatusBadRequest)
		return
	}
	if err := s.op.SetLabel(name, key, value); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.broadcast("context-updated")
	jsonResp(w, map[string]string{"status": "ok"})
}

func (s *Server) handleSetColor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	name := r.FormValue("name")
	color := r.FormValue("color")
	if name == "" {
		jsonError(w, "name is required", http.StatusBadRequest)
		return
	}
	if err := s.op.SetColor(name, color); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.broadcast("context-updated")
	jsonResp(w, map[string]string{"status": "ok"})
}

func (s *Server) handlePin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	name := r.FormValue("name")
	pinned, _ := strconv.ParseBool(r.FormValue("pinned"))
	if name == "" {
		jsonError(w, "name is required", http.StatusBadRequest)
		return
	}
	if err := s.op.SetPinned(name, pinned); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.broadcast("context-updated")
	jsonResp(w, map[string]string{"status": "ok"})
}

// ─── Namespaces ───────────────────────────────────────────────────────────────

func (s *Server) handleNamespaces(w http.ResponseWriter, r *http.Request) {
	ctx := r.URL.Query().Get("context")
	if ctx == "" {
		jsonError(w, "context is required", http.StatusBadRequest)
		return
	}
	namespaces, err := s.op.ListNamespaces(ctx, 5*time.Minute)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResp(w, map[string]interface{}{"namespaces": namespaces})
}

// ─── Health ───────────────────────────────────────────────────────────────────

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.URL.Query().Get("context")
	timeout := 3 * time.Second
	if t := r.URL.Query().Get("timeout"); t != "" {
		if d, err := time.ParseDuration(t); err == nil {
			timeout = d
		}
	}

	if ctx != "" {
		hs := s.op.CheckHealth(ctx, timeout)
		jsonResp(w, hs)
		return
	}
	results := s.op.BulkHealth(timeout)
	jsonResp(w, map[string]interface{}{"results": results})
}

// ─── Groups ───────────────────────────────────────────────────────────────────

func (s *Server) handleGroups(w http.ResponseWriter, r *http.Request) {
	metaMap, err := s.st.AllMeta()
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	groups := make(map[string][]string)
	for name, m := range metaMap {
		if m.Group != "" {
			groups[m.Group] = append(groups[m.Group], name)
		}
	}
	jsonResp(w, map[string]interface{}{"groups": groups})
}

// ─── Files ────────────────────────────────────────────────────────────────────

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	paths := s.mgr.Paths()
	type fileInfo struct {
		Path     string `json:"path"`
		Contexts int    `json:"contexts"`
	}
	out := make([]fileInfo, 0, len(paths))
	for _, p := range paths {
		nc := s.mgr.Get(p)
		count := 0
		if nc != nil {
			nc.RLock()
			count = len(nc.Config.Contexts)
			nc.RUnlock()
		}
		out = append(out, fileInfo{Path: p, Contexts: count})
	}
	jsonResp(w, map[string]interface{}{"files": out})
}

// ─── Validate ─────────────────────────────────────────────────────────────────

func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	var issues []core.ValidationIssue
	if path := r.URL.Query().Get("path"); path != "" {
		cfg, err := core.LoadFile(path)
		if err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		issues = core.Validate(path, cfg)
	} else {
		issues = s.op.ValidateAll()
	}
	jsonResp(w, map[string]interface{}{"issues": issues, "count": len(issues)})
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func (s *Server) broadcast(event string) {
	select {
	case s.events <- event:
	default:
	}
}

func jsonResp(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// Ensure unused imports are kept.
var _ = ops.ConflictSkip
var _ = store.ContextMeta{}
