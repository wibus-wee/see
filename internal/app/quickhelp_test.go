package app

import (
	"context"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"see/internal/lsp"
	"see/internal/platform"
	"see/internal/uri"
)

func TestSplitQuickHelp(t *testing.T) {
	got := splitQuickHelp("```go\nfunc helper() int\n```\n\nReturns the result.\n\nUse it when needed.")
	if got.declaration != "```go\nfunc helper() int\n```" || got.summary != "Returns the result." || got.discussion != "Use it when needed." {
		t.Fatalf("sections = %+v", got)
	}
	got = splitQuickHelp("~~~rust\nfn helper()\n~~~\n\nOnly a summary.")
	if got.declaration != "~~~rust\nfn helper()\n~~~" || got.summary != "Only a summary." || got.discussion != "" {
		t.Fatalf("single paragraph = %+v", got)
	}
	if got = splitQuickHelp(" \n\t"); got != (quickHelpSections{}) {
		t.Fatalf("empty markdown = %+v", got)
	}
	got = splitQuickHelp("Plain text without a declaration.")
	if got.declaration != "" || got.summary != "Plain text without a declaration." || got.discussion != "" {
		t.Fatalf("plain markdown = %+v", got)
	}
}

func TestSplitQuickHelpDropsLeadingMarkdownSeparators(t *testing.T) {
	for _, separator := range []string{"---", "***"} {
		t.Run(separator, func(t *testing.T) {
			md := "\n\n```go\nfunc Greeting(name string) string\n```\n\n" + separator +
				"\n\nGreeting returns a friendly welcome.\n\nThe caller supplies a name."
			got := splitQuickHelp(md)
			if got.declaration != "```go\nfunc Greeting(name string) string\n```" ||
				got.summary != "Greeting returns a friendly welcome." || got.discussion != "The caller supplies a name." {
				t.Fatalf("sections with leading %q = %+v", separator, got)
			}
		})
	}
}

func TestSplitQuickHelpPreservesMarkdownAfterSummary(t *testing.T) {
	got := splitQuickHelp("```go\nfunc Greeting() string\n```\n\nGreeting returns a welcome.\n\n---\n\nKeep this discussion.")
	if got.summary != "Greeting returns a welcome." || got.discussion != "---\n\nKeep this discussion." {
		t.Fatalf("sections = %+v", got)
	}
}

func TestQuickHelpShortcutDoesNotConflict(t *testing.T) {
	var quick command
	for _, cmd := range commands() {
		if cmd.name == "Show Quick Help" {
			quick = cmd
		}
		if cmd.shortcutKey == 0 {
			continue
		}
		for _, other := range commands() {
			if cmd.name != other.name && cmd.shortcutMods == other.shortcutMods && cmd.shortcutKey == other.shortcutKey {
				t.Fatalf("%s and %s share a shortcut", cmd.name, other.name)
			}
		}
	}
	if quick.shortcutMods != ui.Alt|ui.Cmd || quick.shortcutKey != ui.KeyK {
		t.Fatalf("Quick Help shortcut = %v %v", quick.shortcutMods, quick.shortcutKey)
	}
	if commandLabel(quick.name) != "Show Quick Help" {
		t.Fatalf("command palette label = %q", commandLabel(quick.name))
	}
}

func TestQuickHelpAltClick(t *testing.T) {
	h, d, offset := quickHelpHarness(t)
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(offset + 2))
	h.tt.Frame()
	caret, caretOK := h.tt.TextCaret()
	if !caretOK {
		t.Fatal("editor caret is not available")
	}
	h.tt.ClickAtWith(ui.Alt, caret.X, caret.Y+caret.H/2)
	waitForQuickHelp(t, h, st, "Declaration")
	if !h.tt.HasText("Returns the result.") || !h.tt.HasText("Use it when needed.") || !h.tt.HasText("b.go:2") {
		t.Fatalf("quick help sections missing: %q", h.tt.Texts())
	}
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()
	if h.tt.HasText("Declaration") {
		t.Fatal("Escape did not dismiss Quick Help")
	}
}

func TestQuickHelpAltClickUsesClickedSymbol(t *testing.T) {
	h, d, helperOffset := quickHelpHarness(t)
	st := h.a.activeEditor()
	callOffset := strings.Index(d.buf.String(), "call")
	line := d.buf.Line(1)
	helperColumn := strings.Index(line, "helper")
	callColumn := strings.Index(line, "call")
	positions := make(chan lsp.Position, 1)
	h.fc.hover = func(_ context.Context, _ string, pos lsp.Position) (string, *lsp.Range, error) {
		positions <- pos
		if pos.Character >= helperColumn && pos.Character < helperColumn+len("helper") {
			return "```go\nfunc helper() int\n```\n\nClicked helper.", nil, nil
		}
		if pos.Character >= callColumn && pos.Character < callColumn+len("call") {
			return "```go\nfunc call()\n```\n\nOriginal caret symbol.", nil, nil
		}
		return "", nil, nil
	}
	st.GoTo(d.buf.Pos(helperOffset + 2))
	h.tt.Frame()
	helperCaret, ok := h.tt.TextCaret()
	if !ok {
		t.Fatal("helper caret is not available")
	}
	st.GoTo(d.buf.Pos(callOffset + 2))
	h.tt.Frame()
	h.tt.ClickAtWith(ui.Alt, helperCaret.X, helperCaret.Y+helperCaret.H/2)
	waitForQuickHelp(t, h, st, "Declaration")
	if !h.tt.HasText("Clicked helper.") || h.tt.HasText("Original caret symbol.") {
		t.Fatalf("Quick Help did not use the clicked symbol: %q", h.tt.Texts())
	}
	select {
	case pos := <-positions:
		if pos.Line != 1 || pos.Character < helperColumn || pos.Character >= helperColumn+len("helper") {
			t.Fatalf("hover position = %+v; helper column = %d", pos, helperColumn)
		}
	default:
		t.Fatal("hover request did not report its position")
	}
}

func TestQuickHelpAltClickWhitespaceDoesNothing(t *testing.T) {
	h, d, _ := quickHelpHarness(t)
	st := h.a.activeEditor()
	spaceOffset := strings.Index(d.buf.String(), ") {") + 1
	st.GoTo(d.buf.Pos(spaceOffset))
	h.tt.Frame()
	left, ok := h.tt.TextCaret()
	if !ok {
		t.Fatal("whitespace caret is not available")
	}
	st.GoTo(d.buf.Pos(spaceOffset + 1))
	h.tt.Frame()
	right, ok := h.tt.TextCaret()
	if !ok {
		t.Fatal("caret after whitespace is not available")
	}
	before := countCalls(h.fc.calls(), "hover")
	h.tt.ClickAtWith(ui.Alt, (left.X+right.X)/2, left.Y+left.H/2)
	drainHarnessPosts(h)
	h.tt.Frame()
	if h.a.quickHelp != nil || h.a.quickHelpRequest != nil || countCalls(h.fc.calls(), "hover") != before {
		t.Fatalf("Alt-click on whitespace requested Quick Help: calls=%v texts=%q", h.fc.calls(), h.tt.Texts())
	}
}

func TestQuickHelpCombinedAltCommandClickIsNotQuickHelp(t *testing.T) {
	h, d, offset := quickHelpHarness(t)
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(offset + 2))
	h.tt.Frame()
	caret, ok := h.tt.TextCaret()
	if !ok {
		t.Fatal("editor caret is not available")
	}
	before := countCalls(h.fc.calls(), "hover")
	h.tt.ClickAtWith(ui.Alt|ui.Cmd, caret.X, caret.Y+caret.H/2)
	drainHarnessPosts(h)
	h.tt.Frame()
	if h.a.quickHelp != nil || h.a.quickHelpRequest != nil || countCalls(h.fc.calls(), "hover") != before {
		t.Fatalf("Alt+Cmd-click was interpreted as Quick Help: calls=%v texts=%q", h.fc.calls(), h.tt.Texts())
	}
}

func TestQuickHelpMoveOutsideAllowsNormalHover(t *testing.T) {
	h, d, helperOffset := quickHelpHarness(t)
	st := h.a.activeEditor()
	callOffset := strings.Index(d.buf.String(), "call")
	st.GoTo(d.buf.Pos(helperOffset + 2))
	h.tt.Frame()
	helperCaret, ok := h.tt.TextCaret()
	if !ok {
		t.Fatal("helper caret is not available")
	}
	st.GoTo(d.buf.Pos(callOffset + 2))
	h.tt.Frame()
	callCaret, ok := h.tt.TextCaret()
	if !ok {
		t.Fatal("call caret is not available")
	}
	st.GoTo(d.buf.Pos(helperOffset + 2))
	h.a.showQuickHelpAtCaret()
	waitForQuickHelp(t, h, st, "Declaration")

	h.tt.Move(callCaret.X, callCaret.Y+callCaret.H/2)
	h.tt.Advance(350 * time.Millisecond)
	if h.a.quickHelp != nil || h.tt.HasText("Declaration") {
		t.Fatal("Quick Help state remained after MyGo hid its hover")
	}

	h.tt.Move(helperCaret.X, helperCaret.Y+helperCaret.H/2)
	h.tt.Move(callCaret.X, callCaret.Y+callCaret.H/2)
	h.tt.Advance(650 * time.Millisecond)
	waitForText(t, h, "Returns the result.")
	if h.a.quickHelp != nil || h.tt.HasText("Declaration") {
		t.Fatal("normal mouse hover was blocked after Quick Help closed")
	}
}

func TestQuickHelpScrollClearsState(t *testing.T) {
	h, d, offset := quickHelpHarness(t)
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(offset + 2))
	h.tt.Frame()
	caret, ok := h.tt.TextCaret()
	if !ok {
		t.Fatal("editor caret is not available")
	}
	h.a.showQuickHelpAtCaret()
	waitForQuickHelp(t, h, st, "Declaration")
	h.tt.Scroll(caret.X, caret.Y+caret.H/2, 0, 1)
	h.tt.Frame()
	if h.a.quickHelp != nil || h.tt.HasText("Declaration") {
		t.Fatal("Quick Help state remained after scrolling closed its hover")
	}
}

func TestQuickHelpCommandPaletteAndDismissal(t *testing.T) {
	h, d, offset := quickHelpHarness(t)
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(offset + 2))
	h.tt.Frame()
	caret, ok := h.tt.TextCaret()
	if !ok {
		t.Fatal("editor caret is not available")
	}
	h.a.openQuickPanel(">")
	h.a.quick.query = ">Show Quick Help"
	h.a.rankQuick()
	h.tt.Frame()
	h.tt.Key(0, ui.KeyEnter)
	waitForQuickHelp(t, h, st, "Declaration")
	if os.Getenv("SEE_QUICKHELP_SCREENSHOTS") == "1" {
		writeQuickHelpScreenshots(t, h)
	}
	h.tt.ClickAt(12, 12)
	h.tt.Frame()
	if h.tt.HasText("Declaration") {
		t.Fatal("clicking elsewhere did not dismiss Quick Help")
	}

	st.GoTo(d.buf.Pos(offset + 2))
	h.tt.ClickAt(caret.X, caret.Y+caret.H/2)
	h.tt.Frame()
	quickHelpShortcut := jumpShortcut(t, "Show Quick Help")
	h.tt.Key(quickHelpShortcut.shortcutMods, quickHelpShortcut.shortcutKey)
	waitForQuickHelp(t, h, st, "Declaration")
	before := d.buf.String()
	h.tt.TypeKey(0, ui.KeyX, "x")
	h.tt.Frame()
	if d.buf.String() == before {
		t.Fatal("typing did not edit the focused document")
	}
	if h.tt.HasText("Declaration") {
		active, noInputMethod := h.tt.TextInputActive()
		t.Fatalf("editing did not dismiss Quick Help: marker=%p focused=%v textInput=%v/%v buffer=%q", h.a.quickHelp, st.Focused(), active, noInputMethod, d.buf.String())
	}
}

func TestQuickHelpDeclaredInJumpsToDefinition(t *testing.T) {
	h, d, offset := quickHelpHarness(t)
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(offset + 2))
	h.tt.Frame()
	h.a.showQuickHelpAtCaret()
	waitForQuickHelp(t, h, st, "b.go:2")
	if err := h.tt.Click("b.go:2"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool {
		return h.a.activePath() == filepath.Join(h.a.root, "b.go") && h.a.activeEditor() != nil
	})
	if h.a.activeEditor().CursorPos().Line != 1 {
		t.Fatalf("declaration cursor = %+v", h.a.activeEditor().CursorPos())
	}
}

func TestQuickHelpDefinitionLookupIsBounded(t *testing.T) {
	h, d, offset := quickHelpHarness(t)
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(offset + 2))
	h.tt.Frame()
	h.fc.def = func(ctx context.Context, _ string, _ lsp.Position) ([]lsp.Location, error) {
		select {
		case <-time.After(2 * time.Second):
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	started := time.Now()
	h.a.showQuickHelpAtCaret()
	waitForQuickHelp(t, h, st, "Declaration")
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Quick Help waited %v for optional Definition", elapsed)
	}
}

func TestQuickHelpWithoutHover(t *testing.T) {
	for _, result := range []string{"empty", "error"} {
		t.Run(result, func(t *testing.T) {
			h, d, offset := quickHelpHarness(t)
			st := h.a.activeEditor()
			st.GoTo(d.buf.Pos(offset + 2))
			h.tt.Frame()
			done := make(chan struct{})
			h.fc.hover = func(context.Context, string, lsp.Position) (string, *lsp.Range, error) {
				defer close(done)
				if result == "error" {
					return "", nil, errors.New("hover failed")
				}
				return "", nil, nil
			}
			h.a.showQuickHelpAtCaret()
			select {
			case <-done:
			case <-time.After(time.Second * 5):
				t.Fatal("hover request did not finish")
			}
			h.tt.Frame()
			if h.a.quickHelp != nil || h.tt.HasText("Declaration") {
				t.Fatal("Quick Help appeared without hover documentation")
			}
		})
	}
}

func TestQuickHelpRequiresWorkspaceTrust(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.go": "package a\nfunc call() { helper() }\n",
		"b.go": "package a\nfunc helper() int { return 1 }\n",
	})
	h := newHarness(t, root)
	d := h.open("a.go")
	if h.a.cfg.IsTrusted(root) {
		t.Fatal("test workspace unexpectedly trusted")
	}
	st := h.a.activeEditor()
	offset := strings.Index(d.buf.String(), "helper")
	st.GoTo(d.buf.Pos(offset + 2))
	h.tt.Frame()
	before := countCalls(h.fc.calls(), "hover")
	h.a.showQuickHelpAtCaret()
	drainHarnessPosts(h)
	h.tt.Frame()
	if h.a.quickHelp != nil || h.a.quickHelpRequest != nil || countCalls(h.fc.calls(), "hover") != before {
		t.Fatalf("Quick Help queried an untrusted workspace: calls=%v texts=%q", h.fc.calls(), h.tt.Texts())
	}
}

func TestQuickHelpLateResultAfterEscapeIsIgnored(t *testing.T) {
	h, d, offset := quickHelpHarness(t)
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(offset + 2))
	h.tt.Frame()
	started, release := holdQuickHelpHover(h)
	h.a.showQuickHelpAtCaret()
	waitForSignal(t, started, "hover request")
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()
	if h.a.quickHelpRequest != nil {
		t.Fatal("Escape did not cancel the pending Quick Help request")
	}
	drainHarnessPosts(h)
	posted, restore := watchHarnessPosts(h)
	t.Cleanup(restore)
	close(release)
	waitForSignal(t, posted, "late hover result")
	drainHarnessPosts(h)
	h.tt.Frame()
	if h.a.quickHelp != nil || h.tt.HasText("Declaration") {
		t.Fatal("late hover result reopened Quick Help after Escape")
	}
}

func TestQuickHelpLateResultAfterFileSwitchIsIgnored(t *testing.T) {
	h, _, offset := quickHelpHarness(t)
	b := h.open("b.go")
	d := h.open("a.go")
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(offset + 2))
	h.tt.Frame()
	started, release := holdQuickHelpHover(h)
	h.a.showQuickHelpAtCaret()
	waitForSignal(t, started, "hover request")
	h.a.userActivateIn(h.a.focus, b.path)
	h.tt.Frame()
	if h.a.activePath() != b.path {
		t.Fatal("test did not switch to the other file")
	}
	drainHarnessPosts(h)
	posted, restore := watchHarnessPosts(h)
	t.Cleanup(restore)
	close(release)
	waitForQuickHelpRequestClear(t, h, posted)
	h.tt.Frame()
	if h.a.quickHelp != nil || h.tt.HasText("Declaration") {
		t.Fatal("late hover result opened Quick Help after switching files")
	}
}

func quickHelpHarness(t *testing.T) (*harness, *doc, int) {
	t.Helper()
	root := writeTree(t, map[string]string{
		"a.go": "package a\nfunc call() { helper() }\n",
		"b.go": "package a\nfunc helper() int { return 1 }\n",
	})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	h.fc.hover = func(_ context.Context, _ string, pos lsp.Position) (string, *lsp.Range, error) {
		if pos.Line != 1 {
			t.Errorf("hover position = %+v", pos)
		}
		return "```go\nfunc helper() int\n```\n\nReturns the result.\n\nUse it when needed.", &lsp.Range{
			Start: lsp.Position{Line: 1, Character: 14}, End: lsp.Position{Line: 1, Character: 20},
		}, nil
	}
	h.fc.def = func(_ context.Context, _ string, _ lsp.Position) ([]lsp.Location, error) {
		return []lsp.Location{{URI: uri.FromPath(filepath.Join(root, "b.go")), Range: lsp.Range{
			Start: lsp.Position{Line: 1, Character: 5}, End: lsp.Position{Line: 1, Character: 11},
		}}}, nil
	}
	d := h.open("a.go")
	t.Cleanup(func() { h.a.lsp.StopWait(5 * time.Second) })
	offset := strings.Index(d.buf.String(), "helper")
	return h, d, offset
}

func writeQuickHelpScreenshots(t *testing.T, h *harness) {
	t.Helper()
	for _, appearance := range []struct {
		dark bool
		name string
	}{{false, "quickhelp-light.png"}, {true, "quickhelp-dark.png"}} {
		h.tt.SetDark(appearance.dark)
		h.tt.Frame()
		dir := os.Getenv("SEE_QUICKHELP_SCREENSHOT_DIR")
		if dir == "" {
			dir = os.Getenv("HOME")
		}
		f, err := os.Create(filepath.Join(dir, appearance.name))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, h.tt.Image()); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	h.tt.SetDark(false)
	h.tt.Frame()
}

func waitForQuickHelp(t *testing.T, h *harness, st *editor.State, text string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		h.tt.Frame()
		if h.a.quickHelp == st && h.tt.HasText(text) {
			return
		}
		select {
		case fn := <-h.q:
			fn()
			h.ran++
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			state, err := h.a.lsp.Status()
			t.Fatalf("Quick Help did not show %q: calls=%v server=%v/%v trusted=%v active=%q marker=%p texts=%q", text, h.fc.calls(), state, err, h.a.cfg.IsTrusted(h.a.root), h.a.focus.active, h.a.quickHelp, h.tt.Texts())
		}
	}
}

func waitForText(t *testing.T, h *harness, text string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		h.tt.Frame()
		if h.tt.HasText(text) {
			return
		}
		select {
		case fn := <-h.q:
			fn()
			h.ran++
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatalf("text %q did not appear: calls=%v quickHelp=%p texts=%q", text, h.fc.calls(), h.a.quickHelp, h.tt.Texts())
		}
	}
}

func countCalls(calls []string, name string) int {
	count := 0
	for _, call := range calls {
		if call == name {
			count++
		}
	}
	return count
}

func drainHarnessPosts(h *harness) {
	for {
		select {
		case fn := <-h.q:
			fn()
			h.ran++
		default:
			return
		}
	}
}

func holdQuickHelpHover(h *harness) (chan struct{}, chan struct{}) {
	started, release := make(chan struct{}), make(chan struct{})
	h.fc.hover = func(context.Context, string, lsp.Position) (string, *lsp.Range, error) {
		close(started)
		<-release
		return "```go\nfunc helper() int\n```\n\nReturns the result.", nil, nil
	}
	return started, release
}

func watchHarnessPosts(h *harness) (<-chan struct{}, func()) {
	previous := h.a.post
	posted := make(chan struct{}, 16)
	h.a.post = platform.SchedulerFunc(func(fn func()) {
		previous.Post(fn)
		posted <- struct{}{}
	})
	return posted, func() { h.a.post = previous }
}

func waitForSignal(t *testing.T, signal <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func waitForQuickHelpRequestClear(t *testing.T, h *harness, posted <-chan struct{}) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for h.a.quickHelpRequest != nil {
		select {
		case <-posted:
			drainHarnessPosts(h)
		case <-deadline:
			t.Fatal("late response did not clear its pending Quick Help request")
		}
	}
}
