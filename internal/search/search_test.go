package search

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"see/internal/proc"
)

func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, text := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(text), 0o644)
	}
	return root
}

// ripgrep finds rg for the test, which needs it installed (or SEE_RG).
func ripgrep(t *testing.T) string {
	t.Helper()
	rg, err := FindRipgrep("")
	if err != nil {
		t.Skip("ripgrep is not installed; set SEE_RG")
	}
	return rg
}

func run(t *testing.T, rg, root string, o Options) (map[string][]Match, Stats) {
	t.Helper()
	got := map[string][]Match{}
	st, err := Run(context.Background(), rg, root, o, nil, func(f File) {
		rel, _ := filepath.Rel(root, f.Path)
		got[filepath.ToSlash(rel)] = f.Matches
	})
	if err != nil {
		t.Fatal(err)
	}
	return got, st
}

func TestRipgrep(t *testing.T) {
	rg := ripgrep(t)
	root := tree(t, map[string]string{
		"a.go":               "package a\n\nfunc Hello() {}\r\n// hello again, HELLO\n",
		"sub/b.go":           "package sub // 你好 hello\n",
		"sub/b_test.go":      "hello\n",
		"node_modules/x.js":  "hello",
		"build/out.txt":      "hello",
		"bin.dat":            "hello\x00\x01",
		"notes/longline.txt": strings.Repeat("x", 300) + "hello" + strings.Repeat("y", 300) + "\n",
	})
	got, st := run(t, rg, root, Options{Query: "hello"})
	if len(got["a.go"]) != 3 || len(got["sub/b.go"]) != 1 || len(got["sub/b_test.go"]) != 1 || len(got["notes/longline.txt"]) != 1 {
		t.Fatalf("got %v", got)
	}
	for _, skipped := range []string{"node_modules/x.js", "build/out.txt", "bin.dat"} {
		if got[skipped] != nil {
			t.Errorf("searched %s", skipped)
		}
	}
	if st.Files != 4 || st.Matches != 6 {
		t.Errorf("stats %+v", st)
	}
	m := got["a.go"][0]
	if m.Line != 2 || m.Col != 5 || m.End != 10 || m.Preview[m.PreStart:m.PreEnd] != "Hello" {
		t.Errorf("match %+v", m)
	}
	b := got["sub/b.go"][0] // byte columns after Chinese text
	if b.Col != len("package sub // 你好 ") || b.Preview[b.PreStart:b.PreEnd] != "hello" {
		t.Errorf("b.go %+v", b)
	}
	l := got["notes/longline.txt"][0]
	if len(l.Preview) > 250 || !strings.HasPrefix(l.Preview, "…") || l.Preview[l.PreStart:l.PreEnd] != "hello" {
		t.Errorf("long %q %d %d", l.Preview, l.PreStart, l.PreEnd)
	}

	got, _ = run(t, rg, root, Options{Query: "Hello", CaseSensitive: true})
	if len(got["a.go"]) != 1 || got["sub/b.go"] != nil {
		t.Errorf("case: %v", got)
	}
	got, _ = run(t, rg, root, Options{Query: "hell", WholeWord: true})
	if len(got) != 0 {
		t.Errorf("whole word: %v", got)
	}
	got, _ = run(t, rg, root, Options{Query: `func \w+\(`, Regexp: true})
	if len(got) != 1 || len(got["a.go"]) != 1 {
		t.Errorf("regexp: %v", got)
	}
	got, _ = run(t, rg, root, Options{Query: "(", Regexp: false}) // literal, not a group
	if len(got) != 1 || got["a.go"][0].Col != 10 {
		t.Errorf("literal paren: %v", got)
	}
	got, _ = run(t, rg, root, Options{Query: "hello", Include: "*.go", Exclude: "*_test.go"})
	if len(got) != 2 || got["sub/b_test.go"] != nil {
		t.Errorf("globs: %v", got)
	}
	_, st = run(t, rg, root, Options{Query: "hello", MaxMatches: 2})
	if !st.Truncated || st.Matches != 2 {
		t.Errorf("limit: %+v", st)
	}
	if _, err := Run(context.Background(), rg, root, Options{Query: "[", Regexp: true}, nil, func(File) {}); err == nil || !strings.Contains(err.Error(), "unclosed character class") {
		t.Errorf("missing regexp error reason: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, rg, root, Options{Query: "hello"}, nil, func(File) {}); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled: %v", err)
	}
}

func TestListFiles(t *testing.T) {
	root := tree(t, map[string]string{
		"main.go": "", "internal/app/model.go": "", "internal/app/main_test.go": "",
		"docs/MainPage.md": "", "node_modules/m/main.js": "",
	})
	want := []string{"docs/MainPage.md", "internal/app/main_test.go", "internal/app/model.go", "main.go"}
	walked, _, err := ListFiles(context.Background(), "", root, 100, nil, nil, IgnoreFileOptions{})
	slices.Sort(walked)
	if err != nil || !slices.Equal(walked, want) {
		t.Errorf("walk: %v %v", walked, err)
	}
	if rg, err := FindRipgrep(""); err == nil {
		files, _, err := ListFiles(context.Background(), rg, root, 100, nil, nil, IgnoreFileOptions{})
		slices.Sort(files)
		if err != nil || !slices.Equal(files, want) {
			t.Errorf("rg: %v %v", files, err)
		}
		files, truncated, _ := ListFiles(context.Background(), rg, root, 2, nil, nil, IgnoreFileOptions{})
		if !truncated || len(files) != 2 {
			t.Errorf("limit: %v %v", files, truncated)
		}
	}
	files, truncated, err := ListFiles(context.Background(), "", root, 2, nil, nil, IgnoreFileOptions{})
	if err != nil || !truncated || len(files) != 2 {
		t.Errorf("walk limit: %v %v %v", files, truncated, err)
	}
	// A listing the limit fits is not truncated.
	files, truncated, err = ListFiles(context.Background(), "", root, 100, nil, nil, IgnoreFileOptions{})
	if err != nil || truncated || len(files) != len(want) {
		t.Errorf("walk full: %v %v %v", files, truncated, err)
	}
}

func TestFuzzy(t *testing.T) {
	files := []string{"docs/MainPage.md", "internal/app/main_test.go", "internal/app/model.go", "main.go", "cmd/domain/x.go"}
	got := Fuzzy(files, "main", 10)
	if len(got) < 3 || got[0].Path != "main.go" {
		t.Fatalf("ranked %v", got)
	}
	// Hits point at the matched bytes of the path.
	for _, s := range got {
		var b strings.Builder
		for _, i := range s.Hits {
			b.WriteByte(s.Path[i])
		}
		if !strings.EqualFold(b.String(), "main") {
			t.Errorf("%s hits %v spell %q", s.Path, s.Hits, b.String())
		}
	}
	if got := Fuzzy(files, "zzz", 10); len(got) != 0 {
		t.Errorf("no match: %v", got)
	}
	if len(Fuzzy(files, "", 2)) != 2 {
		t.Error("empty query lists")
	}
}

func TestListFilesWalkSkipsFolders(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"a.go", "out/b.go", "src/out/c.go"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, p), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, _, err := ListFiles(context.Background(), "", root, 100, func(rel string) bool { return rel == "out" }, nil, IgnoreFileOptions{})
	if err != nil || !slices.Equal(files, []string{"a.go", "src/out/c.go"}) {
		t.Fatalf("%q %v", files, err)
	}
}

// A case-variant spelling of a workspace ripgrep, and a symlink into the
// workspace, both name the workspace's bytes on a case-insensitive file
// system. The finder's exclusion is the launch boundary's identity
// check, so neither spelling may pass it — the exact-match comparison
// the exclusion used before caught neither.
func TestFindRipgrepDeniesWorkspaceVariants(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "MixedCaseRoot")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(root, "rg")
	if err := os.WriteFile(exe, []byte("workspace program"), 0o755); err != nil {
		t.Fatal(err)
	}
	// No PATH ripgrep may answer these probes.
	t.Setenv("PATH", t.TempDir())
	// verify.sh exports the machine's own rg as SEE_RG for the real-rg
	// integration tests, and SEE_RG precedes the explicit candidates of
	// a resolution — the override it exists as. These probes pass their
	// candidates explicitly, so the ambient override must not answer in
	// their place.
	t.Setenv("SEE_RG", "")

	// The exact spelling inside the workspace is denied — the control
	// that the exclusion still fires for the plain case.
	if got, err := FindRipgrep(root, exe); !errors.Is(err, proc.ErrUntrustedExecutable) {
		t.Fatalf("explicit workspace rg not denied: got %q, %v", got, err)
	}

	variant := filepath.Join(base, "mixedcaseroot", "rg")
	if _, err := os.Lstat(variant); err != nil {
		t.Skipf("filesystem is case-sensitive; the case-variant evasion does not exist here: %v", err)
	}
	if got, err := FindRipgrep(root, variant); !errors.Is(err, proc.ErrUntrustedExecutable) {
		t.Fatalf("case-variant workspace rg evaded the exclusion: got %q, %v", got, err)
	}

	// A symlink outside the workspace pointing inside it resolves to the
	// workspace's bytes; the user pointing at it gets the typed denial.
	alias := filepath.Join(t.TempDir(), "rg")
	if err := os.Symlink(exe, alias); err != nil {
		t.Skip(err)
	}
	if got, err := FindRipgrep(root, alias); !errors.Is(err, proc.ErrUntrustedExecutable) {
		t.Fatalf("symlink into the workspace evaded the exclusion: got %q, %v", got, err)
	}

	// A PATH-discovered copy inside the workspace is skipped silently,
	// not denied: the user never pointed at it.
	path := filepath.Join(base, "empty-path")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(exe, filepath.Join(path, "rg")); err != nil {
		t.Skip(err)
	}
	t.Setenv("PATH", path)
	if got, err := FindRipgrep(root); err == nil {
		t.Fatalf("PATH rg inside the workspace was used: %q", got)
	} else if errors.Is(err, proc.ErrUntrustedExecutable) {
		t.Fatalf("a PATH discovery the user never pointed at reported as a denial: %v", err)
	}
}
