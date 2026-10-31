package app

import (
	"github.com/egoist/mygo/ui"
	"path/filepath"
	"testing"
)

type testProjectWindow struct {
	shown, focused bool
	destroyed      bool
}

func (w *testProjectWindow) Show()             { w.shown = true }
func (w *testProjectWindow) Focus()            { w.focused = true }
func (w *testProjectWindow) Update(fn func())  { fn() }
func (w *testProjectWindow) IsDestroyed() bool { return w.destroyed }

func TestQuitAsksEachWindowWithUnsavedChanges(t *testing.T) {
	first := newHarness(t, writeTree(t, map[string]string{"a.txt": "a"}))
	second := newHarness(t, writeTree(t, map[string]string{"b.txt": "b"}))
	first.open("a.txt").buf.Set("first edit")
	second.open("b.txt").buf.Set("second edit")
	one, two := &testProjectWindow{}, &testProjectWindow{}
	done := false
	approveProjectQuit([]*projectWindow{{app: first.a, win: one}, {app: second.a, win: two}}, func() { done = true })
	if first.a.confirm == nil || second.a.confirm != nil || done {
		t.Fatal("first window was not asked first")
	}
	first.a.confirm.actions[1].fn()
	if second.a.confirm == nil || done || !two.focused {
		t.Fatal("second window was not asked")
	}
	second.a.confirm.actions[2].fn()
	if done {
		t.Fatal("cancelled quit")
	}
	approveProjectQuit([]*projectWindow{{app: first.a, win: one}, {app: second.a, win: two}}, func() { done = true })
	first.a.confirm.actions[0].fn()
	second.a.confirm.actions[1].fn()
	if !done {
		t.Fatal("quit did not finish after both decisions")
	}
}

func TestOpenProjectUsesNewWindowWhenWorkspaceOpen(t *testing.T) {
	root := writeTree(t, map[string]string{"a.txt": "a"})
	other := writeTree(t, map[string]string{"b.txt": "b"})
	h := newHarness(t, root)
	var opened string
	h.host.openProject = func(path string) bool { opened = path; return true }
	h.a.openProject(other)
	if opened != other || h.a.root != root {
		t.Fatalf("opened %q, original root %q", opened, h.a.root)
	}
	h.open("a.txt").buf.Set("unsaved")
	h.a.openRecent(other)
	if h.a.root != root || h.a.confirm != nil {
		t.Fatal("opening a new window interrupted the original workspace")
	}
	if filepath.Clean(opened) != filepath.Clean(other) {
		t.Fatalf("opened %q", opened)
	}
}

func TestNewWindowShortcut(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "a"}))
	count := 0
	h.host.openProject = func(string) bool { count++; return true }
	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyN)
	if count != 1 {
		t.Fatalf("new windows requested = %d", count)
	}
}

func TestNewWindowShortcutFromWelcome(t *testing.T) {
	h := newHarness(t, "")
	count := 0
	h.host.openProject = func(string) bool { count++; return true }
	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyN)
	welcome := ui.NewTester(h.a.welcomeView, welcomeWidth, welcomeHeight)
	welcome.Key(ui.Cmd|ui.Shift, ui.KeyN)
	if count != 2 {
		t.Fatalf("new windows requested = %d", count)
	}
}
