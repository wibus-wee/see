package app

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
	"see/internal/search"
	"see/internal/theme"

	"see/internal/langserver"
)

func TestOpeningTreeFileKeepsScrollPosition(t *testing.T) {
	files := make(map[string]string)
	for i := range 60 {
		files[fmt.Sprintf("file%02d.go", i)] = "package example\n"
	}
	h := newHarness(t, writeTree(t, files))
	before, ok := h.tt.Find("file20.go")
	if !ok {
		t.Fatal("expected file20.go to be visible")
	}
	h.click("file20.go")
	h.tt.Frame()
	h.tt.Frame()
	after, ok := h.tt.Find("file20.go")
	if !ok || after.Y != before.Y {
		t.Fatalf("opening a visible file moved its row: before %v, after %v, visible %v", before, after, ok)
	}
	if h.a.activePath() != filepath.Join(h.a.root, "file20.go") {
		t.Fatalf("file did not open: %q", h.a.activePath())
	}
	if h.a.treeRow < 0 || h.a.treeRow >= h.a.tree.Rows() || h.a.tree.Item(h.a.treeRow) != h.a.activePath() {
		t.Fatalf("opened file is not selected in the tree: row %d", h.a.treeRow)
	}
}

func TestNavigatorShortcutPreservesWidth(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.go": "package a\n"}))
	h.tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	h.open("a.go")
	h.a.sidebarW = 310
	h.tt.Frame()
	h.tt.Key(ui.Cmd, ui.KeyB)
	h.tt.Frame()
	if !h.a.sidebarOff || h.tt.HasText("Filter Files") {
		t.Fatal("Cmd+B did not hide the navigator")
	}
	h.tt.Key(ui.Cmd, ui.KeyB)
	h.tt.Frame()
	if h.a.sidebarOff || h.a.sidebarW != 310 || !h.tt.HasText("a.go") {
		t.Fatalf("navigator did not return at its saved width: hidden %v, width %v", h.a.sidebarOff, h.a.sidebarW)
	}
}

func TestQuickButtonCenteredInEditorHeader(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.go": "package a\n"}))
	h.a.sidebarW = 260
	h.a.notice = "Go to Definition works in Go files, with gopls."
	h.tt.Frame()
	// The middle of the editor's part of the toolbar: past the sidebar
	// and its divider, to the window's edge.
	center := (h.a.sidebarW + 1 + harnessWidth) / 2
	r, ok := h.tt.Find("Open Quickly")
	if !ok || r.X+r.W/2 < center-1 || r.X+r.W/2 > center+1 {
		t.Fatalf("quick button is not centered in the editor header: %+v, found %v", r, ok)
	}
	h.click("Open Quickly")
	if !h.a.quick.open {
		t.Fatal("centered quick button did not open search")
	}
}

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

// ⌘D splits right and ⇧⌘D down, as iTerm, even with the editor focused,
// which would take ⌘D for Select Next Occurrence.
func TestCmdDSplits(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\n\nfunc A() {}\n"})
	h := newHarness(t, root)
	h.open("a.go")
	h.tt.Frame()
	if n := len(h.a.groups()); n != 1 {
		t.Fatalf("%d groups before", n)
	}
	h.tt.Key(ui.Cmd, ui.KeyD)
	h.tt.Frame()
	if n := len(h.a.groups()); n != 2 {
		t.Fatalf("%d groups after ⌘D, want 2", n)
	}
	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyD)
	h.tt.Frame()
	if n := len(h.a.groups()); n != 3 {
		t.Fatalf("%d groups after ⇧⌘D, want 3", n)
	}
}

func TestTabShortcuts(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\n", "b.go": "package b\n", "c.go": "package c\n"})
	h := newHarness(t, root)
	a, b, c := h.open("a.go"), h.open("b.go"), h.open("c.go")
	check := func(key ui.Key, mods ui.Modifiers, want string) {
		t.Helper()
		h.tt.Key(mods, key)
		h.tt.Frame()
		if got := h.a.activePath(); got != want {
			t.Fatalf("key %v mods %v: active %q, want %q", key, mods, got, want)
		}
	}
	check(ui.Key1, ui.Cmd, a.path)
	check(ui.Key2, ui.Cmd, b.path)
	check(ui.Key9, ui.Cmd, c.path)
	check(ui.Key8, ui.Cmd, c.path) // a missing position does nothing
	check(ui.KeyTab, ui.Ctrl, a.path)
	check(ui.KeyTab, ui.Ctrl|ui.Shift, c.path)
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

func TestFindShortcutFocusesQuery(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.go": "hello"}))
	h.a.sidebarOff = true
	h.tt.Frame()
	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyF)
	h.tt.Type("hello")
	if h.a.find.opts.Query != "hello" {
		t.Fatalf("query %q after shortcut and typing", h.a.find.opts.Query)
	}
	h.tt.Click("Show the Project Navigator")
	h.tt.Frame()
	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyF)
	h.tt.Type(" again")
	if h.a.find.opts.Query != "hello again" {
		t.Fatalf("query %q after reopening find", h.a.find.opts.Query)
	}
}

func TestFindNavigator(t *testing.T) {
	rg, err := search.FindRipgrep("")
	if err != nil {
		t.Skip("ripgrep is not installed; set SEE_RG")
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

func TestFindClickSelectsOpenedMatch(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "first hit\nsecond hit\n"})
	h := newHarness(t, root)
	h.a.navigator = navFind
	path := filepath.Join(root, "a.go")
	h.a.find.results = []search.File{{Path: path, Matches: []search.Match{
		{Line: 0, Col: 6, End: 9, Preview: "first hit", PreStart: 6, PreEnd: 9},
		{Line: 1, Col: 7, End: 10, Preview: "second hit", PreStart: 7, PreEnd: 10},
	}}}
	h.a.find.tree.Open.Add(findKey{path: path, match: -1})
	h.tt.Frame()
	for i, label := range []string{"first", "second"} {
		r, ok := h.tt.Find(label)
		if !ok {
			t.Fatalf("missing match preview %q", label)
		}
		h.tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
		h.tt.Frame()
		row := h.a.find.row
		if row < 0 || row >= h.a.find.tree.Rows() || h.a.find.tree.Item(row) != (findKey{path: path, match: i}) {
			t.Fatalf("after clicking %q, selected row %d", label, row)
		}
		if h.a.activePath() != path {
			t.Fatalf("after clicking %q, active path %q", label, h.a.activePath())
		}
	}
}

func TestFindWithoutRipgrep(t *testing.T) {
	t.Setenv("SEE_RG", "")
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
	md := definitionPreview(target{path: bPath, pos: lsp.Position{Line: 3}})
	if !strings.Contains(md, "// 名字 is 🎉.") || !strings.Contains(md, "func 名字() {}") {
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
	if len(h.fc.calls()) != 0 || h.a.lspState != langserver.Off {
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

// TestScreenshot draws the window for a look, when SEE_SHOT names a
// directory to write it to.
func TestScreenshot(t *testing.T) {
	dir := os.Getenv("SEE_SHOT")
	if dir == "" {
		t.Skip("set SEE_SHOT to a directory")
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
	if h.a.colors() != &theme.Dark || h.a.theme.Background != theme.Dark.Editor {
		t.Fatalf("dark: editor %v", h.a.theme.Background)
	}
	darkTheme := h.ed(d).Theme
	h.tt.SetDark(false)
	h.tt.Frame()
	if h.a.colors() != &theme.Light || h.a.theme.Background != theme.Light.Editor {
		t.Errorf("light: editor %v", h.a.theme.Background)
	}
	if st := h.ed(d); st.Theme == darkTheme || st.Theme.Background != theme.Light.Editor {
		t.Error("the open editor kept the dark theme")
	}
	// A new editor takes the light one too.
	os.WriteFile(filepath.Join(h.a.root, "b.go"), []byte("package b\n"), 0o644)
	b := h.open("b.go")
	if h.ed(b).Theme.Background != theme.Light.Editor {
		t.Error("a new editor is not light")
	}
	// Choosing an appearance is remembered, and asks the windows for it.
	var asked string
	h.host.appearance = func(s string) { asked = s }
	h.a.chooseAppearance("dark")
	if h.a.cfg.Appearance != "dark" || asked != "dark" {
		t.Errorf("appearance %q, asked %q", h.a.cfg.Appearance, asked)
	}
}

func TestCommandPaletteAndPreview(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\n", "b.go": "package b\n", "c.go": "package c\n"})
	h := newHarness(t, root)
	a, b, c := filepath.Join(root, "a.go"), filepath.Join(root, "b.go"), filepath.Join(root, "c.go")
	h.a.previewOpen(a)
	h.a.previewOpen(b)
	if names(h.a.focus.order) != "b.go" || h.a.focus.preview != b {
		t.Fatalf("preview order %v, preview %q", h.a.focus.order, h.a.focus.preview)
	}
	h.tt.Frame()
	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyP)
	if !h.a.quick.open || h.a.quick.query != ">" {
		t.Fatal("palette did not open")
	}
	h.tt.Type("keep preview")
	h.tt.Frame()
	h.tt.Key(0, ui.KeyEnter)
	if h.a.quick.open || h.a.focus.preview != "" {
		t.Fatal("command did not pin the preview")
	}
	h.a.previewOpen(c)
	if names(h.a.focus.order) != "b.go c.go" {
		t.Fatalf("pinned tab lost: %v", h.a.focus.order)
	}
	h.a.previewOpen(a)
	if names(h.a.focus.order) != "b.go a.go" {
		t.Fatalf("preview not replaced: %v", h.a.focus.order)
	}
}

func TestTreePreviewDoubleClick(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\n", "b.go": "package b\n"})
	h := newHarness(t, root)
	h.click("a.go")
	if h.a.focus.preview != filepath.Join(root, "a.go") {
		t.Fatalf("single click preview %q", h.a.focus.preview)
	}
	h.click("a.go")
	if h.a.focus.preview != "" {
		t.Fatalf("double click did not keep tab: %q", h.a.focus.preview)
	}
	h.click("b.go")
	if names(h.a.focus.order) != "a.go b.go" {
		t.Fatalf("tabs %v", h.a.focus.order)
	}
}

func TestCollapseAllFolders(t *testing.T) {
	root := writeTree(t, map[string]string{"a/b/file.go": "package p\n", "other/file.go": "package p\n"})
	h := newHarness(t, root)
	file := h.open("a/b/file.go")
	a, b, other := filepath.Join(root, "a"), filepath.Join(root, "a", "b"), filepath.Join(root, "other")
	h.a.tree.Open.Add(other)
	h.a.reveal = file.path
	h.tt.Frame()
	h.click("Collapse All Folders")
	h.tt.Frame()
	if h.a.tree.Open.Len() != 0 || h.a.reveal != "" {
		t.Fatalf("open folders: %d, pending reveal: %q", h.a.tree.Open.Len(), h.a.reveal)
	}
	if h.a.activePath() != file.path {
		t.Fatal("collapsing folders changed the active file")
	}
	h.a.tree.Open.Add(a)
	h.tt.Frame()
	if h.a.tree.Open.Has(b) || h.a.tree.Open.Has(other) {
		t.Fatal("nested folders reopened")
	}
}
