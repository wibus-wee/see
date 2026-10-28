package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/editor"

	"nsr/internal/lsp"
)

// TestRealGopls jumps across files with a real gopls, through Unicode
// before the symbol on both sides. (Größe名字: exported, as 名字 alone
// would not be, which gopls rightly finds no definition for.) It runs when NSR_GOPLS names gopls:
//
//	NSR_GOPLS=$(which gopls) go test -run RealGopls .
func TestRealGopls(t *testing.T) {
	if os.Getenv("NSR_GOPLS") == "" {
		t.Skip("set NSR_GOPLS to the path of gopls")
	}
	root := writeTree(t, map[string]string{
		"go.mod": "module example.com/w\n\ngo 1.21\n",
		"a.go":   "package main\n\nimport \"example.com/w/b\"\n\nfunc main() {\n\t_ = \"🎉中\"; b.Größe名字()\n}\n",
		"b/b.go": "package b\n\n// Größe名字 is 🎉.\nvar _ = \"😀\"; func Größe名字() {}\n",
	})
	h := newHarness(t, root)
	h.a.startLSP = startGopls // the real one, found through NSR_GOPLS
	h.a.lsp = newGoLSP(h.a.root, h.a.startLSP(testLogf(t)), func(s lspState, err error) {
		h.a.post(func() { h.a.lspState, h.a.lspErr = s, err })
	})
	defer h.a.lsp.stopWait(5e9)
	h.a.cfg.trust(h.a.root)

	a := h.open("a.go")
	off := strings.Index(a.buf.String(), "Größe名字()")
	h.ed(a).GoTo(a.buf.Pos(off))
	h.ed(a).OnDefinition(off)
	bPath := filepath.Join(h.a.root, "b", "b.go")
	h.settle(func() bool {
		d := h.a.docs[bPath]
		return !h.a.finding && d != nil && h.ed(d) != nil && h.a.activePath() == bPath || h.a.notice != ""
	})
	if h.a.notice != "" {
		t.Fatalf("notice %q (lsp %v %v), fake calls %v", h.a.notice, h.a.lspState, h.a.lspErr, h.fc.calls())
	}
	if h.a.lspState != lspReady {
		t.Errorf("lsp state %v, want ready", h.a.lspState)
	}
	if strings.Contains(strings.Join(h.fc.calls(), ","), "definition") {
		t.Error("the fake client answered, not gopls")
	}
	b := h.a.docs[bPath]
	// The name Größe名字 after `var _ = "😀"; func ` on line 3.
	want := editor.Pos{Line: 3, Col: strings.Index(b.buf.Line(3), "Größe名字")}
	if got := h.ed(b).CursorPos(); got != want {
		t.Errorf("landed at %v, want %v", got, want)
	}
	h.goBackAndCheck(a, a.buf.Pos(off))

	// The symbol's documentation, as a hover shows it.
	c, err := h.a.lsp.get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	line := a.buf.Line(5)
	col := strings.Index(line, "Größe名字")
	md, _, err := c.Hover(context.Background(), a.uri, lsp.Position{Line: 5, Character: lsp.ColumnToLSP(line, col, c.Encoding())})
	if err != nil || !strings.Contains(md, "func b.Größe名字()") {
		t.Errorf("hover %q, %v", md, err)
	}
}

func (h *harness) goBackAndCheck(d *doc, want editor.Pos) {
	h.t.Helper()
	h.a.goBack()
	h.tt.Frame()
	if h.a.activePath() != d.path || h.ed(d).CursorPos() != want {
		h.t.Errorf("back: %q at %v, want %v", h.a.activePath(), h.ed(d).CursorPos(), want)
	}
}
