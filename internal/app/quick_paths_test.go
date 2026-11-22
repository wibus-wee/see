package app

import (
	"github.com/egoist/mygo/ui"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"see/internal/langserver"
	"see/internal/languages"
)

func TestQuickFileSymbolParser(t *testing.T) {
	for _, tc := range []struct {
		query, file, symbol string
		ok                  bool
	}{
		{"util@Help", "util", "Help", true},
		{"util@", "util", "", true},
		{"pkg/@types/x@Help", "pkg/@types/x", "Help", true},
		{"node_modules/@types/x", "", "", false},
		{"node_modules/@types", "node_modules/", "types", true},
		{"@Help", "", "", false},
		{"#util@Help", "", "", false},
		{">util@Help", "", "", false},
		{":2@Help", "", "", false},
		{"util", "", "", false},
		{"", "", "", false},
	} {
		t.Run(tc.query, func(t *testing.T) {
			file, symbol, ok := splitQuickFileSymbol(tc.query)
			if file != tc.file || symbol != tc.symbol || ok != tc.ok {
				t.Fatalf("got (%q, %q, %v)", file, symbol, ok)
			}
		})
	}
}

func TestQuickFileSymbolUnopenedGoFile(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, writeTree(t, map[string]string{
		"go.mod":      "module example.com/quick\n\ngo 1.21\n",
		"main.go":     "package main\nfunc main() {}\n",
		"pkg/util.go": "package pkg\n\nfunc Help() {}\nfunc Other() {}\n",
	}))
	h.a.lsp = langserver.New(langserver.Options{Name: "gopls", Root: h.a.root, Start: languages.StartGopls(testLogf(t))})
	defer h.a.lsp.StopWait(5 * time.Second)
	h.a.cfg.Trust(h.a.root)
	h.open("main.go")
	h.a.openQuickly()
	h.settle(func() bool { return h.a.quick.files != nil })
	h.tt.Type("util@Help")
	h.tt.Frame()
	path := filepath.Join(h.a.root, "pkg/util.go")
	h.settle(func() bool { d := h.a.docs[path]; return d != nil && !d.loading })
	h.settle(func() bool { return h.a.quick.fileLoaded || h.a.quick.symbolErr != nil })
	if h.a.quick.symbolErr != nil || len(h.a.quick.symbols) != 1 || h.a.quick.symbols[0].name != "Help" {
		t.Fatalf("symbols: %+v, error: %v", h.a.quick.symbols, h.a.quick.symbolErr)
	}
	if h.a.activePath() != filepath.Join(h.a.root, "main.go") || h.a.focus.has(path) {
		t.Fatal("symbol search opened a tab")
	}
	if dest := os.Getenv("SEE_QUICK_FILE_SYMBOL_SCREENSHOT"); dest != "" {
		f, err := os.Create(dest)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, h.tt.Image()); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	h.tt.Key(0, ui.KeyEnter)
	h.settle(func() bool { return h.a.activePath() == path && h.a.activeEditor() != nil })
	if h.a.quick.open || h.a.activeEditor().CursorPos().Line != 2 {
		t.Fatal("did not navigate to Help")
	}
	h.a.openQuickPanel("node_modules/@types/x")
	h.tt.Frame()
	if _, _, ok := splitQuickFileSymbol(h.a.quick.query); ok || len(h.a.quick.symbols) != 0 {
		t.Fatal("scoped package path entered symbol mode")
	}
}

func TestQuickOpenMatchesDirectoryAndFilenameTerms(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{
		"packages/runtime/src/index.ts":         "one\ntwo\nthree",
		"packages/eval/src/runtime-evidence.ts": "evidence",
		"other/runtime.ts":                      "excluded",
	}))
	h.a.openQuickly()
	h.settle(func() bool { return h.a.quick.files != nil })
	h.tt.Type("runtime package")
	h.tt.Frame()
	paths := make([]string, len(h.a.quick.results))
	for i, result := range h.a.quick.results {
		paths[i] = result.Path
	}
	slices.Sort(paths)
	if !slices.Equal(paths, []string{"packages/eval/src/runtime-evidence.ts", "packages/runtime/src/index.ts"}) {
		t.Fatalf("runtime package results: %v", paths)
	}
	if dest := os.Getenv("SEE_QUICK_PATH_SCREENSHOT"); dest != "" {
		f, err := os.Create(dest)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, h.tt.Image()); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	h.a.openQuickPanel("packages/runtime:2:2")
	h.tt.Frame()
	if len(h.a.quick.results) == 0 || h.a.quick.results[0].Path != "packages/runtime/src/index.ts" {
		t.Fatalf("path query results: %+v", h.a.quick.results)
	}
	h.tt.Key(0, ui.KeyEnter)
	h.settle(func() bool { return h.a.activeEditor() != nil })
	if h.a.activePath() != filepath.Join(h.a.root, "packages/runtime/src/index.ts") || h.a.activeEditor().CursorPos().Line != 1 || h.a.activeEditor().CursorPos().Col != 1 {
		t.Fatal("path query failed to open at requested position")
	}
}
