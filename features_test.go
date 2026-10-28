package main

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
	"see/internal/search"
)

func TestSplitAndMoveTabs(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": strings.Repeat("// a\n", 200), "b.go": "package b\n", "c.go": "package c\n"})
	h := newHarness(t, root)
	a := h.open("a.go")
	h.ed(a).GoTo(editor.Pos{Line: 120})
	h.open("b.go")
	h.a.userActivate(a.path)

	// Split right: the same document, at the same place, in a new group
	// sharing the buffer.
	g1 := h.a.focus
	h.tt.Key(ui.Cmd, ui.KeyBackslash)
	gs := h.a.groups()
	if len(gs) != 2 || h.a.focus != gs[1] || gs[1].active != a.path {
		t.Fatalf("groups %d, focus %v", len(gs), h.a.focus.id)
	}
	h.tt.Frame()
	if g2ed := gs[1].eds[a.path]; g2ed == nil || g2ed == g1.eds[a.path] || g2ed.Buffer != g1.eds[a.path].Buffer || g2ed.CursorPos().Line != 120 {
		t.Errorf("the split's editor: %+v", g2ed)
	}
	// Moving b.go into the new group's left edge makes a third group.
	h.a.moveTab(g1, filepath.Join(h.a.root, "b.go"), gs[1], sideBottom, 0)
	if gs = h.a.groups(); len(gs) != 3 || !gs[2].has(filepath.Join(h.a.root, "b.go")) || g1.has(filepath.Join(h.a.root, "b.go")) {
		t.Fatalf("after move: %d groups", len(gs))
	}
	// Closing the last tab of a group takes the group away; a document
	// another group shows stays open.
	h.a.closeTab(gs[2], filepath.Join(h.a.root, "b.go"))
	if len(h.a.groups()) != 2 || h.a.docs[filepath.Join(h.a.root, "b.go")] != nil {
		t.Errorf("groups %d, b.go doc %v", len(h.a.groups()), h.a.docs[filepath.Join(h.a.root, "b.go")])
	}
	h.a.closeTab(g1, a.path)
	if len(h.a.groups()) != 1 || h.a.docs[a.path] == nil {
		t.Errorf("a.go let go while shown: groups %d", len(h.a.groups()))
	}
	h.tt.Frame()
}

func TestDropZones(t *testing.T) {
	for _, tc := range []struct {
		x, y float32
		want side
	}{
		{50, 50, sideNone}, {5, 50, sideLeft}, {95, 50, sideRight}, {50, 3, sideTop}, {50, 97, sideBottom},
	} {
		if got := zoneAt(tc.x, tc.y, 100, 100); got != tc.want {
			t.Errorf("zoneAt(%v, %v) = %v, want %v", tc.x, tc.y, got, tc.want)
		}
	}
	root := writeTree(t, map[string]string{"a.go": "package a\n", "b.go": "package b\n"})
	h := newHarness(t, root)
	h.open("a.go")
	// A file dropped from the tree on an editor's right edge opens beside it.
	h.a.dropTab(&tabDrag{path: filepath.Join(h.a.root, "b.go")}, h.a.focus, sideRight, 0)
	h.settle(func() bool { return h.ed(h.a.docs[filepath.Join(h.a.root, "b.go")]) != nil })
	if gs := h.a.groups(); len(gs) != 2 || gs[1].active != filepath.Join(h.a.root, "b.go") {
		t.Errorf("groups %d", len(gs))
	}
	// Reordering within a tab bar.
	g := h.a.groups()[0]
	h.a.openIn(g, filepath.Join(h.a.root, "b.go"), goTo{})
	h.a.dropTab(&tabDrag{from: g, path: filepath.Join(h.a.root, "b.go")}, g, sideNone, 0)
	if names(g.order) != "b.go a.go" {
		t.Errorf("order %s", names(g.order))
	}
}

func TestFindNavigator(t *testing.T) {
	rg, err := search.FindRipgrep("")
	if err != nil {
		t.Skip("ripgrep is not installed; set NSR_RG")
	}
	_ = rg
	root := writeTree(t, map[string]string{
		"a.go":     "package a\n\nfunc Hello() {}\n// hello 你好 hello\n",
		"sub/b.go": "package sub // hello\n",
	})
	h := newHarness(t, root)
	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyF)
	if h.a.navigator != navFind || !h.tt.HasText("Aa") {
		t.Fatalf("navigator %d, texts %q", h.a.navigator, h.tt.Texts())
	}
	h.a.find.opts.Query = "hello"
	h.a.runFind()
	h.settle(func() bool { return !h.a.find.running })
	if h.a.find.stats.Matches != 4 || h.a.find.stats.Files != 2 || !h.tt.HasText("4 results in 2 files") {
		t.Fatalf("stats %+v, texts %q", h.a.find.stats, h.tt.Texts())
	}
	// Clicking a match opens it with the match selected.
	file := h.a.findFile(filepath.Join(h.a.root, "a.go"))
	h.a.openMatch(findKey{path: file.Path, match: 2})
	d := h.a.docs[file.Path]
	h.settle(func() bool { return h.ed(d) != nil })
	if sel := h.ed(d).SelectedText(); sel != "hello" || h.ed(d).CursorPos().Line != 3 {
		t.Errorf("selected %q at %v", sel, h.ed(d).CursorPos())
	}
	// A new query drops the old results.
	h.a.find.opts.Query = "nothing-here"
	h.a.runFind()
	h.settle(func() bool { return !h.a.find.running })
	if len(h.a.find.results) != 0 || !h.tt.HasText("No results") {
		t.Errorf("results %v", h.a.find.results)
	}
}

func TestFindWithoutRipgrep(t *testing.T) {
	t.Setenv("NSR_RG", "")
	t.Setenv("PATH", t.TempDir())
	root := writeTree(t, map[string]string{"a.go": "x"})
	h := newHarness(t, root)
	h.a.navigator = navFind
	h.a.find.opts.Query = "x"
	h.a.runFind()
	h.tt.Frame()
	if !h.tt.HasText("Finding needs ripgrep: " + search.InstallHint() + ".") {
		t.Errorf("texts %q", h.tt.Texts())
	}
}

func TestOpenQuickly(t *testing.T) {
	root := writeTree(t, map[string]string{"main.go": "", "internal/app/model.go": "", "docs/guide.md": ""})
	h := newHarness(t, root)
	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyO)
	h.settle(func() bool { return h.a.quick.files != nil })
	if !h.a.quick.open || !h.tt.HasText("Open Quickly") {
		t.Fatalf("texts %q", h.tt.Texts())
	}
	h.tt.Type("go")
	h.tt.Frame()
	h.tt.Key(0, ui.KeyDown)
	if h.a.quick.sel != 1 {
		t.Errorf("Down: sel %d of %v", h.a.quick.sel, h.a.quick.results)
	}
	h.tt.Key(0, ui.KeyBackspace)
	h.tt.Key(0, ui.KeyBackspace)
	h.tt.Type("mdl")
	h.tt.Frame()
	if len(h.a.quick.results) != 1 || h.a.quick.results[0].Path != "internal/app/model.go" {
		t.Fatalf("results %v", h.a.quick.results)
	}
	h.tt.Key(0, ui.KeyEnter)
	model := filepath.Join(h.a.root, "internal", "app", "model.go")
	if h.a.quick.open || h.a.activePath() != model {
		t.Errorf("open %v, active %q", h.a.quick.open, h.a.activePath())
	}
	h.settle(func() bool { return !h.a.docs[model].loading })
	// Escape closes it without opening anything.
	h.a.openQuickly()
	h.tt.Frame()
	h.tt.Key(0, ui.KeyEscape)
	if h.a.quick.open {
		t.Error("Escape left it open")
	}
}

func TestHoverAndCmdLink(t *testing.T) {
	root, h := goWorkspace(t)
	bPath := filepath.Join(root, "b", "b.go")
	h.fc.hover = func(context.Context, string, lsp.Position) (string, *lsp.Range, error) {
		return "```go\nfunc b.名字()\n```\n\n名字 is 🎉.", nil, nil
	}
	h.fc.def = func(context.Context, string, lsp.Position) ([]lsp.Location, error) {
		return []lsp.Location{{URI: uriOf(bPath), Range: lsp.Range{Start: lsp.Position{Line: 3, Character: 5}}}}, nil
	}
	a := h.open("a.go")
	st := h.ed(a)
	off := at(a, "名字()") + 3
	st.OnHover(editor.HoverRequest{Offset: off})
	h.settle(func() bool { return strings.Contains(strings.Join(h.fc.calls(), ","), "hover") && len(h.q) == 0 })
	h.drainNow()
	if st.TextBackgrounds != nil {
		t.Error("a plain hover underlaid a link")
	}
	// With Cmd held, the hover is the definition, and the word a link.
	h.tt.HoldModifiers(ui.Cmd)
	h.tt.Frame()
	if !h.a.hover.cmd {
		t.Fatal("Cmd not seen")
	}
	st.OnHover(editor.HoverRequest{Offset: off})
	h.settle(func() bool { return h.a.hover.link == st })
	start := at(a, "名字()")
	if len(st.TextBackgrounds) != 1 || st.TextBackgrounds[0].Start != start || st.TextBackgrounds[0].End != start+len("名字") {
		t.Errorf("link %+v", st.TextBackgrounds)
	}
	md := definitionPreview(root, target{path: bPath, pos: lsp.Position{Line: 3}})
	if !strings.Contains(md, "b/b.go:4") || !strings.Contains(md, "// 名字 is 🎉.") || !strings.Contains(md, "func 名字() {}") {
		t.Errorf("preview %q", md)
	}
	// Letting Cmd go takes the link away.
	h.tt.HoldModifiers(0)
	h.tt.Frame()
	if st.TextBackgrounds != nil || h.a.hover.link != nil {
		t.Error("link stayed after Cmd was let go")
	}
}

func TestHoverAsksNothingUntrusted(t *testing.T) {
	root := writeTree(t, map[string]string{"m.go": "package m\n"})
	h := newHarness(t, root)
	m := h.open("m.go")
	h.ed(m).OnHover(editor.HoverRequest{Offset: 2})
	h.tt.Frame()
	if len(h.fc.calls()) != 0 || h.a.lspState != lspOff {
		t.Errorf("calls %v, state %v", h.fc.calls(), h.a.lspState)
	}
}

func TestWordAt(t *testing.T) {
	line := "\tx := b.名字(y_1)"
	for col, want := range map[int]string{1: "x", 7: "b", 8: "名字", 11: "名字", 15: "y_1", 3: ""} {
		s, e := wordAt(line, col)
		if line[s:e] != want {
			t.Errorf("wordAt(%d) = %q, want %q", col, line[s:e], want)
		}
	}
}

func TestFileKinds(t *testing.T) {
	for name, want := range map[string]fileKind{
		"main.go": kGo, "go.mod": kMod, "App.TSX": kTS, "README.md": kMD, "x.unknown": kText, "Makefile": kSh,
	} {
		if got := kindOf(name); got != want {
			t.Errorf("kindOf(%q) = %+v", name, got)
		}
	}
}

// TestScreenshot draws the window for a look, when NSR_SHOT names a
// directory to write it to.
func TestScreenshot(t *testing.T) {
	dir := os.Getenv("NSR_SHOT")
	if dir == "" {
		t.Skip("set NSR_SHOT to a directory")
	}
	root, h := goWorkspace(t)
	h.tt.SetSize(1280, 800)
	h.click("b")
	h.settle(func() bool { d := h.a.dirs[filepath.Join(root, "b")]; return d != nil && d.loaded })
	h.open("a.go")
	h.a.openFile(filepath.Join(root, "b", "b.go"), nil)
	h.settle(func() bool { return !h.a.docs[filepath.Join(root, "b", "b.go")].loading })
	shot := func(name string) {
		h.tt.Frame()
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		png.Encode(f, h.tt.Image())
	}
	shot("main.png")
	h.tt.SetDark(false)
	shot("main-light.png")
	h.tt.SetDark(true)
	h.a.splitActive(sideRight)
	shot("split.png")
	h.a.openQuickly()
	h.settle(func() bool { return h.a.quick.files != nil })
	h.tt.Type("b")
	shot("quick.png")
	h.tt.Key(0, ui.KeyEscape)
	if rg, err := search.FindRipgrep(""); err == nil && rg != "" {
		h.a.navigator = navFind
		h.a.find.opts.Query = "名字"
		h.a.runFind()
		h.settle(func() bool { return !h.a.find.running })
		shot("find.png")
		h.tt.SetDark(false)
		shot("find-light.png")
		h.tt.SetDark(true)
	}
	h.a.showWelcome = true
	shot("welcome.png")
}

func TestAppearanceFollowsSystem(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\n"})
	h := newHarness(t, root)
	d := h.open("a.go")
	h.tt.SetDark(true)
	h.tt.Frame()
	if colEditor != darkPalette.editor || !h.a.theme.Dark {
		t.Fatalf("dark: editor %v", colEditor)
	}
	darkTheme := h.ed(d).Theme
	h.tt.SetDark(false)
	h.tt.Frame()
	if colEditor != lightPalette.editor || h.a.theme.Dark {
		t.Errorf("light: editor %v", colEditor)
	}
	if st := h.ed(d); st.Theme == darkTheme || st.Theme.Background != lightPalette.editor {
		t.Error("the open editor kept the dark theme")
	}
	// A new editor takes the light one too.
	os.WriteFile(filepath.Join(h.a.root, "b.go"), []byte("package b\n"), 0o644)
	b := h.open("b.go")
	if h.ed(b).Theme.Background != lightPalette.editor {
		t.Error("a new editor is not light")
	}
	// Choosing an appearance is remembered, and asks the windows for it.
	var asked string
	h.a.setAppearance = func(s string) { asked = s }
	h.a.chooseAppearance("dark")
	if h.a.cfg.Appearance != "dark" || asked != "dark" {
		t.Errorf("appearance %q, asked %q", h.a.cfg.Appearance, asked)
	}
}
