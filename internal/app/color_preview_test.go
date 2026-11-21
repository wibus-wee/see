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

	"see/internal/colorliteral"
	"see/internal/lsp"
	"see/locales"
)

func colorPreviewHarness(t *testing.T, files map[string]string) (*harness, *doc, *editor.State) {
	t.Helper()
	h := newHarness(t, writeTree(t, files))
	d := h.open("colors.css")
	return h, d, h.a.activeEditor()
}

func colorPreviewButton(raw string) string {
	return fmt.Sprintf(locales.ColorPreviewButton(), raw)
}

func TestColorPreviewColorKeepsAlpha(t *testing.T) {
	if got := colorliteralColor(colorliteral.Color{R: 139, G: 92, B: 246, A: 128}).A; got != 128 {
		t.Fatalf("preview color alpha = %d, want 128", got)
	}
}

func TestColorPreviewWithoutLiteral(t *testing.T) {
	h, _, _ := colorPreviewHarness(t, map[string]string{"colors.css": "body { color: rebeccapurple; }\n"})
	if _, ok := h.tt.Access(colorPreviewButton("rebeccapurple")); ok {
		t.Fatal("a named color was incorrectly treated as a literal")
	}
}

func TestColorPreviewPopoverAndTabSwitch(t *testing.T) {
	h, _, _ := colorPreviewHarness(t, map[string]string{
		"colors.css": "/* λ */\n#0000\n",
		"other.css":  "body {}\n",
	})
	g := h.a.focus
	label := colorPreviewButton("#0000")
	if _, ok := h.tt.Access(label); !ok {
		t.Fatalf("color swatch %q is not accessible", label)
	}
	if err := h.tt.Click(label); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"#0000", "#00000000", locales.ColorPreviewLight(), locales.ColorPreviewDark()} {
		if !h.tt.HasText(text) {
			t.Errorf("preview does not show %q: %q", text, h.tt.Texts())
		}
	}
	if g.colorPreview == nil || !g.colorPreview.open {
		t.Fatal("clicking the swatch did not open its preview")
	}
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()
	if g.colorPreview != nil || h.tt.HasText(locales.ColorPreviewLight()) {
		t.Fatal("Escape did not dismiss the color preview")
	}
	if err := h.tt.Click(label); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.Click(locales.ColorPreviewClose()); err != nil {
		t.Fatal(err)
	}
	if g.colorPreview != nil {
		t.Fatal("the close button left the color preview open")
	}
	if err := h.tt.Click(label); err != nil {
		t.Fatal(err)
	}
	h.open("other.css")
	if g.colorPreview != nil || h.tt.HasText(locales.ColorPreviewLight()) {
		t.Fatal("switching tabs left a stale color preview")
	}
}

func TestColorPreviewFocusesSplit(t *testing.T) {
	h, _, _ := colorPreviewHarness(t, map[string]string{
		"colors.css": "#112233\n",
		"other.css":  "#abcdef\n",
	})
	left := h.a.focus
	right := h.a.splitBeside(left, sideRight)
	h.a.focus = right
	h.open("other.css")
	h.tt.Frame()
	h.a.focus = left
	if err := h.tt.Click(colorPreviewButton("#abcdef")); err != nil {
		t.Fatal(err)
	}
	if h.a.focus != right || right.colorPreview == nil || !h.tt.HasText(locales.ColorPreviewLight()) {
		t.Fatal("clicking a swatch in the other split did not focus it and open the preview")
	}
}

func TestColorPreviewPreservesLiteralEditingAndCopy(t *testing.T) {
	const source = "/* λ */ #123456\n"
	h, d, st := colorPreviewHarness(t, map[string]string{"colors.css": source})
	start := strings.Index(source, "#123456")
	st.SetSelections(editor.Selection{Anchor: d.buf.Len(), Head: d.buf.Len()})
	h.tt.Frame()
	glyph, ok := h.tt.Find("#")
	if !ok {
		t.Fatal("replacement did not render the literal's # glyph")
	}
	h.tt.ClickAt(glyph.X+glyph.W/2, glyph.Y+glyph.H/2)
	h.tt.Frame()
	caret := d.buf.Offset(st.CursorPos())
	if caret < start || caret > start+1 {
		t.Fatalf("clicking the displayed glyph placed caret at %d, want %d or %d", caret, start, start+1)
	}
	st.SetSelections(editor.Selection{Anchor: start + 1, Head: start + 1})
	h.tt.Frame()
	h.tt.Key(0, ui.KeyBackspace)
	h.tt.Frame()
	if got, want := d.buf.String(), strings.Replace(source, "#", "", 1); got != want {
		t.Fatalf("Backspace changed literal to %q, want %q", got, want)
	}
	h.tt.Key(ui.Cmd, ui.KeyZ)
	h.tt.Frame()
	if got := d.buf.String(); got != source {
		t.Fatalf("undo produced %q, want %q", got, source)
	}
	st.SetSelections(editor.Selection{Anchor: start, Head: start + len("#123456")})
	h.tt.Frame()
	h.tt.Key(ui.Cmd, ui.KeyC)
	h.tt.Frame()
	if got := h.tt.Clipboard(); got != "#123456" {
		t.Fatalf("copy selected %q, want %q", got, "#123456")
	}
	st.Find("#123456")
	if count, _ := st.FindMatches(); count != 1 {
		t.Fatalf("find saw %d matches, want 1", count)
	}
}

func TestColorPreviewDismissesAfterEditAndWithTypeInlay(t *testing.T) {
	root := writeTree(t, map[string]string{
		"main.go": "package main\n\nfunc main() {\n\t_ = \"#0000\"\n}\n",
	})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	h.fc.inlayHints = func(context.Context, string, lsp.Range) ([]lsp.InlayHint, error) {
		return []lsp.InlayHint{{Position: lsp.Position{Line: 3, Character: 11}, Label: ": string"}}, nil
	}
	d := h.open("main.go")
	h.settle(func() bool {
		h.tt.Frame()
		return len(d.languageInlays) > 0
	})
	st := h.a.activeEditor()
	g := h.a.focus
	if got := len(d.languageInlays); got != 1 {
		t.Fatalf("got %d type inlays, want 1", got)
	}
	label := colorPreviewButton("#0000")
	if err := h.tt.Click(label); err != nil {
		t.Fatal(err)
	}
	if g.colorPreview == nil || !h.tt.HasText(locales.ColorPreviewLight()) {
		t.Fatal("the color preview did not open alongside the type inlay")
	}
	st.SetSelections(editor.Selection{Anchor: d.buf.Len(), Head: d.buf.Len()})
	h.tt.Frame()
	h.tt.Type("x")
	h.tt.Frame()
	if g.colorPreview != nil || !strings.HasSuffix(d.buf.String(), "x") {
		t.Fatal("editing the document did not dismiss the preview or preserve the edit")
	}
}

func TestColorPreviewScreenshots(t *testing.T) {
	dir := os.Getenv("SEE_COLOR_PREVIEW_SHOTS")
	if dir == "" {
		t.Skip("SEE_COLOR_PREVIEW_SHOTS not set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, appearance := range []struct {
		dark bool
		name string
	}{{false, "color-preview-light.png"}, {true, "color-preview-dark.png"}} {
		h, _, _ := colorPreviewHarness(t, map[string]string{"colors.css": ":root {\n  --accent: #8b5cf6;\n  --accent-soft: rgba(139,92,246,.5);\n  --clear: #0000;\n  --surface: #1f2937;\n}\n"})
		h.tt.SetDark(appearance.dark)
		h.tt.Frame()
		if err := h.tt.Click(colorPreviewButton("rgba(139,92,246,.5)")); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, appearance.name)
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, h.tt.Image()); err != nil {
			f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", path)
	}
}
