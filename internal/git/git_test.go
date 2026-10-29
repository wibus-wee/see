package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReadNewRepository(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	run := func(args ...string) {
		t.Helper()
		if out, err := Command(ctx, root, args...); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	run("init", "-q")
	name := ":odd name\nfile"
	if err := os.WriteFile(filepath.Join(root, name), []byte("hello\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, changes, err := Read(ctx, root)
	if err != nil || len(changes) != 1 || changes[0].Path != name || changes[0].Index != '?' {
		t.Fatalf("untracked: %#v %v", changes, err)
	}
	run("add", "--", name)
	_, _, changes, err = Read(ctx, root)
	if err != nil || len(changes) != 1 || changes[0].Index != 'A' {
		t.Fatalf("staged: %#v %v", changes, err)
	}
	run("reset", "-q", "--", name)
	_, _, changes, err = Read(ctx, root)
	if err != nil || len(changes) != 1 || changes[0].Index != '?' {
		t.Fatalf("unstaged: %#v %v", changes, err)
	}
}

func TestParseStatusRename(t *testing.T) {
	changes := ParseStatus([]byte("R  new name\x00old name\x00 M another\x00"))
	if len(changes) != 2 || changes[0].Path != "new name" || changes[0].OldPath != "old name" || changes[1].Work != 'M' {
		t.Fatalf("changes: %#v", changes)
	}
}

func TestDecorations(t *testing.T) {
	root := t.TempDir()
	decor := Decorations(root, []Change{
		{Path: "pkg/new.go", Index: '?', Work: '?'},
		{Path: "pkg/edit.go", Index: ' ', Work: 'M'},
	})
	if decor[filepath.Join(root, "pkg", "new.go")] != 'U' || decor[filepath.Join(root, "pkg", "edit.go")] != 'M' || decor[filepath.Join(root, "pkg")] != 'M' {
		t.Fatalf("decorations: %#v", decor)
	}
}

func TestStatus(t *testing.T) {
	for ch, want := range map[Change]byte{
		{Index: '?', Work: '?'}: 'U',
		{Index: 'U', Work: 'U'}: '!',
		{Index: 'A', Work: 'A'}: '!',
		{Index: 'D', Work: 'D'}: '!',
		{Index: 'M', Work: 'D'}: 'D',
		{Index: 'A', Work: ' '}: 'A',
	} {
		if got := ch.Status(); got != want {
			t.Errorf("%c%c: %c, want %c", ch.Index, ch.Work, got, want)
		}
	}
}
