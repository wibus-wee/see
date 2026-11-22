package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestConflictHunksPreserveSurroundingBytes(t *testing.T) {
	text := "\xef\xbb\xbf<<<< HEAD\r\nours\r\n|||| base\r\nold\r\n====\r\ntheirs\r\n>>>> feature\r\n" +
		"kept\n<<<<<<< HEAD\n=======\nincoming only\n>>>>>>> feature\n" +
		"<<<<<<< HEAD\ncurrent only\n=======\n>>>>>>> feature\nno final newline"
	hunks, err := parseConflictHunks(text, 4)
	if err != nil || len(hunks) != 3 {
		t.Fatalf("hunks = %+v, %v", hunks, err)
	}
	if first := hunks[0]; first.Line != 1 || first.CurrentLabel != "HEAD" || first.IncomingLabel != "feature" {
		t.Fatalf("first hunk = %+v", first)
	}
	f := ConflictFile{text: text, Hunks: hunks}
	out, err := f.result([]ConflictChoice{KeepBoth, KeepIncoming, KeepCurrent})
	want := "\xef\xbb\xbfours\r\ntheirs\r\nkept\nincoming only\ncurrent only\nno final newline"
	if err != nil || out != want {
		t.Fatalf("result = %q, %v; want %q", out, err, want)
	}
	if _, err := f.result([]ConflictChoice{KeepCurrent, 0, KeepBoth}); err == nil {
		t.Fatal("accepted an undecided hunk")
	}
	out, err = f.result([]ConflictChoice{KeepCurrent, KeepCurrent, KeepIncoming})
	if want := "\xef\xbb\xbfours\r\nkept\nno final newline"; err != nil || out != want {
		t.Fatalf("choosing deletion produced %q, %v; want %q", out, err, want)
	}
}

func TestConflictHunksRejectPartialAndNestedMarkers(t *testing.T) {
	for _, text := range []string{
		"<<<<<<< HEAD\nours\n=======\ntheirs\n",
		"theirs\n>>>>>>> feature\n",
		"<<<<<<< HEAD\n<<<<<<<<<< inner\n=======\ntheirs\n>>>>>>> feature\n",
		"<<<<<<< HEAD\nours\n=======\ntheirs\n=======\n>>>>>>> feature\n",
	} {
		if _, err := parseConflictHunks(text, 7); !errors.Is(err, ErrConflictMarkers) {
			t.Errorf("%q: %v", text, err)
		}
	}
	if hunks, err := parseConflictHunks("Heading\n=======\nbody\n", 7); err != nil || len(hunks) != 0 {
		t.Fatalf("ordinary text parsed as a conflict: %+v, %v", hunks, err)
	}
}

func TestConflictKeepBothPreservesOneFileBOM(t *testing.T) {
	const bom = "\xef\xbb\xbf"
	text := "<<<<<<< HEAD\n" + bom + "current\n=======\n" + bom + "incoming\n>>>>>>> feature\n"
	hunks, err := parseConflictHunks(text, 7)
	if err != nil || len(hunks) != 1 {
		t.Fatalf("hunks = %+v, %v", hunks, err)
	}
	if got, want := hunks[0].Result(KeepBoth), bom+"current\nincoming\n"; got != want {
		t.Fatalf("Keep Both = %q; want %q", got, want)
	}
}

func TestResolveConflictStagesOnlyChosenFile(t *testing.T) {
	root, run := newRepo(t)
	run("config", "core.autocrlf", "false")
	write(t, root, ".gitattributes", "*.txt conflict-marker-size=4\n")
	const file, sibling = "conflict file.txt", "other.txt"
	for _, name := range []string{file, sibling} {
		write(t, root, name, "base\n")
	}
	run("add", ".")
	run("commit", "-qm", "base")
	run("switch", "-qc", "feature")
	for _, name := range []string{file, sibling} {
		write(t, root, name, "incoming\n")
	}
	run("commit", "-qam", "incoming")
	run("switch", "-q", "main")
	for _, name := range []string{file, sibling} {
		write(t, root, name, "current\n")
	}
	run("commit", "-qam", "current")
	ctx := context.Background()
	if _, err := Command(ctx, root, "merge", "feature"); err == nil {
		t.Fatal("merge did not conflict")
	}

	load := func() *ConflictFile {
		t.Helper()
		f, err := LoadConflict(ctx, root, file)
		if err != nil || len(f.Hunks) != 1 {
			t.Fatalf("load = %+v, %v", f, err)
		}
		return f
	}
	f := load()
	write(t, root, file, f.text+"external edit\n")
	if err := f.Resolve(ctx, []ConflictChoice{KeepBoth}); !errors.Is(err, ErrConflictStale) {
		t.Fatalf("apply stale worktree snapshot: %v", err)
	}
	f = load()
	if err := f.Resolve(ctx, []ConflictChoice{KeepBoth}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, file))
	if err != nil || string(got) != "current\nincoming\nexternal edit\n" {
		t.Fatalf("working file = %q, %v", got, err)
	}
	out, err := Command(ctx, root, "show", ":"+file)
	if err != nil || string(out) != string(got) {
		t.Fatalf("staged file = %q, %v", out, err)
	}
	if _, err := LoadConflict(ctx, root, file); !errors.Is(err, ErrConflictResolved) {
		t.Fatalf("path still unmerged: %v", err)
	}
	other, err := LoadConflict(ctx, root, sibling)
	if err != nil || len(other.Hunks) != 1 {
		t.Fatalf("sibling changed: %+v, %v", other, err)
	}
	write(t, root, sibling, "manual resolution\n")
	other, err = LoadConflict(ctx, root, sibling)
	if err != nil || len(other.Hunks) != 0 {
		t.Fatalf("manual resolution: %+v, %v", other, err)
	}
	run("add", "--", sibling)
	if err := other.Resolve(ctx, nil); !errors.Is(err, ErrConflictResolved) {
		t.Fatalf("apply stale index snapshot: %v", err)
	}
}
