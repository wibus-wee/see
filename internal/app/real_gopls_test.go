package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"see/internal/langserver"
	"see/internal/languages"
)

// TestRealGopls jumps across files with a real gopls, through Unicode
// before the symbol on both sides. (Größe名字 is exported, as 名字 alone
// would not be, which gopls rightly finds no definition for.) It runs when
// SEE_GOPLS names gopls:
//
//	SEE_GOPLS=$(which gopls) go test -run RealGopls ./internal/app
func TestRealGopls(t *testing.T) {
	if os.Getenv("SEE_GOPLS") == "" {
		t.Skip("set SEE_GOPLS to the path of gopls")
	}
	root := writeTree(t, map[string]string{
		"go.mod": "module example.com/w\n\ngo 1.21\n",
		"a.go":   "package main\n\nimport \"example.com/w/b\"\n\nfunc main() {\n\t_ = \"🎉中\"; b.Größe名字()\n}\n",
		"b/b.go": "package b\n\n// Größe名字 is 🎉.\nvar _ = \"😀\"; func Größe名字() {}\n",
	})
	h := newHarness(t, root)
	h.a.startLSP = languages.StartGopls // the real one, found through SEE_GOPLS
	h.a.lsp = langserver.New(langserver.Options{
		Name: "gopls", Root: h.a.root, Start: h.a.startLSP(testLogf(t)),
		OnState: func(s langserver.State, err error) {
			h.a.post.Post(func() { h.a.lspState, h.a.lspErr = s, err })
		},
	})
	defer h.a.lsp.StopWait(5e9)
	h.a.cfg.Trust(h.a.root)

	from := h.open("a.go")
	off := strings.Index(from.buf.String(), "b.Größe名字")
	if off < 0 {
		t.Fatal("the call is not in a.go")
	}
	h.a.goToDefinition(h.a.focus, from, off)
	target := filepath.Join(root, "b", "b.go")
	h.settle(func() bool { return h.a.activePath() == target })

	// Back returns to the call.
	h.a.goBack()
	h.settle(func() bool { return h.a.activePath() == filepath.Join(root, "a.go") })
}
