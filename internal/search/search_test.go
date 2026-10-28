package search

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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

// ripgrep finds rg for the test, which needs it installed (or NSR_RG).
func ripgrep(t *testing.T) string {
	t.Helper()
	rg, err := FindRipgrep("")
	if err != nil {
		t.Skip("ripgrep is not installed; set NSR_RG")
	}
	return rg
}

func run(t *testing.T, rg, root string, o Options) (map[string][]Match, Stats) {
	t.Helper()
	got := map[string][]Match{}
	st, err := Run(context.Background(), rg, root, o, func(f File) {
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
	if _, err := Run(context.Background(), rg, root, Options{Query: "(", Regexp: true}, func(File) {}); err == nil {
		t.Error("bad regexp accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, rg, root, Options{Query: "hello"}, func(File) {}); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled: %v", err)
	}
}

func TestListFiles(t *testing.T) {
	root := tree(t, map[string]string{
		"main.go": "", "internal/app/model.go": "", "internal/app/main_test.go": "",
		"docs/MainPage.md": "", "node_modules/m/main.js": "",
	})
	want := []string{"docs/MainPage.md", "internal/app/main_test.go", "internal/app/model.go", "main.go"}
	walked, _, err := ListFiles(context.Background(), "", root, 100)
	slices.Sort(walked)
	if err != nil || !slices.Equal(walked, want) {
		t.Errorf("walk: %v %v", walked, err)
	}
	if rg, err := FindRipgrep(""); err == nil {
		files, _, err := ListFiles(context.Background(), rg, root, 100)
		slices.Sort(files)
		if err != nil || !slices.Equal(files, want) {
			t.Errorf("rg: %v %v", files, err)
		}
		files, truncated, _ := ListFiles(context.Background(), rg, root, 2)
		if !truncated || len(files) != 2 {
			t.Errorf("limit: %v %v", files, truncated)
		}
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
