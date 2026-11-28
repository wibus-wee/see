package app

import (
	"context"
	"path/filepath"
	"testing"

	"see/internal/config"
	"see/internal/langserver"
	"see/internal/lsp"
)

// aConfiguredWorkspace returns a harness whose config names one language
// server for ".rs" files. Its command is a helper that never starts a
// real server, so the test exercises See's wiring only.
func aConfiguredWorkspace(t *testing.T, root string) *harness {
	t.Helper()
	h := newHarness(t, "")
	h.a.cfg.LSP = []config.LSPConfig{{
		Name:       "custom-lsp",
		Extensions: []string{".rs"},
		Languages:  []string{"rust"},
		Command:    []string{"see-test-lsp", "serve"},
	}}
	h.a.openWorkspace(root)
	h.settle(func() bool { d := h.a.dirs[h.a.root]; return d != nil && d.loaded })
	return h
}

func TestServerForResolvesConfiguredServer(t *testing.T) {
	root := writeTree(t, map[string]string{"main.rs": "fn main() {}\n", "a.go": "package a\n"})
	h := aConfiguredWorkspace(t, root)
	m := h.a.serverFor(filepath.Join(root, "main.rs"))
	if m == nil {
		t.Fatal("no server for a configured extension")
	}
	if m == h.a.lsp || m == h.a.clangLSP || m == h.a.tsLSP || m == h.a.cmakeLSP {
		t.Fatal("a built-in server claimed a configured extension")
	}
	// A file a built-in server covers stays with it.
	if got := h.a.serverFor(filepath.Join(root, "a.go")); got != h.a.lsp {
		t.Fatalf("a.go resolved to %p, want gopls", got)
	}
}

func TestUntrustedWorkspaceDoesNotStartConfiguredServer(t *testing.T) {
	root := writeTree(t, map[string]string{"main.rs": "fn main() {}\n"})
	h := aConfiguredWorkspace(t, root)
	m := h.a.serverFor(filepath.Join(root, "main.rs"))
	if m == nil {
		t.Fatal("no server for a configured extension")
	}
	h.open("main.rs")
	h.settle(func() bool { return h.a.trustAsk })
	if state, _ := m.Status(); state != langserver.Off {
		t.Fatalf("server ran in an untrusted workspace: %v", state)
	}
	if h.a.lsp.Current() != nil {
		t.Fatal("built-in server started")
	}
	// Swap in a client-less starter before trusting, so the server the
	// document opens is a fake rather than a process See cannot run.
	m.Stop()
	h.a.customLSPs[h.a.customIndex(m)] = langserver.New(langserver.Options{
		Name: "custom-lsp", Root: root,
		Start: func(ctx context.Context, _ string, _ []lsp.WorkspaceFolder, _ func(error)) (langserver.Client, error) {
			return h.fc, nil
		},
	})
	m = h.a.serverFor(filepath.Join(root, "main.rs"))
	h.a.trust()
	h.settle(func() bool { return m.Current() != nil })
	if state, _ := m.Status(); state != langserver.Ready {
		t.Fatalf("server never became ready: %v", state)
	}
}

func TestConfiguredServerThatFailsToStartReportsFailed(t *testing.T) {
	root := writeTree(t, map[string]string{"main.rs": "fn main() {}\n"})
	h := newHarness(t, "")
	h.a.cfg.LSP = []config.LSPConfig{{
		Name:       "custom-lsp",
		Extensions: []string{".rs"},
		Command:    []string{"see-test-lsp"},
	}}
	h.a.openWorkspace(root)
	h.settle(func() bool { d := h.a.dirs[h.a.root]; return d != nil && d.loaded })
	m := h.a.serverFor(filepath.Join(root, "main.rs"))
	if m == nil {
		t.Fatal("no server for a configured extension")
	}
	h.a.cfg.Trust(root)
	// Replace its starter with one that fails, as a missing binary would.
	m.Stop()
	h.a.customLSPs[h.a.customIndex(m)] = langserver.New(langserver.Options{
		Name: "custom-lsp", Root: root,
		Start: func(ctx context.Context, _ string, _ []lsp.WorkspaceFolder, _ func(error)) (langserver.Client, error) {
			return nil, errNoServer
		},
	})
	m = h.a.serverFor(filepath.Join(root, "main.rs"))
	h.open("main.rs")
	h.settle(func() bool {
		s, _ := m.Status()
		return s == langserver.Failed
	})
	// The toolbar surfaces the failure under the server's name.
	if a := h.a; a.customServerFor(a.activePath()) != m {
		t.Fatal("active file did not resolve to the failed server")
	}
}

var errNoServer = errorString("custom-lsp is not installed")

type errorString string

func (e errorString) Error() string { return string(e) }
