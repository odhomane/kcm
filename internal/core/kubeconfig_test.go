package core_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odhomane/kcm/internal/core"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestLoadFile_Valid(t *testing.T) {
	path := writeFixture(t, validKubeconfig)
	cfg, err := core.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(cfg.Contexts) != 2 {
		t.Errorf("expected 2 contexts, got %d", len(cfg.Contexts))
	}
	if cfg.CurrentContext != "ctx-a" {
		t.Errorf("expected current-context ctx-a, got %q", cfg.CurrentContext)
	}
}

func TestLoadFile_Invalid(t *testing.T) {
	path := writeFixture(t, `not: valid: yaml: [[[`)
	_, err := core.LoadFile(path)
	if err == nil {
		t.Fatal("expected error for malformed YAML, got nil")
	}
}

func TestWriteFile_AtomicRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kube.yaml")
	cfg := fixtureConfig()
	if err := core.WriteFile(path, cfg); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Temp file must be gone.
	if _, err := os.Stat(path + ".kcm.tmp"); !os.IsNotExist(err) {
		t.Error("temp file still exists after atomic write")
	}
	// Round-trip read.
	loaded, err := core.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile after write: %v", err)
	}
	if _, ok := loaded.Contexts["ctx-a"]; !ok {
		t.Error("ctx-a not found after round-trip")
	}
}

func TestMergeConfigs_NoCollision(t *testing.T) {
	dst := clientcmdapi.NewConfig()
	dst.Clusters["cluster-a"] = &clientcmdapi.Cluster{Server: "https://a.example.com"}
	dst.Contexts["ctx-a"] = &clientcmdapi.Context{Cluster: "cluster-a"}

	src := clientcmdapi.NewConfig()
	src.Clusters["cluster-b"] = &clientcmdapi.Cluster{Server: "https://b.example.com"}
	src.Contexts["ctx-b"] = &clientcmdapi.Context{Cluster: "cluster-b"}

	warnings := core.MergeConfigs(dst, src)
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if len(dst.Contexts) != 2 {
		t.Errorf("expected 2 contexts after merge, got %d", len(dst.Contexts))
	}
}

func TestMergeConfigs_CollisionSkipped(t *testing.T) {
	dst := clientcmdapi.NewConfig()
	dst.Contexts["ctx-a"] = &clientcmdapi.Context{Cluster: "cluster-a"}

	src := clientcmdapi.NewConfig()
	src.Contexts["ctx-a"] = &clientcmdapi.Context{Cluster: "cluster-b"} // collision

	warnings := core.MergeConfigs(dst, src)
	if len(warnings) == 0 {
		t.Error("expected collision warning, got none")
	}
	// Original value preserved.
	if dst.Contexts["ctx-a"].Cluster != "cluster-a" {
		t.Error("collision overwrote original context")
	}
}

func TestExtractContext(t *testing.T) {
	path := writeFixture(t, validKubeconfig)
	cfg, _ := core.LoadFile(path)

	mini, err := core.ExtractContext(cfg, "ctx-b")
	if err != nil {
		t.Fatalf("ExtractContext: %v", err)
	}
	if len(mini.Contexts) != 1 {
		t.Errorf("expected 1 context in extracted config, got %d", len(mini.Contexts))
	}
	if mini.CurrentContext != "ctx-b" {
		t.Errorf("expected current-context ctx-b, got %q", mini.CurrentContext)
	}
}

func TestManager_AllContexts(t *testing.T) {
	p1 := writeFixture(t, validKubeconfig)
	p2 := writeFixture(t, secondKubeconfig)

	mgr := core.NewManager()
	if err := mgr.Load(p1); err != nil {
		t.Fatalf("Load p1: %v", err)
	}
	if err := mgr.Load(p2); err != nil {
		t.Fatalf("Load p2: %v", err)
	}

	contexts := mgr.AllContexts()
	if len(contexts) != 3 {
		t.Errorf("expected 3 contexts across two files, got %d", len(contexts))
	}
}

func TestManager_FindContext(t *testing.T) {
	p := writeFixture(t, validKubeconfig)
	mgr := core.NewManager()
	_ = mgr.Load(p)

	nc, name, err := mgr.FindContext("ctx-b")
	if err != nil {
		t.Fatalf("FindContext: %v", err)
	}
	if name != "ctx-b" || nc == nil {
		t.Error("FindContext returned wrong result")
	}

	_, _, err = mgr.FindContext("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent context")
	}
}

func TestValidate_Clean(t *testing.T) {
	cfg := fixtureConfig()
	issues := core.Validate("test.yaml", cfg)
	for _, i := range issues {
		if i.Severity == "error" {
			t.Errorf("unexpected error: %s", i.Message)
		}
	}
}

func TestValidate_OrphanedCluster(t *testing.T) {
	cfg := fixtureConfig()
	cfg.Clusters["orphan"] = &clientcmdapi.Cluster{Server: "https://orphan.example.com"}
	issues := core.Validate("test.yaml", cfg)
	found := false
	for _, i := range issues {
		if i.Severity == "warning" {
			found = true
		}
	}
	if !found {
		t.Error("expected warning for orphaned cluster")
	}
}

func TestRedactConfig(t *testing.T) {
	cfg := fixtureConfig()
	cfg.AuthInfos["user-a"].Token = "super-secret-token"
	cfg.AuthInfos["user-a"].Password = "hunter2"

	redacted := core.RedactConfig(cfg)
	if u, ok := redacted.AuthInfos["user-a"]; ok {
		if u.Token != "" && u.Token != "<redacted>" {
			t.Errorf("token not redacted: %q", u.Token)
		}
		if u.Password != "" && u.Password != "<redacted>" {
			t.Errorf("password not redacted: %q", u.Password)
		}
	}
}

// ─── fixtures ─────────────────────────────────────────────────────────────────

const validKubeconfig = `
apiVersion: v1
kind: Config
current-context: ctx-a
clusters:
- name: cluster-a
  cluster:
    server: https://a.example.com
- name: cluster-b
  cluster:
    server: https://b.example.com
users:
- name: user-a
  user:
    token: ""
- name: user-b
  user:
    token: ""
contexts:
- name: ctx-a
  context:
    cluster: cluster-a
    user: user-a
    namespace: default
- name: ctx-b
  context:
    cluster: cluster-b
    user: user-b
    namespace: staging
`

const secondKubeconfig = `
apiVersion: v1
kind: Config
clusters:
- name: cluster-c
  cluster:
    server: https://c.example.com
users:
- name: user-c
  user:
    token: ""
contexts:
- name: ctx-c
  context:
    cluster: cluster-c
    user: user-c
`

func fixtureConfig() *clientcmdapi.Config {
	cfg := clientcmdapi.NewConfig()
	cfg.Clusters["cluster-a"] = &clientcmdapi.Cluster{Server: "https://a.example.com"}
	cfg.AuthInfos["user-a"] = &clientcmdapi.AuthInfo{}
	cfg.Contexts["ctx-a"] = &clientcmdapi.Context{Cluster: "cluster-a", AuthInfo: "user-a", Namespace: "default"}
	cfg.CurrentContext = "ctx-a"
	return cfg
}

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}
