package app

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/theme"
	"see/locales"
)

func minimapSource(lines int) string {
	var b strings.Builder
	b.WriteString("package big\n\nimport \"fmt\"\n\n")
	for i := 4; i < lines; i += 6 {
		fmt.Fprintf(&b, "// F%d prints its number.\nfunc F%d(n int) string {\n\tif n > %d {\n\t\treturn fmt.Sprint(n)\n\t}\n}\n", i, i, i)
	}
	return b.String()
}

func insertLine(d *doc, line int, text string) {
	off := d.buf.Snapshot().LineStart(line)
	s := d.buf.String()
	d.buf.Set(s[:off] + text + s[off:])
}

func absDiff(a, b uint8) int { return max(int(a)-int(b), int(b)-int(a)) }

func minimapReady(h *harness, g *group, d *doc) *minimap {
	h.t.Helper()
	var m *minimap
	h.settle(func() bool {
		h.tt.Frame()
		m = g.minimaps[d.path]
		return m != nil && m.bmp != nil && !m.busy && m.version == d.buf.Version() && m.language == d.buf.Language()
	})
	return m
}

func TestMinimapRendersAfterLanguageChanges(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte(strings.Repeat("# Heading\nSome text.\n\n", 300)), 0o644); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	d := h.open("README.md")
	g := h.a.focus
	m := minimapReady(h, g, d)
	if m.language != d.buf.Language() {
		t.Fatalf("minimap language = %v, want %v", m.language, d.buf.Language())
	}

	h.a.selectDocumentLanguage(d, nil)
	m = minimapReady(h, g, d)
	if m.language != nil {
		t.Fatalf("minimap language after switching to plain text = %v, want nil", m.language)
	}
}

func TestMinimapScrollsLargeFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "big.go"), []byte(minimapSource(60000)), 0o644); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	d := h.open("big.go")
	g, st := h.a.focus, h.a.activeEditor()
	m := minimapReady(h, g, d)
	info, ok := h.tt.Access(locales.MinimapLabel())
	if !ok {
		t.Fatal("no minimap")
	}
	r := info.Bounds
	if r.W != minimapWidth || r.X+r.W < harnessWidth-1 {
		t.Fatalf("minimap bounds %+v", r)
	}
	if m.to-m.from > minimapMaxLines || m.from != 0 {
		t.Fatalf("rendered lines %d-%d of a %d-line file", m.from, m.to, d.buf.LineCount())
	}

	// A press below the view box jumps there; the box follows the drag.
	h.tt.Press(r.X+r.W/2, r.Y+r.H-4)
	h.tt.Frame()
	bottom := st.ScrollY()
	if max := st.LineTop(d.buf.LineCount()) - st.ViewportHeight(); bottom < max*0.95 {
		t.Fatalf("press near the end scrolled to %v of %v", bottom, max)
	}
	h.tt.Move(r.X+r.W/2, r.Y+r.H/2)
	h.tt.Frame()
	h.tt.Release(r.X+r.W/2, r.Y+r.H/2)
	h.tt.Frame()
	mid := st.ScrollY()
	if mid <= 0 || mid >= bottom {
		t.Fatalf("drag to the middle scrolled to %v (bottom %v)", mid, bottom)
	}
	m = minimapReady(h, g, d)
	first, _ := st.VisibleLines()
	if first < m.from || first >= m.to || m.to-m.from > minimapMaxLines {
		t.Fatalf("rendered %d-%d around line %d", m.from, m.to, first)
	}

	h.tt.Wheel(r.X+r.W/2, r.Y+r.H/2, 0, 120)
	h.tt.Frame()
	if st.ScrollY() <= mid {
		t.Fatalf("wheel over the minimap did not scroll the editor: %v", st.ScrollY())
	}
}

func TestMinimapMarksAndSetting(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": minimapSource(400)})
	h := newHarness(t, root)
	h.readGit()
	d := h.open("a.go")
	g, st := h.a.focus, h.a.activeEditor()
	h.settle(func() bool { return d.gitChanges != nil })
	line := 4
	insertLine(d, line, "// added\n")
	h.settle(func() bool { h.tt.Frame(); return d.gitChanges != nil && len(d.gitChanges.Hunks()) == 1 })
	m := minimapReady(h, g, d)

	info, _ := h.tt.Access(locales.MinimapLabel())
	r := info.Bounds
	px := h.tt.Image().RGBAAt(int(r.X+3), int(r.Y+float32(line)*minimapRow+1))
	if want := h.a.edTheme.Added; absDiff(px.R, want.R)+absDiff(px.G, want.G)+absDiff(px.B, want.B) > 40 {
		t.Fatalf("Git change mark %v, want about %v", px, want)
	}

	d.diagnosticVersion = d.buf.Version()
	d.diagnostics = []editor.Diagnostic{{Start: d.buf.Snapshot().LineStart(10), End: d.buf.Snapshot().LineStart(10) + 3, Severity: editor.SeverityError}}
	st.Find("F100(")
	h.settle(func() bool { h.tt.Frame(); return !m.matchBusy && m.matchKey.query == "F100(" })
	if len(m.diagLines) != 1 || m.diagLines[0].line != 10 {
		t.Fatalf("diagnostic marks: %+v", m.diagLines)
	}
	if len(m.matches) != 1 || !strings.Contains(d.buf.Line(m.matches[0]), "F100(") {
		t.Fatalf("match marks: %v", m.matches)
	}

	h.a.cfg.HideMinimap = true
	h.tt.Frame()
	if _, ok := h.tt.Access(locales.MinimapLabel()); ok {
		t.Fatal("hidden minimap still shows")
	}
	h.a.cfg.HideMinimap = false
	h.tt.Frame()
	if _, ok := h.tt.Access(locales.MinimapLabel()); !ok {
		t.Fatal("minimap did not come back")
	}
}

func TestMinimapLayout(t *testing.T) {
	l := layoutMinimap(100, 600, 0, 300, 1500)
	if l.off != 0 || l.boxY != 0 || l.track != 300 || l.boxH != 60 {
		t.Fatalf("short file: %+v", l)
	}
	if y := l.scrollFor(l.track - l.boxH); y != l.maxScroll {
		t.Fatalf("scrollFor end: %v", y)
	}
	l = layoutMinimap(10000, 600, 75000, 900, 150000)
	if l.boxY <= 0 || l.boxY >= l.track-l.boxH || l.off <= 0 {
		t.Fatalf("long file: %+v", l)
	}
	if got := l.scrollFor(l.boxY); got < 74999 || got > 75001 {
		t.Fatalf("round trip: %v", got)
	}
}

// TestMinimapScreenshots writes the editor with its minimap in light and
// dark to SEE_MINIMAP_SHOTS, for review.
func TestMinimapScreenshots(t *testing.T) {
	dir := os.Getenv("SEE_MINIMAP_SHOTS")
	if dir == "" {
		t.Skip("SEE_MINIMAP_SHOTS not set")
	}
	src, err := os.ReadFile("editor_area.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{false, true} {
		h := newHarness(t, gitRepo(t, map[string]string{"editor_area.go": string(src)}))
		h.tt.SetDark(dark)
		h.readGit()
		d := h.open("editor_area.go")
		g, st := h.a.focus, h.a.activeEditor()
		h.settle(func() bool { return d.gitChanges != nil })
		insertLine(d, 60, "\t// Minimap preview edit.\n")
		d.diagnosticVersion = d.buf.Version()
		d.diagnostics = []editor.Diagnostic{
			{Start: d.buf.Snapshot().LineStart(140), End: d.buf.Snapshot().LineStart(140) + 4, Severity: editor.SeverityError},
			{Start: d.buf.Snapshot().LineStart(260), End: d.buf.Snapshot().LineStart(260) + 4, Severity: editor.SeverityWarning},
		}
		st.SetDiagnostics(d.diagnosticVersion, d.diagnostics)
		st.Find("minimap")
		st.SetScrollY(st.LineTop(200))
		m := minimapReady(h, g, d)
		h.settle(func() bool {
			h.tt.Frame()
			return !m.matchBusy && m.matchKey.query != "" && len(d.gitChanges.Hunks()) > 0
		})
		h.tt.Frame()
		name := "minimap-light.png"
		if dark {
			name = "minimap-dark.png"
		}
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, h.tt.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}

func BenchmarkMinimapRender(b *testing.B) {
	buf := editor.NewBuffer(minimapSource(200000))
	buf.SetLanguage(editor.LanguageFor("big.go"))
	_, t := theme.New(ui.LightTheme(), 1)
	snap, lang := buf.Snapshot(), buf.Language()
	for b.Loop() {
		drawMinimap(snap, lang, t, 100000, 100000+minimapMaxLines)
	}
}
