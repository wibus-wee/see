package main

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/nav"
)

// waitTerm draws frames until the terminal's text holds want: its shell
// answers on goroutines of its own, which no queue of the harness carries.
func (h *harness) waitTerm(p *termPane, want string) {
	h.t.Helper()
	deadline := time.Now().Add(time.Minute) // a guard against a hang, never tuned
	for !strings.Contains(p.term.Text(), want) {
		if time.Now().After(deadline) {
			h.t.Fatalf("terminal never showed %q:\n%s", want, p.term.Text())
		}
		h.drainNow()
		time.Sleep(5 * time.Millisecond)
	}
}

func termHarness(t *testing.T) *harness {
	t.Setenv("SHELL", "/bin/sh")
	root := writeTree(t, map[string]string{"a.go": "package a\n", "b.go": "package b\n\nfunc B() {}\n"})
	h := newHarness(t, root)
	h.tt.Key(ui.Ctrl, ui.KeyBackquote)
	h.settle(func() bool { return h.a.term.active != nil || h.a.term.loadErr != nil })
	if err := h.a.term.loadErr; err != nil {
		t.Skip("no terminal library here:", err)
	}
	t.Cleanup(h.a.closeTerminals)
	return h
}

func TestTerminalPanel(t *testing.T) {
	h := termHarness(t)
	tp := &h.a.term
	h.tt.Frame() // the new terminal takes the keyboard as it shows
	if !tp.open || len(tp.tabs) != 1 || !h.tt.HasText("New Terminal") {
		t.Fatalf("open %v, tabs %d, texts %q", tp.open, len(tp.tabs), h.tt.Texts())
	}
	p := tp.active.active
	if !tp.focused {
		t.Error("the new terminal has no keyboard")
	}
	// The shell runs in the workspace.
	h.tt.Type("pwd; echo sum=$((40+2))")
	h.tt.Key(0, ui.KeyEnter)
	h.waitTerm(p, "sum=42")
	if !strings.Contains(p.term.Text(), h.a.root) {
		t.Errorf("not in the workspace:\n%s", p.term.Text())
	}
	// Split: two terminals side by side in the tab.
	h.tt.Key(ui.Cmd, ui.KeyBackslash)
	h.settle(func() bool { return len(tp.active.panes) == 2 })
	if len(h.a.groups()) != 1 {
		t.Error("Cmd+\\ in a terminal split the editors")
	}
	// A second tab.
	h.tt.Key(ui.Ctrl|ui.Shift, ui.KeyBackquote)
	h.settle(func() bool { return len(tp.tabs) == 2 })
	// A shell that exits closes its terminal, and its tab with it.
	q := tp.active.active
	h.tt.Type("exit")
	h.tt.Key(0, ui.KeyEnter)
	h.settle(func() bool { return len(tp.tabs) == 1 })
	if tp.active.active == q {
		t.Error("the exited terminal is still there")
	}
	// Hiding keeps the terminals running.
	h.tt.Key(ui.Ctrl, ui.KeyBackquote)
	if tp.open || len(tp.tabs) != 1 {
		t.Errorf("hidden: open %v, tabs %d", tp.open, len(tp.tabs))
	}
	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyY)
	if !tp.open {
		t.Error("Shift+Cmd+Y did not show it")
	}
	// Closing the folder ends every terminal.
	h.a.closeWorkspace()
	if len(tp.tabs) != 0 || tp.open {
		t.Errorf("after closing: tabs %d", len(tp.tabs))
	}
	select {
	case <-p.term.Done():
	case <-time.After(time.Minute):
		t.Error("the shell outlived its folder")
	}
}

func TestTerminalKeysStayInTheShell(t *testing.T) {
	h := termHarness(t)
	a := h.open("a.go")
	h.open("b.go")
	h.a.hist.Push(editorEntry(a, 0))
	h.a.term.focus = h.a.term.active.active
	h.tt.Frame()
	h.tt.Frame()
	if !h.a.term.focused {
		t.Fatal("the terminal has no keyboard")
	}
	active := h.a.activePath()
	h.tt.Key(ui.Ctrl, ui.KeyMinus)
	h.tt.Key(ui.Ctrl, ui.KeyTab)
	h.tt.Key(ui.Alt, ui.KeyLeft)
	if h.a.activePath() != active || len(h.a.hist.Back) != 1 {
		t.Errorf("the app took the shell's keys: active %q, back %v", h.a.activePath(), h.a.hist.Back)
	}
}

func TestTerminalLinksOpenInTheEditor(t *testing.T) {
	h := termHarness(t)
	p := h.a.term.active.active
	if !h.a.openTermLink(p, "b.go:3:6") {
		t.Fatal("a printed path was not opened")
	}
	b := h.a.docs[filepath.Join(h.a.root, "b.go")]
	h.settle(func() bool { return h.ed(b) != nil })
	if got := h.ed(b).CursorPos(); got != (editor.Pos{Line: 2, Col: 5}) {
		t.Errorf("at %v", got)
	}
	for _, link := range []string{"https://example.com/a.go", "missing.go:1", h.a.root} {
		if h.a.openTermLink(p, link) {
			t.Errorf("opened %q", link)
		}
	}
}

func TestTerminalScreenshot(t *testing.T) {
	dir := os.Getenv("SEE_SHOT")
	if dir == "" {
		t.Skip("set SEE_SHOT to a directory")
	}
	h := termHarness(t)
	h.tt.SetSize(1280, 800)
	p := h.a.term.active.active
	h.tt.Type("printf '\\033[32mok\\033[0m %s\\n' \"$PWD\"; ls")
	h.tt.Key(0, ui.KeyEnter)
	h.waitTerm(p, "b.go")
	h.a.userOpen(filepath.Join(h.a.root, "b.go"))
	h.settle(func() bool { return !h.a.docs[filepath.Join(h.a.root, "b.go")].loading })
	h.a.term.focus = p
	h.tt.Frame()
	h.tt.Key(ui.Cmd, ui.KeyD)
	h.settle(func() bool { return len(h.a.term.active.panes) == 2 })
	h.waitTerm(h.a.term.active.active, "$")
	h.tt.Frame()
	f, _ := os.Create(filepath.Join(dir, "terminal.png"))
	defer f.Close()
	png.Encode(f, h.tt.Image())
}

func editorEntry(d *doc, line int) nav.Entry { return nav.Entry{Path: d.path, Line: line} }
