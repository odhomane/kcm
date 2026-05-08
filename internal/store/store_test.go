package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/odhomane/kcm/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestContextMeta_Roundtrip(t *testing.T) {
	st := openTestStore(t)
	now := time.Now().Truncate(time.Second)
	m := store.ContextMeta{
		Name:      "ctx-prod",
		Group:     "production",
		Color:     "red",
		Labels:    map[string]string{"env": "prod", "tier": "backend"},
		Pinned:    true,
		Namespace: "default",
		LastUsedAt: &now,
	}
	if err := st.UpsertMeta(m); err != nil {
		t.Fatalf("UpsertMeta: %v", err)
	}
	got, err := st.GetMeta("ctx-prod")
	if err != nil {
		t.Fatalf("GetMeta: %v", err)
	}
	if got.Group != "production" {
		t.Errorf("group: got %q, want %q", got.Group, "production")
	}
	if got.Color != "red" {
		t.Errorf("color: got %q", got.Color)
	}
	if !got.Pinned {
		t.Error("expected pinned=true")
	}
	if got.Labels["env"] != "prod" {
		t.Errorf("label env: got %q", got.Labels["env"])
	}
}

func TestContextMeta_NotFound(t *testing.T) {
	st := openTestStore(t)
	m, err := st.GetMeta("nonexistent")
	if err != nil {
		t.Fatalf("GetMeta nonexistent: %v", err)
	}
	if m.Name != "" {
		t.Errorf("expected zero value, got %+v", m)
	}
}

func TestTouchLastUsed(t *testing.T) {
	st := openTestStore(t)
	if err := st.TouchLastUsed("ctx-a"); err != nil {
		t.Fatalf("TouchLastUsed: %v", err)
	}
	m, err := st.GetMeta("ctx-a")
	if err != nil {
		t.Fatalf("GetMeta: %v", err)
	}
	if m.LastUsedAt == nil {
		t.Error("expected LastUsedAt to be set")
	}
}

func TestSetGroup(t *testing.T) {
	st := openTestStore(t)
	if err := st.SetGroup("ctx-a", "team-alpha"); err != nil {
		t.Fatalf("SetGroup: %v", err)
	}
	m, _ := st.GetMeta("ctx-a")
	if m.Group != "team-alpha" {
		t.Errorf("expected group team-alpha, got %q", m.Group)
	}
}

func TestRenameContext(t *testing.T) {
	st := openTestStore(t)
	_ = st.SetGroup("old-name", "prod")
	if err := st.RenameContext("old-name", "new-name"); err != nil {
		t.Fatalf("RenameContext: %v", err)
	}
	m, _ := st.GetMeta("new-name")
	if m.Group != "prod" {
		t.Errorf("expected group prod after rename, got %q", m.Group)
	}
}

func TestAuditLog(t *testing.T) {
	st := openTestStore(t)
	for i := 0; i < 5; i++ {
		if err := st.LogAudit("switch", "ctx-a", "ctx-b", "ctx-a"); err != nil {
			t.Fatalf("LogAudit: %v", err)
		}
	}
	entries, err := st.AuditLog(3)
	if err != nil {
		t.Fatalf("AuditLog: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}
}

func TestUndoStack(t *testing.T) {
	st := openTestStore(t)
	if err := st.PushUndo("switch", `{"from":"ctx-a","to":"ctx-b"}`); err != nil {
		t.Fatalf("PushUndo: %v", err)
	}
	entry, err := st.PopUndo()
	if err != nil {
		t.Fatalf("PopUndo: %v", err)
	}
	if entry == nil {
		t.Fatal("expected undo entry, got nil")
	}
	if entry.Op != "switch" {
		t.Errorf("expected op=switch, got %q", entry.Op)
	}
	// Stack should be empty now.
	entry2, _ := st.PopUndo()
	if entry2 != nil {
		t.Error("expected empty stack after pop")
	}
}

func TestProfiles(t *testing.T) {
	st := openTestStore(t)
	p := store.Profile{
		Name:  "work",
		Paths: []string{"/home/user/.kube/work.yaml", "/home/user/.kube/eks.yaml"},
	}
	if err := st.SaveProfile(p); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}
	got, err := st.GetProfile("work")
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if len(got.Paths) != 2 {
		t.Errorf("expected 2 paths, got %d", len(got.Paths))
	}
}

func TestFavoriteNamespaces(t *testing.T) {
	st := openTestStore(t)
	_ = st.AddFavoriteNS("ctx-a", "default")
	_ = st.AddFavoriteNS("ctx-a", "kube-system")
	_ = st.AddFavoriteNS("ctx-a", "default") // duplicate should be ignored

	ns, err := st.FavoriteNamespaces("ctx-a")
	if err != nil {
		t.Fatalf("FavoriteNamespaces: %v", err)
	}
	if len(ns) != 2 {
		t.Errorf("expected 2 favourite namespaces, got %d", len(ns))
	}
	_ = st.RemoveFavoriteNS("ctx-a", "kube-system")
	ns, _ = st.FavoriteNamespaces("ctx-a")
	if len(ns) != 1 {
		t.Errorf("expected 1 after remove, got %d", len(ns))
	}
}
