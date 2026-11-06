package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"see/internal/tasks"
)

func TestTaskPanelLinksAndStop(t *testing.T) {
	root := writeTree(t, map[string]string{"a.txt": "first\nsecond\nthird", "b.go": "package b\n\nfunc broken() {}"})
	h := newHarness(t, root)
	h.open("a.txt")
	h.a.activeEditor().GoTo(editor.Pos{Line: 1, Col: 2})
	ctx, cancel := context.WithCancel(context.Background())
	run := &taskRun{kind: tasks.Build, output: &tasks.Output{}, cancel: cancel, status: "taskRunning"}
	defer cancel()
	h.a.task = run
	run.output.Write([]byte("b.go:3:6: example compile error\n"))
	h.a.showTasks()
	h.tt.Frame()
	if !h.tt.HasText("go build ./... — Running") {
		t.Fatalf("task panel missing: %v", h.tt.Texts())
	}
	if err := h.tt.Click(filepath.Join(h.a.root, "b.go") + ":3:6"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return h.a.activeEditor() != nil && h.a.activePath() == filepath.Join(h.a.root, "b.go") })
	if got := h.a.activeEditor().CursorPos(); got != (editor.Pos{Line: 2, Col: 5}) {
		t.Fatalf("location: %+v", got)
	}
	h.a.goBack()
	if h.a.activePath() != filepath.Join(h.a.root, "a.txt") || h.a.activeEditor().CursorPos() != (editor.Pos{Line: 1, Col: 2}) {
		t.Fatal("jump lost history")
	}
	h.tt.Frame()
	if err := h.tt.Click(commandLabel("Tasks: Stop")); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != context.Canceled {
		t.Fatal("Stop did not cancel run")
	}
	h.a.closeWorkspace()
	if h.a.task != nil {
		t.Fatal("workspace kept task")
	}
}

func TestGoBuildTrustAndExecution(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"go.mod": "module example.test/tasks\n\ngo 1.18\n", "a.go": "package main\n\nfunc main() {}\n"}))
	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyB)
	h.tt.Frame()
	if !h.a.trustAsk || h.a.task != nil {
		t.Fatal("untrusted workspace executed task")
	}
	h.a.cfg.Trust(h.a.root)
	h.a.trustAsk = false
	h.a.afterTrust = nil
	h.a.runGoTask(tasks.Build)
	first := h.a.task
	h.a.runGoTask(tasks.Test)
	if h.a.task != first {
		t.Fatal("started concurrent task")
	}
	h.settle(func() bool { return first.status != "taskRunning" })
	if first.status != "taskSucceeded" {
		t.Fatalf("build: %s %v", first.status, first.err)
	}
	if h.a.term.view != panelTasks {
		t.Fatal("task output not shown")
	}
}

func TestTaskLocation(t *testing.T) {
	root := t.TempDir()
	for _, line := range []string{"a.go:2:3: error", "  a.go:2:3: error"} {
		path, pos, ok := taskLocation(root, line)
		if !ok || path != filepath.Join(root, "a.go") || pos != (editor.Pos{Line: 1, Col: 2}) {
			t.Fatalf("%q: %q %+v %v", line, path, pos, ok)
		}
	}
	for _, line := range []string{"error only", "a.go:0:1: error", "a.go:1:0: error"} {
		if _, _, ok := taskLocation(root, line); ok {
			t.Fatalf("accepted %q", line)
		}
	}
}

func TestWorkspaceCloseCancelsTask(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "hello"}))
	cancelled := false
	h.a.task = &taskRun{status: "taskRunning", cancel: func() { cancelled = true }, output: &tasks.Output{}}
	h.a.closeWorkspace()
	if !cancelled || h.a.task != nil {
		t.Fatal("workspace close retained task process")
	}
}

func TestTaskSourceBasenameInSubpackage(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"sub/a_test.go": "package sub\n\nfunc example() {}"}))
	h.a.quick.files = []string{"sub/a_test.go"}
	path, pos, ok := taskLocation(h.a.root, "a_test.go:3:2: test failed")
	if !ok {
		t.Fatal("test location not recognized")
	}
	h.a.openTaskLocation(path, pos)
	h.settle(func() bool { return h.a.activeEditor() != nil })
	if h.a.activePath() != filepath.Join(h.a.root, "sub", "a_test.go") {
		t.Fatal("did not resolve unique subpackage filename")
	}
	h.a.quick.files = []string{"sub/a_test.go", "other/a_test.go"}
	before := h.a.activeEditor().Cursor()
	h.a.openTaskLocation(path, editor.Pos{})
	if !h.a.noticeError || h.a.activeEditor().Cursor() != before {
		t.Fatal("ambiguous source guessed a file")
	}
}
