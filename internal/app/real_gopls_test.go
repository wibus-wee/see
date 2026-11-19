package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"see/internal/langserver"
	"see/internal/languages"
	"see/internal/lsp"
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
		"c.go":   "package main\n\ntype Speaker interface { Speak() }\ntype Person struct{}\nfunc (Person) Speak() {}\nvar person Person\n",
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
	off += len("b.")
	h.a.activeEditor().GoTo(from.buf.Pos(off))
	h.a.goToDefinition(h.a.focus, from, off)
	target := filepath.Join(root, "b", "b.go")
	h.settle(func() bool {
		d := h.a.docs[target]
		return h.a.activePath() == target && d != nil && !d.loading && h.a.activeEditor() != nil
	})
	targetDoc := h.a.docs[target]
	wantOffset := strings.Index(targetDoc.buf.String(), "Größe名字()")
	if wantOffset < 0 || h.a.activeEditor().Cursor() != wantOffset {
		t.Fatalf("definition cursor=%v want byte offset %d", h.a.activeEditor().CursorPos(), wantOffset)
	}

	// Back returns to the call.
	h.a.goBack()
	h.settle(func() bool { return h.a.activePath() == filepath.Join(root, "a.go") && h.a.activeEditor() != nil })
	if h.a.activeEditor().Cursor() != off {
		t.Fatalf("Go Back cursor=%v want %+v", h.a.activeEditor().CursorPos(), from.buf.Pos(off))
	}
	for _, step := range []struct{ kind, source, target string }{
		{"implementation", "Speak() }", "Speak() {}"},
		{"typeDefinition", "person Person", "Person struct"},
	} {
		d := h.open("c.go")
		offset := strings.Index(d.buf.String(), step.source)
		want := strings.Index(d.buf.String(), step.target)
		h.a.activeEditor().GoTo(d.buf.Pos(offset))
		h.a.goToLocation(h.a.focus, d, offset, step.kind)
		h.settle(func() bool { return !h.a.finding })
		if h.a.activeEditor().Cursor() != want {
			t.Fatalf("%s cursor=%v want byte offset %d; notice=%s", step.kind, h.a.activeEditor().CursorPos(), want, h.a.notice)
		}
	}
}

func TestRealGoplsQuickHelpHover(t *testing.T) {
	if os.Getenv("SEE_GOPLS") == "" {
		t.Skip("set SEE_GOPLS to the path of gopls")
	}
	root := writeTree(t, map[string]string{
		"go.mod":  "module example.com/quickhelp\n\ngo 1.21\n",
		"main.go": "package main\n\nfunc main() {\n\tanswer := Greeting(\"Ada\")\n\t_ = answer\n\t_ = LongHelp(\"reader\")\n}\n",
		"docs.go": "package main\n\n// Greeting returns a friendly welcome for the named visitor.\n//\n// The caller supplies a display name. Empty names are allowed.\n// Use this helper when presenting the first message in a session.\nfunc Greeting(name string) string {\n\treturn \"Hello, \" + name\n}\n\n// LongHelp explains a deliberately long documentation example.\n//\n// First, choose a descriptive name for the reader.\n//\n// Second, keep the declaration visible while reading the documentation.\n//\n// Third, scroll through this discussion without moving the editor.\n//\n// Fourth, the popover should retain its own scroll position.\n//\n// Fifth, keyboard activation should work at the same symbol.\n//\n// Sixth, declaration code can be copied using its Copy button.\n//\n// Seventh, the source location remains available at the end.\n//\n// Eighth, Escape should close the help without editing source.\n//\n// Ninth, clicking outside should return focus to the editor.\n//\n// Tenth, light and dark themes should remain readable.\n//\n// Final checkpoint: end of the long discussion.\nfunc LongHelp(name string) string {\n\treturn Greeting(name)\n}\n",
	})
	h := newHarness(t, root)
	h.a.startLSP = languages.StartGopls
	h.a.lsp = langserver.New(langserver.Options{
		Name: "gopls", Root: h.a.root, Start: h.a.startLSP(testLogf(t)),
	})
	defer h.a.lsp.StopWait(5 * time.Second)
	h.a.cfg.Trust(h.a.root)
	d := h.open("main.go")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := h.a.lsp.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	snap := d.buf.Snapshot()
	if err := h.a.lsp.Sync(ctx, client, d.uri, snap); err != nil {
		t.Fatal(err)
	}
	offset := strings.Index(d.buf.String(), "Greeting")
	if offset < 0 {
		t.Fatal("Greeting call not found")
	}
	pos := snap.Pos(offset + 2)
	line := snap.Line(pos.Line)
	md, _, err := client.Hover(ctx, d.uri, lsp.Position{
		Line: pos.Line, Character: lsp.ColumnToLSP(line, pos.Col, client.Encoding()),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("gopls Hover for Greeting: %q", md)
	sections := splitQuickHelp(md)
	if !strings.Contains(strings.ToLower(sections.summary), "friendly welcome") {
		t.Fatalf("Greeting summary = %q; hover Markdown = %q", sections.summary, md)
	}
}
