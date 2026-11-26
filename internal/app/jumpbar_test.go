package app

import (
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"see/internal/lsp"
	"see/locales"
)

func TestSymbolPathAndChildren(t *testing.T) {
	s := outlineSymbols()
	if got := symbolPath(s, 1); !slices.Equal(got, []int{0, 1}) {
		t.Fatalf("path of Name = %v", got)
	}
	if got := symbolPath(s, 3); !slices.Equal(got, []int{3}) {
		t.Fatalf("path of Helper = %v", got)
	}
	if got := symbolChildren(s, -1); !slices.Equal(got, []int{0, 2, 3}) {
		t.Fatalf("top level = %v", got)
	}
	if got := symbolChildren(s, 0); !slices.Equal(got, []int{1}) {
		t.Fatalf("children of Server = %v", got)
	}
}

func jumpShortcut(t *testing.T, name string) command {
	t.Helper()
	for _, cmd := range commands() {
		if cmd.name == name {
			if cmd.shortcutKey == 0 {
				t.Fatalf("%s has no shortcut", name)
			}
			return cmd
		}
	}
	t.Fatalf("no command %s", name)
	return command{}
}

func TestJumpBarFollowsCursorAndJumps(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.go": outlineSource, "notes.txt": "plain", "sub/b.go": "package sub\n"}))
	h.a.cfg.Trust(h.a.root)
	client := &fileSymbolClient{fakeClient: h.fc, symbols: func(context.Context, string) ([]lsp.DocumentSymbol, error) { return outlineSymbols(), nil }}
	h.a.lsp = serverOf(h.a.root, client)
	defer h.a.lsp.StopWait(time.Second)
	h.open("a.go")
	st := h.a.activeEditor()
	moveTo := func(line, col int) {
		at := st.Buffer.Offset(editor.Pos{Line: line, Col: col})
		st.Select(at, at)
		h.tt.Frame()
	}
	moveTo(3, 3)
	h.settle(func() bool { return !h.a.outline.loading && len(h.a.outline.symbols) == 4 })
	h.tt.Frame()
	for _, text := range []string{"a.go", "Server", "Name"} {
		if !h.tt.HasText(text) {
			t.Fatalf("missing %q in %q", text, h.tt.Texts())
		}
	}
	if h.a.navigator == navOutline {
		t.Fatal("the jump bar needs the outline navigator")
	}
	shot := func(env string) {
		out := os.Getenv(env)
		if out == "" {
			return
		}
		for _, dark := range []bool{false, true} {
			h.tt.SetDark(dark)
			h.tt.Frame()
			name := out
			if dark {
				name = strings.TrimSuffix(out, ".png") + "-dark.png"
			}
			f, err := os.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if err = png.Encode(f, h.tt.Image()); err != nil {
				t.Fatal(err)
			}
			if err = f.Close(); err != nil {
				t.Fatal(err)
			}
		}
		h.tt.SetDark(false)
		h.tt.Frame()
	}
	shot("SEE_JUMPBAR_SCREENSHOT")

	moveTo(8, 6)
	if !h.tt.HasText("Helper") || h.tt.HasText("Name") {
		t.Fatalf("jump bar did not follow the cursor: %q", h.tt.Texts())
	}
	moveTo(0, 2)
	if !h.tt.HasText("No Selection") {
		t.Fatalf("no No Selection outside symbols: %q", h.tt.Texts())
	}

	// Clicking a symbol segment lists its siblings.
	moveTo(3, 3)
	if err := h.tt.Click("Server"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if got := h.tt.Menu(); !slices.Equal(got, []string{"Server", "(*Server).Start", "Helper"}) {
		t.Fatalf("Server's siblings = %q", got)
	}
	shot("SEE_JUMPBAR_MENU_SCREENSHOT")
	if err := h.tt.ChooseMenuItem("Helper"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if pos := st.CursorPos(); pos.Line != 8 {
		t.Fatalf("cursor at %v after choosing Helper", pos)
	}
	if !h.tt.HasText("Helper") || h.tt.HasText("Name") {
		t.Fatalf("jump bar did not follow the jump: %q", h.tt.Texts())
	}

	// Show Document Items opens the innermost segment's menu from the keyboard.
	moveTo(3, 3)
	docItems := jumpShortcut(t, "Show Document Items")
	h.tt.Key(docItems.shortcutMods, docItems.shortcutKey)
	h.tt.Frame()
	if got := h.tt.Menu(); !slices.Equal(got, []string{"Name"}) {
		t.Fatalf("document items = %q", got)
	}
	if err := h.tt.ChooseMenuItem("Name"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if pos := st.CursorPos(); pos.Line != 3 || pos.Col != 1 {
		t.Fatalf("cursor at %v after choosing Name", pos)
	}

	// Show Group Files lists the file's siblings, folders as submenus.
	files := jumpShortcut(t, "Show Group Files")
	h.tt.Key(files.shortcutMods, files.shortcutKey)
	h.settle(func() bool { return slices.Contains(h.tt.Menu(), "notes.txt") })
	if got := h.tt.Menu(); !slices.Equal(got, []string{"sub", "a.go", "notes.txt"}) {
		t.Fatalf("group files = %q", got)
	}
	if err := h.tt.ChooseMenuItem("notes.txt"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if got := h.a.activePath(); got != filepath.Join(h.a.root, "notes.txt") {
		t.Fatalf("active = %q after choosing notes.txt", got)
	}
	if h.tt.Menu() != nil {
		t.Fatal("menu still open")
	}
	if len(h.a.hist.Back) == 0 {
		t.Fatal("jumping to a file left no way back")
	}
}

func TestJumpBarKeepsDeepCurrentSymbolReachableWhenNarrow(t *testing.T) {
	const source = "package a\nfunc f() {}\n"
	current := strings.Repeat("VeryLongCurrentSymbol", 4)
	var symbols []lsp.DocumentSymbol
	for depth := 0; depth < 6; depth++ {
		symbols = append(symbols, lsp.DocumentSymbol{
			Name:     strings.Repeat("Ancestor", 3) + string(rune('A'+depth)),
			Kind:     12,
			Depth:    depth,
			Position: lsp.Position{Line: 1},
			Range:    lsp.Range{Start: lsp.Position{Line: 1}, End: lsp.Position{Line: 2}},
		})
	}
	symbols = append(symbols,
		lsp.DocumentSymbol{
			Name:     "AlternateParent",
			Kind:     12,
			Depth:    5,
			Position: lsp.Position{Line: 1},
			Range:    lsp.Range{Start: lsp.Position{Line: 1}, End: lsp.Position{Line: 2}},
		},
		lsp.DocumentSymbol{
			Name:     current,
			Kind:     12,
			Depth:    6,
			Position: lsp.Position{Line: 1},
			Range:    lsp.Range{Start: lsp.Position{Line: 1}, End: lsp.Position{Line: 2}},
		},
		lsp.DocumentSymbol{
			Name:     "Alternate",
			Kind:     12,
			Depth:    6,
			Position: lsp.Position{Line: 2},
			Range:    lsp.Range{Start: lsp.Position{Line: 2}, End: lsp.Position{Line: 3}},
		},
	)
	parent := symbols[6].Name
	h := newHarness(t, writeTree(t, map[string]string{"deep/branch/a.go": source}))
	h.a.cfg.Trust(h.a.root)
	client := &fileSymbolClient{fakeClient: h.fc, symbols: func(context.Context, string) ([]lsp.DocumentSymbol, error) { return symbols, nil }}
	h.a.lsp = serverOf(h.a.root, client)
	defer h.a.lsp.StopWait(time.Second)
	h.open("deep/branch/a.go")
	h.a.sidebarOff = true
	st := h.a.activeEditor()
	at := st.Buffer.Offset(editor.Pos{Line: 1, Col: 5})
	st.Select(at, at)
	h.tt.SetSize(420, harnessHeight)
	h.tt.Advance(300 * time.Millisecond)
	h.tt.Frame()
	h.settle(func() bool { return !h.a.outline.loading && len(h.a.outline.symbols) == len(symbols) })
	h.tt.Frame()

	r, ok := h.tt.Find(current)
	if !ok || r.W <= 0 || r.X < 0 || r.X+r.W > 420 {
		t.Fatalf("current symbol is not reachable within the narrow editor: bounds=%+v found=%v", r, ok)
	}

	if out := os.Getenv("SEE_JUMPBAR_OVERFLOW_SCREENSHOT"); out != "" {
		for _, dark := range []bool{false, true} {
			h.tt.SetDark(dark)
			h.tt.Frame()
			name := out
			if dark {
				name = strings.TrimSuffix(out, ".png") + "-dark.png"
			}
			f, err := os.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if err = png.Encode(f, h.tt.Image()); err != nil {
				t.Fatal(err)
			}
			if err = f.Close(); err != nil {
				t.Fatal(err)
			}
		}
		h.tt.SetDark(false)
		h.tt.Frame()
	}

	if err := h.tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	if got := h.tt.Menu(); !slices.Contains(got, parent) || !slices.Contains(got, "branch") {
		t.Fatalf("overflow items = %q", got)
	}
	if err := h.tt.ChooseMenuItem(parent); err != nil {
		t.Fatal(err)
	}
	if got := h.tt.Menu(); !slices.Equal(got, []string{symbols[5].Name, parent}) {
		t.Fatalf("ancestor siblings = %q", got)
	}
	if err := h.tt.ChooseMenuItem(parent); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if err := h.tt.Click(current); err != nil {
		t.Fatal(err)
	}
	if got := h.tt.Menu(); !slices.Equal(got, []string{current, "Alternate"}) {
		t.Fatalf("current symbol siblings = %q", got)
	}
	if err := h.tt.ChooseMenuItem(current); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if err := h.tt.Click("deep"); err != nil {
		if err := h.tt.Click("More"); err != nil {
			t.Fatal(err)
		}
		if err := h.tt.ChooseMenuItem("deep"); err != nil {
			t.Fatal(err)
		}
	}
	if got := h.tt.Menu(); !slices.Contains(got, "deep") {
		t.Fatalf("file ancestor menu = %q", got)
	}
	h.tt.Key(0, ui.KeyEscape)
	files := jumpShortcut(t, "Show Group Files")
	h.tt.Key(files.shortcutMods, files.shortcutKey)
	h.settleFrames(func() bool { return slices.Contains(h.tt.Menu(), "a.go") })
	if got := h.tt.Menu(); !slices.Contains(got, "a.go") {
		t.Fatalf("keyboard file menu = %q", got)
	}
	h.tt.Key(0, ui.KeyEscape)
	if h.tt.Menu() != nil {
		t.Fatal("Escape left the file menu open")
	}
}

func TestJumpBarShowsWholeModerateCurrentSymbolWhenRoomRemains(t *testing.T) {
	const current = "OtherRow"
	source := "package a\nfunc f() {}\n"
	symbols := make([]lsp.DocumentSymbol, 0, 7)
	for depth := 0; depth < 6; depth++ {
		symbols = append(symbols, lsp.DocumentSymbol{
			Name:     strings.Repeat("Ancestor", 2) + string(rune('A'+depth)),
			Kind:     12,
			Depth:    depth,
			Position: lsp.Position{Line: 1},
			Range:    lsp.Range{Start: lsp.Position{Line: 1}, End: lsp.Position{Line: 2}},
		})
	}
	symbols = append(symbols, lsp.DocumentSymbol{
		Name:     current,
		Kind:     12,
		Depth:    6,
		Position: lsp.Position{Line: 1},
		Range:    lsp.Range{Start: lsp.Position{Line: 1}, End: lsp.Position{Line: 2}},
	})
	h := newHarness(t, writeTree(t, map[string]string{"deep/branch/a.go": source}))
	h.a.cfg.Trust(h.a.root)
	client := &fileSymbolClient{fakeClient: h.fc, symbols: func(context.Context, string) ([]lsp.DocumentSymbol, error) { return symbols, nil }}
	h.a.lsp = serverOf(h.a.root, client)
	defer h.a.lsp.StopWait(time.Second)
	h.open("deep/branch/a.go")
	st := h.a.activeEditor()
	at := st.Buffer.Offset(editor.Pos{Line: 1, Col: 5})
	st.Select(at, at)
	h.settle(func() bool { return !h.a.outline.loading && len(h.a.outline.symbols) == len(symbols) })

	h.tt.SetSize(1800, harnessHeight)
	h.tt.Advance(300 * time.Millisecond)
	h.tt.Frame()
	fullBounds, ok := h.tt.Find(current)
	if !ok {
		t.Fatalf("missing current symbol %q", current)
	}
	fullImage := h.tt.Image()

	h.tt.SetSize(harnessWidth, harnessHeight)
	h.tt.Advance(300 * time.Millisecond)
	h.tt.Frame()
	actualBounds, ok := h.tt.Find(current)
	if !ok {
		t.Fatalf("missing current symbol %q at normal width", current)
	}
	actualImage := h.tt.Image()
	if !sameJumpBarLabelPixels(t, fullImage, fullBounds, actualImage, actualBounds) {
		t.Fatalf("current symbol %q is truncated despite free space: full=%+v normal=%+v", current, fullBounds, actualBounds)
	}
	if err := h.tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	if got := h.tt.Menu(); !slices.Contains(got, "AncestorAncestorF") {
		t.Fatalf("ancestor overflow menu = %q", got)
	}
}

func TestJumpBarShowsWholeModerateCurrentSymbolInSplitPane(t *testing.T) {
	h, current, parent := newSplitJumpBarHarness(t)

	h.tt.SetSize(1800, harnessHeight)
	h.tt.Advance(300 * time.Millisecond)
	h.tt.Frame()
	fullBounds, ok := h.tt.Find(current)
	if !ok {
		t.Fatalf("missing current symbol %q at wide split width", current)
	}
	fullImage := h.tt.Image()

	h.tt.SetSize(1280, harnessHeight)
	h.tt.Advance(300 * time.Millisecond)
	h.tt.Frame()
	actualBounds, ok := h.tt.Find(current)
	if !ok {
		t.Fatalf("missing current symbol %q in 640px split pane", current)
	}
	actualImage := h.tt.Image()
	bytesBounds, bytesOK := h.tt.Find(locales.MsgFileBytes())
	if !bytesOK {
		t.Fatal("missing file-bytes toggle in split pane")
	}
	t.Logf("current-symbol button parent at 640px pane: %+v; file-bytes toggle: %+v; intervening gap: %.1fpx", actualBounds, bytesBounds, bytesBounds.X-(actualBounds.X+actualBounds.W))
	if !sameJumpBarLabelPixels(t, fullImage, fullBounds, actualImage, actualBounds) {
		t.Fatalf("current symbol %q is truncated in a 640px split pane despite free space: parent=%+v wide-parent=%+v", current, actualBounds, fullBounds)
	}

	if out := os.Getenv("SEE_JUMPBAR_SPLIT_SCREENSHOT"); out != "" {
		f, err := os.Create(out)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, actualImage); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}

	if err := h.tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	if got := h.tt.Menu(); !slices.Contains(got, "five") || !slices.Contains(got, parent) {
		t.Fatalf("split-pane overflow items = %q", got)
	}
}

func TestJumpBarOverflowUsesDarkAppTheme(t *testing.T) {
	h, _, parent := newSplitJumpBarHarness(t)
	h.tt.SetDark(true)
	h.tt.Frame()
	if err := h.tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	if got := h.tt.Menu(); !slices.Contains(got, "five") || !slices.Contains(got, parent) {
		t.Fatalf("split-pane overflow items = %q", got)
	}
	itemBounds, ok := h.tt.Find("five")
	if !ok {
		t.Fatal("Toolbar overflow menu is not drawn in the app window")
	}
	img := h.tt.Image()
	px := img.RGBAAt(int(itemBounds.X+1), int(itemBounds.Y+1))
	if px.R > 128 || px.G > 128 || px.B > 128 {
		t.Fatalf("Toolbar overflow panel is not dark in explicit Dark appearance: pixel=%v at %+v", px, itemBounds)
	}
	if out := os.Getenv("SEE_JUMPBAR_SPLIT_DARK_SCREENSHOT"); out != "" {
		f, err := os.Create(out)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func newSplitJumpBarHarness(t *testing.T) (*harness, string, string) {
	t.Helper()
	const current = "WorkspaceMachineAccordionRow"
	const parent = "WorkspaceMachineAccordionRowControllerWithMetrics"
	names := []string{
		"WorkspaceRepository",
		"WorkspaceMachine",
		"WorkspaceMachineAccordion",
		"WorkspaceMachineAccordionRowModel",
		"WorkspaceMachineAccordionRowConfiguration",
		parent,
		current,
	}
	symbols := make([]lsp.DocumentSymbol, 0, len(names))
	for depth, name := range names {
		symbols = append(symbols, lsp.DocumentSymbol{
			Name:     name,
			Kind:     12,
			Depth:    depth,
			Position: lsp.Position{Line: 1},
			Range:    lsp.Range{Start: lsp.Position{Line: 1}, End: lsp.Position{Line: 2}},
		})
	}
	root := writeTree(t, map[string]string{
		"one/two/three/four/five/workspace.go": "package workspace\nfunc current() {}\n",
	})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	client := &fileSymbolClient{fakeClient: h.fc, symbols: func(context.Context, string) ([]lsp.DocumentSymbol, error) {
		return symbols, nil
	}}
	h.a.lsp = serverOf(root, client)
	t.Cleanup(func() { h.a.lsp.StopWait(time.Second) })
	h.a.splitActive(sideRight)
	h.open("one/two/three/four/five/workspace.go")
	st := h.a.activeEditor()
	at := st.Buffer.Offset(editor.Pos{Line: 1, Col: 5})
	st.Select(at, at)
	h.a.sidebarOff = true
	h.tt.SetSize(1280, harnessHeight)
	h.tt.Advance(300 * time.Millisecond)
	h.tt.Frame()
	h.settle(func() bool { return !h.a.outline.loading && len(h.a.outline.symbols) == len(symbols) })
	h.tt.Frame()
	return h, current, parent
}

func sameJumpBarLabelPixels(t *testing.T, want *image.RGBA, wantRect ui.Rect, got *image.RGBA, gotRect ui.Rect) bool {
	t.Helper()
	wantX, wantY := int(wantRect.X+0.5), int(wantRect.Y+0.5)
	gotX, gotY := int(gotRect.X+0.5), int(gotRect.Y+0.5)
	const labelWidth = 320
	w, h := labelWidth, int(wantRect.H+0.5)
	if int(wantRect.W) < w {
		w = int(wantRect.W)
	}
	if int(gotRect.W) < w {
		w = int(gotRect.W)
	}
	if w <= 0 || h <= 0 || int(gotRect.H+0.5) < h || wantX < 0 || wantY < 0 || gotX < 0 || gotY < 0 || wantX+w > want.Bounds().Max.X || wantY+h > want.Bounds().Max.Y || gotX+w > got.Bounds().Max.X || gotY+h > got.Bounds().Max.Y {
		return false
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if want.RGBAAt(wantX+x, wantY+y) != got.RGBAAt(gotX+x, gotY+y) {
				t.Logf("rendered label differs at offset (%d,%d) of %dpx content", x, y, w)
				return false
			}
		}
	}
	return true
}
