package git

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
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
	run("config", "commit.gpgsign", "false")
	name := ":odd name\nfile"
	if runtime.GOOS == "windows" {
		name = "odd name file"
	}
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
	if h := Head(ctx, root); h != "" {
		t.Fatalf("head without commits: %q", h)
	}
	run("add", "--", name)
	run("-c", "user.name=See", "-c", "user.email=see@example.com", "commit", "-q", "-m", "first")
	first := Head(ctx, root)
	if len(first) < 40 {
		t.Fatalf("head: %q", first)
	}
	run("-c", "user.name=See", "-c", "user.email=see@example.com", "commit", "-q", "--allow-empty", "-m", "second")
	if h := Head(ctx, root); h == first || len(h) < 40 {
		t.Fatalf("head after a commit: %q, was %q", h, first)
	}
}

// Sync reads in one status --porcelain=v2 --branch what the refresh
// used to ask of a rev-parse, a symbolic-ref, a status and the tracking
// and remote calls besides.
func TestSync(t *testing.T) {
	root, run := newRepo(t)
	ctx := context.Background()
	bare := t.TempDir()
	run("init", "-q", "--bare", bare)

	// No commits yet: HEAD is the initial branch's, not a commit's.
	write(t, root, "new", "n\n")
	s, err := Sync(ctx, root, false)
	if err != nil || s.Branch != "main" || s.Head != "" || len(s.Changes) != 1 || s.Changes[0].Index != '?' {
		t.Fatalf("unborn: %+v %v", s, err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if s.Root != filepath.Clean(resolvedRoot) {
		t.Fatalf("repository root = %q, want normalized path %q", s.Root, filepath.Clean(resolvedRoot))
	}

	run("add", "new")
	run("commit", "-qm", "first")
	run("remote", "add", "origin", bare)
	run("push", "-qu", "origin", "main")
	run("commit", "-qm", "second", "--allow-empty")

	// A staged rename, an unstaged edit, an untracked file and one
	// whose name holds spaces.
	run("mv", "new", "renamed")
	write(t, root, "edited", "e\n")
	run("add", "edited")
	write(t, root, "edited", "e\nmore\n")
	write(t, root, "with space.txt", "s\n")
	run("add", "with space.txt")

	s, err = Sync(ctx, root, true)
	if err != nil {
		t.Fatal(err)
	}
	r, b, changes, err := Read(ctx, root)
	if err != nil || s.Root != r || s.Branch != b || len(s.Changes) != len(changes) {
		t.Fatalf("sync against read: %+v vs %q %q %#v", s, r, b, changes)
	}
	for i, ch := range changes {
		if s.Changes[i] != ch {
			t.Fatalf("change %d: %+v vs %+v", i, s.Changes[i], ch)
		}
	}
	if s.Head != Head(ctx, root) || len(s.Head) != 40 {
		t.Fatalf("head %q vs %q", s.Head, Head(ctx, root))
	}
	up, ahead, behind := Tracking(ctx, root)
	if s.Upstream != up || s.Ahead != ahead || s.Behind != behind || up != "origin/main" {
		t.Fatalf("tracking %q ↑%d ↓%d vs %q ↑%d ↓%d", s.Upstream, s.Ahead, s.Behind, up, ahead, behind)
	}
	if s.Ahead != 1 || s.Behind != 0 {
		t.Fatalf("ahead/behind ↑%d ↓%d", s.Ahead, s.Behind)
	}
	if len(s.Remotes) != 1 || s.Remotes[0] != "origin" {
		t.Fatalf("remotes %v", s.Remotes)
	}
	if s.Web != RemoteWebURL(ctx, root, "origin") {
		t.Fatalf("web %q", s.Web)
	}
	if s.RefsErr != nil || s.Refs == "" {
		t.Fatalf("refs %q %v", s.Refs, s.RefsErr)
	}
	// The rename is one change with its old path, as v1 reported it.
	var rn *Change
	for i := range s.Changes {
		if s.Changes[i].Path == "renamed" {
			rn = &s.Changes[i]
		}
	}
	if rn == nil || rn.OldPath != "new" || rn.Index != 'R' || rn.Work != ' ' {
		t.Fatalf("rename: %#v", s.Changes)
	}
	// A name holding spaces comes back whole, not cut at one.
	found := false
	for _, ch := range s.Changes {
		if ch.Path == "with space.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a name with spaces: %#v", s.Changes)
	}
	// Line counts, staged and not, as LineCounts gives them.
	staged, unstaged := LineCounts(ctx, root)
	if len(s.Staged) != len(staged) || len(s.Unstaged) != len(unstaged) {
		t.Fatalf("counts %v %v vs %v %v", s.Staged, s.Unstaged, staged, unstaged)
	}

	// Detached HEAD shows the commit's short hash for a branch.
	run("checkout", "-q", "--detach", "HEAD")
	s, err = Sync(ctx, root, false)
	if err != nil || s.Branch != s.Head[:7] {
		t.Fatalf("detached: branch %q head %q", s.Branch, s.Head)
	}
}

func TestParseStatusRename(t *testing.T) {
	const unusual = ":odd name\nfile"
	got := ParseStatus([]byte("?? " + unusual + "\x00"))
	if len(got) != 1 || got[0].Path != unusual || got[0].Index != '?' {
		t.Fatalf("unusual path: %#v", got)
	}
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
