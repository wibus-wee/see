package app

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/editor"
	"see/internal/lsp"
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
