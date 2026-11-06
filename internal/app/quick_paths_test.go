package app

import (
	"github.com/egoist/mygo/ui"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

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
	if len(h.a.quick.results) != 2 {
		t.Fatalf("runtime package results: %+v", h.a.quick.results)
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
