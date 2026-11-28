package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/editor"

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
			t.Fatalf("%s cursor=%v want byte offset %d; feedback=%v", step.kind, h.a.activeEditor().CursorPos(), want, h.feedbackTexts())
		}
	}
}

func TestRealGoplsDocumentHighlights(t *testing.T) {
	if os.Getenv("SEE_GOPLS") == "" {
		t.Skip("set SEE_GOPLS to the path of gopls")
	}
	root := writeTree(t, map[string]string{
		"go.mod":  "module example.com/scope\n\ngo 1.21\n",
		"main.go": "package main\n\nimport \"fmt\"\n\nfunc main() {\n\t名称 := \"hello\"\n\tfmt.Println(\"🎉\", 名称)\n\t{\n\t\t名称 := \"shadow\"\n\t\tfmt.Println(名称)\n\t}\n\tfmt.Println(名称)\n}\n",
	})
	h := newHarness(t, root)
	h.a.startLSP = languages.StartGopls
	h.a.lsp = langserver.New(langserver.Options{
		Name: "gopls", Root: h.a.root, Start: h.a.startLSP(testLogf(t)),
	})
	defer h.a.lsp.StopWait(5 * time.Second)
	h.a.cfg.Trust(h.a.root)
	d := h.open("main.go")
	name := "名称"
	offsets := scopeOffsets(d.buf.String(), name)
	outer := []int{offsets[0], offsets[1], offsets[4]}
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(offsets[1]))
	h.tt.Frame()
	h.clearFeedback()
	h.a.editAllInScope()
	h.settle(func() bool { return len(st.Selections()) == 3 || len(h.feedback) > 0 })
	want := []editor.Selection{
		{Anchor: outer[0], Head: outer[0] + len(name)},
		{Anchor: outer[1], Head: outer[1] + len(name)},
		{Anchor: outer[2], Head: outer[2] + len(name)},
	}
	if !slices.Equal(st.Selections(), want) {
		t.Fatalf("gopls selections = %+v, want %+v; feedback=%v", st.Selections(), want, h.feedbackTexts())
	}
	if primary := st.Selection(); primary.Anchor != outer[1] || primary.Head != outer[1]+len(name) {
		t.Fatalf("gopls primary selection = %+v, want occurrence at %d", primary, outer[1])
	}
	for _, selection := range st.Selections() {
		if selection.Start() == offsets[2] || selection.Start() == offsets[3] {
			t.Fatalf("gopls selected a shadowed occurrence: %+v", selection)
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

// TestRealGoplsCallHierarchy asks a real gopls for the calls of a Go
// function, through the same prepare + incomingCalls pair the panel
// runs. It runs when SEE_GOPLS names gopls.
func TestRealGoplsTypeHierarchy(t *testing.T) {
	if os.Getenv("SEE_GOPLS") == "" {
		t.Skip("set SEE_GOPLS to the path of gopls")
	}
	root := writeTree(t, map[string]string{
		"go.mod":  "module example.com/types\n\ngo 1.21\n",
		"main.go": "package main\n\ntype Reader interface { Read() }\ntype Concrete struct{}\nfunc (Concrete) Read() {}\nfunc main() {}\n",
	})
	h := newHarness(t, root)
	h.a.startLSP = languages.StartGopls
	h.a.lsp = langserver.New(langserver.Options{Name: "gopls", Root: h.a.root, Start: h.a.startLSP(testLogf(t))})
	defer h.a.lsp.StopWait(5 * time.Second)
	d := h.open("main.go")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c, err := h.a.lsp.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.a.lsp.Sync(ctx, c, d.uri, d.buf.Snapshot()); err != nil {
		t.Fatal(err)
	}
	th, ok := c.(typeHierarchyClient)
	if !ok || !th.HasTypeHierarchy() {
		t.Fatal("gopls has no type hierarchy")
	}
	items, err := th.PrepareTypeHierarchy(ctx, d.uri, lsp.Position{Line: 2, Character: 6})
	if err != nil || len(items) != 1 || items[0].Name != "Reader" {
		t.Fatalf("prepare: %+v %v", items, err)
	}
	subs, err := th.Subtypes(ctx, items[0])
	if err != nil || len(subs) != 1 || subs[0].Name != "Concrete" {
		t.Fatalf("subtypes: %+v %v", subs, err)
	}
	supers, err := th.Supertypes(ctx, subs[0])
	if err != nil || len(supers) != 1 || supers[0].Name != "Reader" {
		t.Fatalf("supertypes: %+v %v", supers, err)
	}
}

func TestRealGoplsCallHierarchy(t *testing.T) {
	if os.Getenv("SEE_GOPLS") == "" {
		t.Skip("set SEE_GOPLS to the path of gopls")
	}
	root := writeTree(t, map[string]string{
		"go.mod":  "module example.com/calls\n\ngo 1.21\n",
		"main.go": "package main\n\nfunc main() {\n\thelper()\n}\n\nfunc helper() int {\n\treturn 1\n}\n",
	})
	h := newHarness(t, root)
	h.a.startLSP = languages.StartGopls
	h.a.lsp = langserver.New(langserver.Options{
		Name: "gopls", Root: h.a.root, Start: h.a.startLSP(testLogf(t)),
	})
	defer h.a.lsp.StopWait(5 * time.Second)
	d := h.open("main.go")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := h.a.lsp.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	snap := d.buf.Snapshot()
	if err := h.a.lsp.Sync(ctx, client, d.uri, snap); err != nil {
		t.Fatal(err)
	}
	cc, ok := client.(callClient)
	if !ok || !cc.HasCallHierarchy() {
		t.Fatalf("gopls has no call hierarchy: %v %T", ok, client)
	}
	offset := strings.Index(d.buf.String(), "func helper")
	pos := snap.Pos(offset + len("func he"))
	items, err := cc.PrepareCallHierarchy(ctx, d.uri, lsp.Position{
		Line: pos.Line, Character: lsp.ColumnToLSP(snap.Line(pos.Line), pos.Col, client.Encoding()),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "helper" {
		t.Fatalf("prepare = %+v", items)
	}
	in, err := cc.IncomingCalls(ctx, items[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(in) != 1 || in[0].Item.Name != "main" || len(in[0].FromRanges) != 1 || in[0].FromRanges[0].Start.Line != 3 {
		t.Fatalf("incoming = %+v", in)
	}
	out, err := cc.OutgoingCalls(ctx, items[0])
	if err != nil {
		t.Logf("gopls outgoingCalls: %v", err)
	} else if len(out) != 0 {
		t.Logf("helper's outgoing calls: %+v", out)
	}
}
