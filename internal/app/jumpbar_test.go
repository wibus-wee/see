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

	if err := h.tt.Click("Jump Bar overflow"); err != nil {
		t.Fatal(err)
	}
	if got := h.tt.Menu(); !slices.Contains(got, parent) || !slices.Contains(got, "branch") {
		t.Fatalf("overflow items = %q", got)
	}
	h.tt.Key(0, ui.KeyEscape)
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
	if err := h.tt.Click("Jump Bar overflow"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.ChooseMenuItem(parent, symbols[5].Name); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if err := h.tt.Click("Jump Bar overflow"); err != nil {
		t.Fatal(err)
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
	h.tt.Frame()
	if _, ok := h.tt.Find("Jump Bar overflow"); ok {
		t.Fatal("wide Jump Bar shows an overflow pill when every crumb fits")
	}
	fullBounds, ok := h.tt.Find(current)
	if !ok {
		t.Fatalf("missing current symbol %q", current)
	}
	fullImage := h.tt.Image()

	h.tt.SetSize(harnessWidth, harnessHeight)
	h.tt.Advance(300 * time.Millisecond)
	h.tt.Frame()
	h.tt.Frame()
	actualBounds, ok := h.tt.Find(current)
	if !ok {
		t.Fatalf("missing current symbol %q at normal width", current)
	}
	actualImage := h.tt.Image()
	if !sameJumpBarLabelPixels(t, fullImage, fullBounds, actualImage, actualBounds) {
		t.Fatalf("current symbol %q is truncated despite free space: full=%+v normal=%+v", current, fullBounds, actualBounds)
	}
	if err := h.tt.Click("Jump Bar overflow"); err != nil {
		t.Fatal(err)
	}
	if got := h.tt.Menu(); !slices.Contains(got, "AncestorAncestorF") {
		t.Fatalf("ancestor overflow menu = %q", got)
	}
}

func TestJumpBarShowsWholeModerateCurrentSymbolInSplitPane(t *testing.T) {
	h, current, parent, fileName := newSplitJumpBarHarness(t)

	h.tt.SetSize(1800, harnessHeight)
	h.tt.Advance(300 * time.Millisecond)
	h.tt.Frame()
	fullBounds, ok := h.tt.Find(current)
	if !ok {
		t.Fatalf("missing current symbol %q at wide split width", current)
	}
	fullFileBounds, ok := jumpBarButtonBounds(h.tt, fileName, fullBounds)
	if !ok {
		t.Fatalf("missing long filename %q at wide split width", fileName)
	}
	fullImage := h.tt.Image()

	h.tt.SetSize(1280, harnessHeight)
	h.tt.Advance(300 * time.Millisecond)
	h.tt.Frame()
	h.tt.Frame()
	actualBounds, ok := h.tt.Find(current)
	if !ok {
		t.Fatalf("missing current symbol %q in 640px split pane", current)
	}
	actualFileBounds, ok := jumpBarButtonBounds(h.tt, fileName, actualBounds)
	if !ok {
		t.Fatalf("missing long filename %q in 640px split pane", fileName)
	}
	pillBounds, pillOK := h.tt.Find("Jump Bar overflow")
	if !pillOK {
		t.Fatal("missing Jump Bar overflow pill")
	}
	middleBounds, middleOK := h.tt.Find("Jump Bar overflow area")
	if !middleOK {
		t.Fatal("missing Jump Bar overflow area")
	}
	if pillBounds.W > 32 || pillBounds.H > 24 || pillBounds.X < middleBounds.X || pillBounds.X+pillBounds.W > middleBounds.X+middleBounds.W || pillBounds.X+pillBounds.W > actualBounds.X {
		t.Fatalf("overflow pill is not a small control in the path/current-symbol gap: pill=%+v middle=%+v current=%+v", pillBounds, middleBounds, actualBounds)
	}
	gapCenter := middleBounds.X + middleBounds.W/2
	pillCenter := pillBounds.X + pillBounds.W/2
	centerDelta := pillCenter - gapCenter
	if centerDelta < 0 {
		centerDelta = -centerDelta
	}
	if centerDelta > 2 {
		t.Fatalf("overflow pill is not centered in the gap: pill=%+v middle=%+v current=%+v", pillBounds, middleBounds, actualBounds)
	}
	actualImage := h.tt.Image()
	bytesBounds, bytesOK := h.tt.Find(locales.MsgFileBytes())
	if !bytesOK {
		t.Fatal("missing file-bytes toggle in split pane")
	}
	t.Logf("current symbol at 640px pane: %+v; filename: %+v (wide: %+v); file-bytes toggle: %+v; intervening gap: %.1fpx", actualBounds, actualFileBounds, fullFileBounds, bytesBounds, bytesBounds.X-(actualBounds.X+actualBounds.W))
	if !sameJumpBarLabelPixels(t, fullImage, fullBounds, actualImage, actualBounds) {
		t.Fatalf("current symbol %q is truncated in a 640px split pane despite free space: parent=%+v wide-parent=%+v", current, actualBounds, fullBounds)
	}
	if actualFileBounds.W+1 >= fullFileBounds.W {
		t.Fatalf("long filename was not shortened before the current symbol: narrow=%+v wide=%+v", actualFileBounds, fullFileBounds)
	}
	for _, hidden := range []string{
		"WorkspaceRepository", "WorkspaceMachine", "WorkspaceMachineAccordion",
		"WorkspaceMachineAccordionRowModel", "WorkspaceMachineAccordionRowConfiguration", parent,
		"packages", "components", "src", "settings",
	} {
		if bounds, ok := h.tt.Find(hidden); ok {
			t.Fatalf("collapsed Jump Bar crumb %q remains visible at %+v", hidden, bounds)
		}
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
	if out := os.Getenv("SEE_JUMPBAR_SPLIT_DARK_BAR_SCREENSHOT"); out != "" {
		h.tt.SetDark(true)
		h.tt.Frame()
		f, err := os.Create(out)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, h.tt.Image()); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		h.tt.SetDark(false)
		h.tt.Frame()
	}

	if err := h.tt.Click("Jump Bar overflow"); err != nil {
		t.Fatal(err)
	}
	if got := h.tt.Menu(); !slices.Contains(got, fileName) || !slices.Contains(got, parent) {
		t.Fatalf("split-pane overflow items = %q", got)
	}
	if out := os.Getenv("SEE_JUMPBAR_SPLIT_LIGHT_MENU_SCREENSHOT"); out != "" {
		f, err := os.Create(out)
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
}

func TestJumpBarOmitsUnreadableFileCrumbAt510DIP(t *testing.T) {
	const current = "WorkspaceMachineAccordionRowWithAnIntentionallyLongName"
	h, _, parent, fileName := newSplitJumpBarHarnessWithSize(t, 1020, current)
	h.tt.SetDark(false)
	h.tt.Frame()
	currentTextBounds, ok := h.tt.Find(current)
	if !ok {
		t.Fatalf("missing long current symbol %q", current)
	}
	currentBounds, ok := jumpBarButtonBounds(h.tt, current, currentTextBounds)
	if !ok {
		t.Fatalf("missing current-symbol button for %q", current)
	}
	if fileBounds, visible := jumpBarButtonBounds(h.tt, fileName, currentTextBounds); visible {
		t.Fatalf("unreadable filename crumb remains in the 510-DIP Jump Bar: %+v", fileBounds)
	}
	pillBounds, ok := h.tt.Find("Jump Bar overflow")
	if !ok {
		t.Fatal("missing overflow pill when the file crumb is omitted")
	}
	middleBounds, ok := h.tt.Find("Jump Bar overflow area")
	if !ok {
		t.Fatal("missing overflow area when the file crumb is omitted")
	}
	if pillBounds.W > 32 || pillBounds.H > 24 || pillBounds.X < middleBounds.X || pillBounds.X+pillBounds.W > middleBounds.X+middleBounds.W {
		t.Fatalf("overflow pill is outside its allocated area: pill=%+v middle=%+v", pillBounds, middleBounds)
	}
	if pillBounds.X+pillBounds.W > currentBounds.X {
		t.Fatalf("overflow pill overlaps the current-symbol button: pill=%+v current=%+v", pillBounds, currentBounds)
	}
	gapCenter := middleBounds.X + middleBounds.W/2
	pillCenter := pillBounds.X + pillBounds.W/2
	delta := pillCenter - gapCenter
	if delta < 0 {
		delta = -delta
	}
	if delta > 2 {
		t.Fatalf("overflow pill is not centered in its allocated gap: pill=%+v middle=%+v", pillBounds, middleBounds)
	}
	t.Logf("510-DIP pane geometry: current=%+v pill=%+v middle=%+v; file crumb omitted", currentBounds, pillBounds, middleBounds)
	saveJumpBarScreenshot(t, "SEE_JUMPBAR_510_LIGHT_SCREENSHOT", h.tt.Image())
	if jumpBarPillHasChromaticPaint(h.tt.Image(), pillBounds) {
		t.Fatal("colored file icon paints over the overflow pill")
	}
	h.tt.SetDark(true)
	h.tt.Frame()
	saveJumpBarScreenshot(t, "SEE_JUMPBAR_510_DARK_SCREENSHOT", h.tt.Image())
	h.tt.SetDark(false)
	h.tt.Frame()

	siblingsDir := filepath.Join(h.a.root, "packages", "components", "src", "components", "settings")
	h.a.loadDir(siblingsDir)
	h.settle(func() bool {
		d := h.a.dirs[siblingsDir]
		return d != nil && d.loaded
	})
	showSiblingFiles := func() {
		files := jumpShortcut(t, "Show Group Files")
		h.tt.Key(files.shortcutMods, files.shortcutKey)
		h.settleFrames(func() bool { return len(h.tt.Menu()) > 0 })
		got := h.tt.Menu()
		if len(got) != 2 || !slices.Contains(got, fileName) || !slices.Contains(got, "neighbor.go") {
			t.Fatalf("Show Group Files should open direct siblings, got %q", got)
		}
	}
	showSiblingFiles()
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()
	if h.a.focus.jumpFilesDirect {
		t.Fatal("dismissed direct sibling menu left its temporary mode enabled")
	}
	if err := h.tt.Click("Jump Bar overflow"); err != nil {
		t.Fatal(err)
	}
	if got := h.tt.Menu(); !slices.Contains(got, parent) || !slices.Contains(got, fileName) || slices.Contains(got, "neighbor.go") {
		t.Fatalf("mouse capsule did not return to full overflow after dismiss: %q", got)
	}
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()

	showSiblingFiles()
	if err := h.tt.ChooseMenuItem("neighbor.go"); err != nil {
		t.Fatal(err)
	}
	neighbor := filepath.Join(h.a.root, "packages", "components", "src", "components", "settings", "neighbor.go")
	h.settle(func() bool {
		d := h.a.docs[neighbor]
		return h.a.activePath() == neighbor && d != nil && !d.loading
	})
	if h.a.focus.jumpFilesDirect {
		t.Fatal("selecting a sibling left the temporary direct-menu mode enabled")
	}
	st := h.a.activeEditor()
	at := st.Buffer.Offset(editor.Pos{Line: 1, Col: 5})
	st.Select(at, at)
	h.settleFrames(func() bool {
		return h.a.outline.shown.path == neighbor && !h.a.outline.loading && len(h.a.outline.symbols) > 0
	})
	if _, ok := h.tt.Find("Jump Bar overflow"); !ok {
		t.Fatal("missing overflow pill after selecting a sibling")
	}
	if err := h.tt.Click("Jump Bar overflow"); err != nil {
		t.Fatal(err)
	}
	if got := h.tt.Menu(); !slices.Contains(got, parent) || !slices.Contains(got, "packages") {
		t.Fatalf("mouse capsule did not return to full overflow after selection: %q", got)
	}
	if got, want := h.a.activePath(), neighbor; got != want {
		t.Fatalf("Show Group Files selected %q, want %q", got, want)
	}
}

func TestJumpBarOverflowOpensFromKeyboard(t *testing.T) {
	h, _, parent, fileName := newSplitJumpBarHarness(t)
	const label = "Jump Bar overflow"
	if err := h.tt.Click(label); err != nil {
		t.Fatal(err)
	}
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()
	if !h.tt.Focused(label) {
		t.Fatal("closing the overflow menu did not return focus to its pill")
	}
	h.tt.Key(0, ui.KeyEnter)
	h.tt.Frame()
	if got := h.tt.Menu(); !slices.Contains(got, fileName) || !slices.Contains(got, parent) {
		t.Fatalf("keyboard-opened overflow items = %q", got)
	}
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()
	if h.tt.Menu() != nil {
		t.Fatal("Escape left the Jump Bar overflow menu open")
	}
}

func TestJumpBarOverflowUsesDarkAppTheme(t *testing.T) {
	h, _, parent, fileName := newSplitJumpBarHarness(t)
	h.tt.SetDark(true)
	h.tt.Frame()
	if err := h.tt.Click("Jump Bar overflow"); err != nil {
		t.Fatal(err)
	}
	if got := h.tt.Menu(); !slices.Contains(got, fileName) || !slices.Contains(got, parent) {
		t.Fatalf("split-pane overflow items = %q", got)
	}
	itemBounds, ok := h.tt.Find("settings")
	if !ok {
		t.Fatal("Jump Bar overflow menu is not drawn in the app window")
	}
	img := h.tt.Image()
	px := img.RGBAAt(int(itemBounds.X+1), int(itemBounds.Y+1))
	if px.R > 128 || px.G > 128 || px.B > 128 {
		t.Fatalf("Jump Bar overflow panel is not dark in explicit Dark appearance: pixel=%v at %+v", px, itemBounds)
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

func newSplitJumpBarHarness(t *testing.T) (*harness, string, string, string) {
	return newSplitJumpBarHarnessWithSize(t, 1280, "WorkspaceMachineAccordionRow")
}

func newSplitJumpBarHarnessWithSize(t *testing.T, windowWidth int, current string) (*harness, string, string, string) {
	t.Helper()
	const parent = "WorkspaceMachineAccordionRowControllerWithMetrics"
	const fileName = "workspace_machine_accordion_configuration_and_runtime.go"
	const filePath = "packages/components/src/components/settings/" + fileName
	const neighborPath = "packages/components/src/components/settings/neighbor.go"
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
		filePath:     "package settings\nfunc current() {}\n",
		neighborPath: "package settings\nfunc neighbor() {}\n",
	})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	client := &fileSymbolClient{fakeClient: h.fc, symbols: func(context.Context, string) ([]lsp.DocumentSymbol, error) {
		return symbols, nil
	}}
	h.a.lsp = serverOf(root, client)
	t.Cleanup(func() { h.a.lsp.StopWait(time.Second) })
	h.a.splitActive(sideRight)
	h.open(filePath)
	st := h.a.activeEditor()
	at := st.Buffer.Offset(editor.Pos{Line: 1, Col: 5})
	st.Select(at, at)
	h.a.sidebarOff = true
	h.tt.SetSize(windowWidth, harnessHeight)
	h.tt.Advance(300 * time.Millisecond)
	h.tt.Frame()
	h.settle(func() bool { return !h.a.outline.loading && len(h.a.outline.symbols) == len(symbols) })
	h.tt.Frame()
	return h, current, parent, fileName
}

func jumpBarPillHasChromaticPaint(img *image.RGBA, bounds ui.Rect) bool {
	x, y := int(bounds.X+0.5), int(bounds.Y+0.5)
	if x < 0 || y < 0 || x+int(bounds.W) > img.Bounds().Max.X || y+int(bounds.H) > img.Bounds().Max.Y {
		return true
	}
	for py := 2; py < int(bounds.H)-2; py++ {
		for px := 2; px < int(bounds.W)-2; px++ {
			pixel := img.RGBAAt(x+px, y+py)
			r, g, b := pixel.R, pixel.G, pixel.B
			max, min := max(r, g, b), min(r, g, b)
			if max-min > 30 {
				return true
			}
		}
	}
	return false
}

func saveJumpBarScreenshot(t *testing.T, env string, img *image.RGBA) {
	t.Helper()
	out := os.Getenv(env)
	if out == "" {
		return
	}
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

func jumpBarButtonBounds(tt *ui.Tester, name string, row ui.Rect) (ui.Rect, bool) {
	for _, item := range tt.AccessAll() {
		bounds := item.Bounds
		if item.Name == name && bounds.Y <= row.Y+row.H && bounds.Y+bounds.H >= row.Y {
			return bounds, true
		}
	}
	return ui.Rect{}, false
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
