package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui/editor"
)

func TestGitStatusAndStageInNewRepository(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	run := func(args ...string) {
		t.Helper()
		if out, err := gitCommand(ctx, root, args...); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	run("init", "-q")
	name := ":odd name\nfile"
	if err := os.WriteFile(filepath.Join(root, name), []byte("hello\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, changes, err := readGit(ctx, root)
	if err != nil || len(changes) != 1 || changes[0].path != name || changes[0].index != '?' {
		t.Fatalf("untracked: %#v %v", changes, err)
	}
	run("add", "--", name)
	_, _, changes, err = readGit(ctx, root)
	if err != nil || len(changes) != 1 || changes[0].index != 'A' {
		t.Fatalf("staged: %#v %v", changes, err)
	}
	run("reset", "-q", "--", name)
	_, _, changes, err = readGit(ctx, root)
	if err != nil || len(changes) != 1 || changes[0].index != '?' {
		t.Fatalf("unstaged: %#v %v", changes, err)
	}
}

func TestDiffFollowsEditorPreferences(t *testing.T) {
	h := newHarness(t, "")
	h.a.git.diff = &editor.DiffState{}
	h.a.cfg.FontFamily = "Test Mono"
	h.a.cfg.FontSize = 21
	h.a.cfg.SoftWrap = true
	h.a.cfg.LineNumbers = int(editor.LineNumbersOff)
	h.a.applyEditorPrefsAll()
	d := h.a.git.diff
	if d.Font.Family != "Test Mono" || d.Font.Size != 21 || !d.Wrap || !d.NoLineNumbers {
		t.Fatalf("diff preferences: font=%+v wrap=%v no numbers=%v", d.Font, d.Wrap, d.NoLineNumbers)
	}
	h.a.cfg.FontSize = 12
	h.a.cfg.SoftWrap = false
	h.a.cfg.LineNumbers = int(editor.LineNumbersOn)
	h.a.applyEditorPrefsAll()
	if d.Font.Size != 12 || d.Wrap || d.NoLineNumbers {
		t.Fatalf("updated preferences: %+v", d.Font)
	}
}

func TestParseGitStatusRename(t *testing.T) {
	changes := parseGitStatus([]byte("R  new name\x00old name\x00 M another\x00"))
	if len(changes) != 2 || changes[0].path != "new name" || changes[0].oldPath != "old name" || changes[1].work != 'M' {
		t.Fatalf("changes: %#v", changes)
	}
}

func TestGitDecorations(t *testing.T) {
	root := t.TempDir()
	decor := gitDecorations(root, []gitChange{
		{path: "pkg/new.go", index: '?', work: '?'},
		{path: "pkg/edit.go", index: ' ', work: 'M'},
	})
	if decor[filepath.Join(root, "pkg", "new.go")] != 'U' || decor[filepath.Join(root, "pkg", "edit.go")] != 'M' || decor[filepath.Join(root, "pkg")] != 'M' {
		t.Fatalf("decorations: %#v", decor)
	}
}
