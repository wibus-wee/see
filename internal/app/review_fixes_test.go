package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"see/internal/config"
	"see/internal/langserver"
	"see/internal/operr"
	"see/internal/proc"
)

// shortErr renders the persistent terminal failure of a spent restart
// budget in words a user can act on; the bare kind/status pair operr
// prints — "unexpected (persistent)" — names nothing and suggests no
// way back.
func TestShortErrExplainsBudgetExhaustion(t *testing.T) {
	failure := operr.New(operr.Unexpected, operr.Persistent, errors.New("gopls stopped"))
	failure.Operation = "language-server session"
	failure.Attempt = 3
	if failure.Error() != "unexpected (persistent)" {
		t.Fatalf("control: operr renders differently now: %q", failure.Error())
	}
	got := shortErr(failure)
	if got == failure.Error() || !strings.Contains(got, "crashed repeatedly") {
		t.Fatalf("budget exhaustion not explained: %q", got)
	}
	if !strings.Contains(got, "Retry") {
		t.Fatalf("the way back is not named: %q", got)
	}
	// A temporary unexpected failure keeps its raw rendering: it is not
	// terminal, so there is nothing to explain.
	transient := operr.New(operr.Unexpected, operr.Temporary, errors.New("hiccup"))
	if shortErr(transient) == shortErr(failure) {
		t.Fatal("temporary failure mislabeled as budget exhaustion")
	}
}

// lspFailure reports a persistent server failure with the server's name,
// not the bare error text.
func TestLspFailureNamesTheServerOnBudgetExhaustion(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\nvar x int\n"})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	h.open("a.go")
	failure := operr.New(operr.Unexpected, operr.Persistent, errors.New("stopped"))
	failure.Operation = "language-server session"
	h.a.lspFailure(failure)
	errmsg, isErr := h.feedbackError()
	if !isErr {
		t.Fatalf("a persistent server failure is not an error: %v", h.feedbackTexts())
	}
	if !strings.Contains(errmsg, "gopls") {
		t.Fatalf("the error does not name the server: %q", errmsg)
	}
	if !strings.Contains(errmsg, "crashed repeatedly") {
		t.Fatalf("the error does not explain: %q", errmsg)
	}
}

// A trust-denied launch reads as a denial in the failure note, never as
// the server's own failure text.
func TestLspFailureExplainsTrustDenial(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\nvar x int\n"})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	h.open("a.go")
	denial := operr.New(operr.PermissionDenied, operr.Permanent, proc.ErrUntrustedExecutable)
	h.a.lspFailure(denial)
	if !h.feedbackContains("never run") {
		t.Fatalf("denial not explained: %v", h.feedbackTexts())
	}
}

// The toolbar shows a failed server with its name and an explanation,
// and degradations are visible rather than rendered as Ready.
func TestToolbarRendersServerFailureAndDegraded(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\nvar x int\n"})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	h.open("a.go")
	failure := operr.New(operr.Unexpected, operr.Persistent, errors.New("stopped"))
	h.a.lspState, h.a.lspErr = langserver.Failed, failure
	h.tt.Frame()
	texts := strings.Join(h.tt.Texts(), "\n")
	if !strings.Contains(texts, "gopls") {
		t.Fatalf("toolbar failure does not name the server: %q", texts)
	}
	if strings.Contains(texts, "unexpected (persistent)") {
		t.Fatalf("toolbar shows the bare classification: %q", texts)
	}
	if !strings.Contains(texts, "crashed repeatedly") {
		t.Fatalf("toolbar failure unexplained: %q", texts)
	}
	// Degraded is its own state, visibly distinct from Ready.
	h.a.lspState, h.a.lspErr = langserver.Degraded, errors.New("hiccup")
	h.tt.Frame()
	texts = strings.Join(h.tt.Texts(), "\n")
	if !strings.Contains(texts, "Degraded") {
		t.Fatalf("degraded server rendered as ready: %q", texts)
	}
	h.a.lspState, h.a.lspErr = langserver.Ready, nil
	h.tt.Frame()
	if strings.Contains(strings.Join(h.tt.Texts(), "\n"), "Degraded") {
		t.Fatal("degraded state stuck after recovery")
	}
}

// Settings marks a denied server as denied rather than not installed:
// the binary the user configured is there; it is where it is that
// cannot run.
func TestSettingsRowMarksDeniedServers(t *testing.T) {
	root := writeTree(t, map[string]string{"main.rs": "fn main() {}\n", "myserver": "#!/bin/sh\n"})
	if err := os.Chmod(filepath.Join(root, "myserver"), 0o755); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, "")
	h.a.cfg.LSP = []config.LSPConfig{{
		Name:       "custom-lsp",
		Extensions: []string{".rs"},
		Command:    []string{filepath.Join(root, "myserver")},
	}}
	h.a.openWorkspace(root)
	h.settle(func() bool { d := h.a.dirs[h.a.root]; return d != nil && d.loaded })
	h.a.settings.pane = "intelligence"
	st := ui.NewTester(h.a.settingsView, 900, 680)
	texts := strings.Join(st.Texts(), "\n")
	if strings.Contains(texts, "custom-lsp · not installed") || !strings.Contains(texts, "Denied") {
		t.Fatalf("denied custom server mislabeled: %q", texts)
	}
	if !strings.Contains(texts, "never run") {
		t.Fatalf("denial unexplained: %q", texts)
	}
}
