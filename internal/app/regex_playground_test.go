package app

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"see/internal/regexplayground"
	"see/locales"
)

func TestRegexDiagramSemanticLabelsAreLocalized(t *testing.T) {
	cases := []struct {
		class regexplayground.DiagramClass
		want  string
	}{
		{regexplayground.DiagramClassLetter, locales.RegexDiagramLetter()},
		{regexplayground.DiagramClassDigit, "0–9"},
		{regexplayground.DiagramClassWhitespace, locales.RegexDiagramWhitespace()},
		{regexplayground.DiagramClassWord, locales.RegexDiagramWord()},
		{regexplayground.DiagramClassNonDigit, locales.RegexDiagramNonDigit()},
		{regexplayground.DiagramClassNonWhitespace, locales.RegexDiagramNonWhitespace()},
		{regexplayground.DiagramClassNonWord, locales.RegexDiagramNonWord()},
	}
	for _, test := range cases {
		if got := regexDiagramClassLabel(test.class); got != test.want || got == "" || strings.HasPrefix(got, "[") {
			t.Errorf("class %v label = %q, want semantic label %q", test.class, got, test.want)
		}
	}
}

func TestRegexDiagramRepeatLabelsPreserveBoundsAndLaziness(t *testing.T) {
	cases := []struct {
		node regexplayground.DiagramNode
		want string
	}{
		{regexplayground.DiagramNode{RepeatMin: 1, RepeatMax: -1, Label: "+"}, "1+"},
		{regexplayground.DiagramNode{RepeatMin: 0, RepeatMax: -1, Label: "*"}, "0+"},
		{regexplayground.DiagramNode{RepeatMin: 0, RepeatMax: 1, Label: "?"}, locales.RegexDiagramOptional()},
		{regexplayground.DiagramNode{RepeatMin: 2, RepeatMax: 4, Label: "{2,4}?", Lazy: true}, "{2,4} · " + locales.RegexDiagramLazy()},
		{regexplayground.DiagramNode{RepeatMin: 1, RepeatMax: -1, Label: "+?", Lazy: true}, "1+ · " + locales.RegexDiagramLazy()},
	}
	for _, test := range cases {
		if got := regexDiagramRepeatLabel(test.node); got != test.want {
			t.Errorf("repeat label for %+v = %q, want %q", test.node, got, test.want)
		}
	}
}

func TestRegexDiagramOptionalBypassFitsDefaultAndEnlargedTools(t *testing.T) {
	diagram, err := regexplayground.ParseDiagram(`(?P<word>\p{L}+)([0-9]+)?`, regexplayground.Flags{})
	if err != nil {
		t.Fatal(err)
	}
	var layout regexRailLayout
	tt := ui.NewTester(func(c *ui.Context) {
		layout = layoutRegexRail(diagram.Root, nil, false, c)
	}, 680, 420)
	tt.Frame()
	if layout.node.Kind != regexplayground.DiagramSequence || len(layout.children) != 2 || len(layout.positions) != 2 || layout.children[1].node.Kind != regexplayground.DiagramRepeat {
		t.Fatalf("expected sequence ending in optional repeat, got %+v", layout.node)
	}
	optional := layout.children[1]
	if optional.node.RepeatMin != 0 || optional.node.RepeatMax != 1 {
		t.Fatalf("expected optional digit branch, got %+v", optional.node)
	}

	for _, tool := range []struct {
		name   string
		height float32
	}{
		{name: "default 680x420", height: 420},
		{name: "enlarged 920x620", height: 620},
	} {
		maxHeight := regexPlaygroundDiagramMaxHeight(tool.height - 40)
		viewportHeight := regexPlaygroundDiagramViewportHeight(layout.height, maxHeight)
		if layout.height > viewportHeight {
			t.Errorf("%s tool clips full optional diagram: diagram %.1f, viewport %.1f", tool.name, layout.height, viewportHeight)
		}
	}
}

func TestRegexPlaygroundDiagramClearsAndRecovers(t *testing.T) {
	const pattern = `(?P<word>\p{L}+)([0-9]+)?`
	p := &regexPlayground{pattern: pattern}
	p.refreshDiagram()
	if !p.diagramValid {
		t.Fatal("valid pattern did not produce a diagram")
	}
	p.activeUnsupportedFeatures = []string{"lookaround"}
	p.refreshDiagram()
	if p.diagramValid {
		t.Fatal("unsupported pattern state left a diagram visible")
	}
	p.activeUnsupportedFeatures = nil
	p.refreshDiagram()
	if !p.diagramValid {
		t.Fatal("cached pattern did not recover after unsupported state cleared")
	}
	p.result.Error = "sample is too large"
	p.refreshDiagram()
	if !p.diagramValid {
		t.Fatal("sample error invalidated the structurally valid diagram")
	}
	p.pattern = "("
	p.refreshDiagram()
	if p.diagramValid {
		t.Fatal("invalid pattern left the previous diagram visible")
	}
	p.pattern = pattern
	p.refreshDiagram()
	if !p.diagramValid {
		t.Fatal("valid pattern did not recover after syntax error")
	}
}

func TestRegexPlaygroundHighlightsOnFirstSettledPaste(t *testing.T) {
	source := "package main\nimport \"regexp\"\nvar re = regexp.MustCompile(`(?P<word>\\p{L}+)([0-9]+)?`)\n"
	h := newHarness(t, writeTree(t, map[string]string{"a.go": source}))
	d := h.open("a.go")
	st := h.a.activeEditor()
	st.GoTo(st.Buffer.Pos(strings.Index(source, `\p{L}`) + 2))
	h.a.openRegexPlaygroundFor(h.a.focus, d, st)
	h.tt.Frame()
	h.tt.Type("café42 tea")
	p := h.a.regexPlayground
	if p == nil || len(p.result.Matches) != 2 {
		t.Fatalf("first paste matches: %+v", p)
	}
	if !h.tt.HasText(locales.RegexMatch()+" 1") || !h.tt.HasText(locales.RegexCaptures()) {
		t.Fatalf("first paste results are not visible: %v", h.tt.Texts())
	}
	firstFrame := regexPlaygroundSamplePixels(t, h)

	p.pattern = "("
	p.patternVersion++
	h.tt.Frame()
	h.tt.Frame()
	if p.result.Error == "" || p.diagramValid || !h.tt.HasText(locales.RegexDiagramUnavailable()) {
		t.Fatal("invalid pattern did not clear the sample highlights")
	}
	withoutHighlights := regexPlaygroundSamplePixels(t, h)
	if sameRegexPlaygroundPixels(firstFrame, withoutHighlights) {
		t.Fatal("first settled paste did not render match highlights")
	}
}

func regexPlaygroundSamplePixels(t *testing.T, h *harness) []uint32 {
	t.Helper()
	bounds, ok := h.tt.Find(locales.RegexSample())
	if !ok {
		t.Fatal("sample editor not found")
	}
	img := h.tt.Image()
	sx := float64(img.Bounds().Dx()) / harnessWidth
	sy := float64(img.Bounds().Dy()) / harnessHeight
	x0 := max(img.Bounds().Min.X, int(float64(bounds.X)*sx))
	y0 := max(img.Bounds().Min.Y, int(float64(bounds.Y)*sy))
	x1 := min(img.Bounds().Max.X, int(float64(bounds.X+bounds.W)*sx))
	y1 := min(img.Bounds().Max.Y, int(float64(bounds.Y+bounds.H)*sy))
	if x0 >= x1 || y0 >= y1 {
		t.Fatalf("sample editor has empty image bounds: %+v", bounds)
	}
	pixels := make([]uint32, 0, (x1-x0)*(y1-y0))
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			c := img.RGBAAt(x, y)
			pixels = append(pixels, uint32(c.R)<<24|uint32(c.G)<<16|uint32(c.B)<<8|uint32(c.A))
		}
	}
	return pixels
}

func sameRegexPlaygroundPixels(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRegexPlaygroundPaletteEditAndEscape(t *testing.T) {
	source := "package main\nimport \"regexp\"\nvar re = regexp.MustCompile(`(?P<word>\\p{L}+)`)\n"
	h := newHarness(t, writeTree(t, map[string]string{"a.go": source}))
	d := h.open("a.go")
	group := h.a.focus
	st := group.eds[d.path]
	st.GoTo(st.Buffer.Pos(strings.Index(source, `\p{L}`) + 2))
	h.a.openPalette()
	h.a.quick.query = ">" + commandLabel("regex.playground")
	h.a.rankQuick()
	h.tt.Frame()
	if err := h.tt.Click(commandLabel("regex.playground")); err != nil {
		t.Fatalf("click Test Regex: %v; visible=%v", err, h.tt.Texts())
	}
	h.tt.Frame()
	p := h.a.regexPlayground
	if p == nil || p.pattern != `(?P<word>\p{L}+)` {
		t.Fatalf("playground pattern = %v", p)
	}
	if !h.tt.HasText(locales.RegexEngineBadge()) || !h.tt.HasText(locales.RegexEngineNote()) {
		t.Fatalf("engine disclosure missing: %v", h.tt.Texts())
	}
	h.tt.Type("focused")
	h.tt.Frame()
	if p.sample.Buffer.String() != "focused" {
		t.Fatalf("opening did not focus the sample editor: %q", p.sample.Buffer.String())
	}
	if err := h.tt.Click(locales.RegexPattern()); err != nil {
		t.Fatalf("click pattern: %v", err)
	}
	h.tt.Key(ui.Cmd, ui.KeyA)
	h.tt.Type(`(?P<word>\p{L}+)([0-9]+)?`)
	h.tt.Frame()
	if err := h.tt.Click(locales.RegexSample()); err != nil {
		t.Fatalf("click sample: %v", err)
	}
	h.tt.Key(ui.Cmd, ui.KeyA)
	h.tt.Type("café42 tea")
	h.tt.Frame()
	if p.pattern != `(?P<word>\p{L}+)([0-9]+)?` || p.sample.Buffer.String() != "café42 tea" {
		t.Fatalf("inputs: pattern=%q sample=%q", p.pattern, p.sample.Buffer.String())
	}
	if len(p.result.Matches) != 2 || len(p.result.Matches[0].Captures) != 3 {
		t.Fatalf("matches: %+v", p.result)
	}
	if !h.tt.HasText(capturePreview("café")) || !h.tt.HasText(capturePreview("42")) {
		t.Fatalf("selected capture values are not visible: %v", h.tt.Texts())
	}
	first, second := p.result.Matches[0], p.result.Matches[1]
	if first.Start != 0 || first.End != len("café42") || first.Captures[1].Name != "word" || !first.Captures[2].Matched || first.Captures[2].Value != "42" {
		t.Fatalf("first match: %+v", first)
	}
	if second.Start != len("café42 ") || second.End != len("café42 tea") || second.Captures[1].Value != "tea" || second.Captures[2].Matched {
		t.Fatalf("second match: %+v", second)
	}
	if !h.tt.HasText(regexCharacterRange("café42 tea", first.Start, first.End)) || !h.tt.HasText(regexCharacterRange("café42 tea", second.Start, second.End)) {
		t.Fatalf("character ranges missing from match list: %v", h.tt.Texts())
	}
	if len(p.sample.TextBackgrounds) != 4 {
		t.Fatalf("sample highlights = %d", len(p.sample.TextBackgrounds))
	}
	wantHighlights := [][2]int{{0, len("café42")}, {0, len("café")}, {len("café"), len("café42")}, {len("café42 "), len("café42 tea")}}
	for i, want := range wantHighlights {
		got := p.sample.TextBackgrounds[i]
		if got.Start != want[0] || got.End != want[1] {
			t.Errorf("sample highlight %d = [%d,%d), want [%d,%d)", i, got.Start, got.End, want[0], want[1])
		}
	}
	if p.sample.TextBackgrounds[1].Color == p.sample.TextBackgrounds[2].Color || p.sample.TextBackgrounds[1].Color == p.sample.TextBackgrounds[3].Color {
		t.Fatal("capture colors are not distinct from each other and neutral matches")
	}
	if d.buf.String() != source || len(h.a.dirtyDocs()) != 0 {
		t.Fatal("opening or editing the playground changed source")
	}
	if output := os.Getenv("SEE_REGEX_PLAYGROUND_SCREENSHOT_DIR"); output != "" {
		saveRegexPlaygroundScreenshot(t, h, filepath.Join(output, "regex-playground-light.png"))
		h.tt.SetDark(true)
		h.tt.Frame()
		saveRegexPlaygroundScreenshot(t, h, filepath.Join(output, "regex-playground-dark.png"))
	}
	if err := h.tt.Click(regexCharacterRange("café42 tea", second.Start, second.End)); err != nil {
		t.Fatalf("select optional capture's unmatched result: %v", err)
	}
	h.tt.Frame()
	if p.selected != 1 || p.result.Matches[p.selected].Captures[2].Matched || !h.tt.HasText(locales.RegexNoCapture()) {
		t.Fatalf("selecting tea did not show the optional group as uncaptured: selected=%d texts=%v", p.selected, h.tt.Texts())
	}
	if err := h.tt.Click(locales.RegexSample()); err != nil {
		t.Fatalf("click sample for empty state: %v", err)
	}
	h.tt.Key(ui.Cmd, ui.KeyA)
	h.tt.Key(0, ui.KeyBackspace)
	h.tt.Frame()
	if !h.tt.HasText(locales.RegexEmptySample()) {
		t.Fatalf("empty sample state missing: %v", h.tt.Texts())
	}
	h.tt.Type("nomatch")
	if err := h.tt.Click(locales.RegexPattern()); err != nil {
		t.Fatalf("click pattern for no-match case: %v", err)
	}
	h.tt.Key(ui.Cmd, ui.KeyA)
	h.tt.Type("zzzz")
	h.tt.Frame()
	if !h.tt.HasText(locales.RegexNoMatches()) {
		t.Fatalf("no-match state missing: %v", h.tt.Texts())
	}
	if err := h.tt.Click(locales.RegexPattern()); err != nil {
		t.Fatalf("click pattern for invalid case: %v", err)
	}
	h.tt.Key(ui.Cmd, ui.KeyA)
	h.tt.Type("(")
	h.tt.Frame()
	if p.result.Error == "" || !h.tt.HasText(locales.RegexInvalidPattern()) {
		t.Fatalf("invalid pattern state: %+v; visible=%v", p.result, h.tt.Texts())
	}
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()
	if h.a.regexPlayground != nil || h.a.focus != group {
		t.Fatal("Escape did not close the playground")
	}
	cursor := st.Cursor()
	h.tt.Key(0, ui.KeyRight)
	if st.Cursor() == cursor || d.buf.String() != source {
		t.Fatal("Escape did not restore focus to the unchanged source editor")
	}
}

func TestRegexPlaygroundAlternationDiagramScreenshots(t *testing.T) {
	source := "package main\nimport \"regexp\"\nvar re = regexp.MustCompile(`(?P<word>\\p{L}+)|cat`)\n"
	h := newHarness(t, writeTree(t, map[string]string{"a.go": source}))
	d := h.open("a.go")
	st := h.a.focus.eds[d.path]
	st.GoTo(st.Buffer.Pos(strings.Index(source, `\p{L}`) + 2))
	h.a.openRegexPlaygroundFor(h.a.focus, d, st)
	h.tt.Frame()
	h.tt.Type("tea cat")
	h.tt.Frame()
	p := h.a.regexPlayground
	if p == nil || !p.diagramValid || p.diagram.Root.Kind != regexplayground.DiagramAlternate || len(p.result.Matches) != 2 {
		t.Fatalf("alternation diagram or matches unavailable: %+v", p)
	}
	if output := os.Getenv("SEE_REGEX_PLAYGROUND_SCREENSHOT_DIR"); output != "" {
		saveRegexPlaygroundScreenshot(t, h, filepath.Join(output, "regex-playground-alternation-light.png"))
		h.tt.SetDark(true)
		h.tt.Frame()
		saveRegexPlaygroundScreenshot(t, h, filepath.Join(output, "regex-playground-alternation-dark.png"))
	}
}

func TestRegexPlaygroundCaptureBackgroundsUseUnicodeRangesAndIgnoreZeroWidth(t *testing.T) {
	source := "package main\nimport \"regexp\"\nvar re = regexp.MustCompile(`(?P<outer>café(?P<inner>42)(?P<empty>))|(?P<tea>tea)`)\n"
	h := newHarness(t, writeTree(t, map[string]string{"a.go": source}))
	d := h.open("a.go")
	st := h.a.focus.eds[d.path]
	st.GoTo(st.Buffer.Pos(strings.Index(source, "café") + 1))
	h.a.openRegexPlaygroundFor(h.a.focus, d, st)
	h.tt.Frame()
	h.tt.Type("café42 tea")
	h.tt.Frame()
	p := h.a.regexPlayground
	if p == nil || len(p.sample.TextBackgrounds) != 4 {
		t.Fatalf("capture backgrounds = %+v", p)
	}
	want := [][2]int{{0, len("café42")}, {0, len("café")}, {len("café"), len("café42")}, {len("café42 "), len("café42 tea")}}
	for i, rangeWant := range want {
		got := p.sample.TextBackgrounds[i]
		if got.Start != rangeWant[0] || got.End != rangeWant[1] {
			t.Fatalf("background %d = [%d,%d), want [%d,%d)", i, got.Start, got.End, rangeWant[0], rangeWant[1])
		}
		if got.Start == got.End {
			t.Fatalf("zero-width capture painted a false range: %+v", got)
		}
	}
	if p.result.Matches[0].Captures[3].Start != p.result.Matches[0].Captures[3].End {
		t.Fatal("test did not produce its intended zero-width capture")
	}
}

func TestRegexPlaygroundStaysWithItsDocumentAndEditor(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{
		"a.go": "package a\nimport \"regexp\"\nvar re = regexp.MustCompile(`word`)\n",
		"b.go": "package b\n",
	}))
	d := h.open("a.go")
	g := h.a.focus
	st := g.eds[d.path]
	st.GoTo(st.Buffer.Pos(strings.Index(d.buf.String(), "word")))
	h.a.openRegexPlaygroundFor(g, d, st)
	h.tt.Frame()
	p := h.a.regexPlayground
	if p == nil || !h.tt.HasText(locales.RegexPlaygroundTitle()) {
		t.Fatal("playground did not open for its source document")
	}
	h.open("b.go")
	if h.tt.HasText(locales.RegexToolWindow()) {
		t.Fatal("playground escaped its initiating document after switching tabs")
	}
	h.a.activateIn(g, d.path)
	h.tt.Frame()
	if h.a.regexPlayground != p || !h.tt.HasText(locales.RegexToolWindow()) {
		t.Fatal("playground did not return with its initiating document")
	}
	h.a.splitActive(sideRight)
	other := h.a.focus
	h.tt.Frame()
	tool, ok := h.tt.Find(locales.RegexToolWindow())
	if !ok {
		t.Fatal("playground is missing from the initiating editor after split")
	}
	sourceBounds, otherBounds := regexToolGroupBounds(t, h, g), regexToolGroupBounds(t, h, other)
	workspaceLeft, workspaceTop := min(sourceBounds.X, otherBounds.X), min(sourceBounds.Y, otherBounds.Y)
	workspaceRight := max(sourceBounds.X+sourceBounds.W, otherBounds.X+otherBounds.W)
	workspaceBottom := max(sourceBounds.Y+sourceBounds.H, otherBounds.Y+otherBounds.H)
	if p.group != g || p.doc != d || other == g || textCount(h.tt.Texts(), locales.RegexPlaygroundTitle()) != 1 ||
		tool.X < workspaceLeft || tool.Y < workspaceTop || tool.X+tool.W > workspaceRight || tool.Y+tool.H > workspaceBottom {
		t.Fatal("playground lost its source ownership or workspace bounds in the split layout")
	}
	if d.buf.String() != "package a\nimport \"regexp\"\nvar re = regexp.MustCompile(`word`)\n" || len(h.a.dirtyDocs()) != 0 {
		t.Fatal("tab or group changes modified source")
	}
}

func TestRegexPlaygroundRejectsJavaScriptUnicodePropertyEscapeWithoutU(t *testing.T) {
	source := "const letters = /\\p{L}/;\n"
	h := newHarness(t, writeTree(t, map[string]string{"a.js": source}))
	d := h.open("a.js")
	st := h.a.focus.eds[d.path]
	st.GoTo(st.Buffer.Pos(strings.Index(source, `\p{L}`) + 2))
	h.a.openRegexPlaygroundFor(h.a.focus, d, st)
	h.tt.Frame()
	p := h.a.regexPlayground
	unsupportedLabel := strings.Split(locales.RegexUnsupportedFeatures(), "%s")[0]
	if p == nil || p.result.Error != "unsupported JavaScript features" || !h.tt.HasText(unsupportedLabel) {
		t.Fatalf("unsupported JS syntax was not rejected: playground=%+v visible=%v", p, h.tt.Texts())
	}
}

func TestRegexPlaygroundContextualAction(t *testing.T) {
	source := "package main\nimport \"regexp\"\nvar re = regexp.MustCompile(`word`)\n"
	h := newHarness(t, writeTree(t, map[string]string{"a.go": source}))
	d := h.open("a.go")
	st := h.a.focus.eds[d.path]
	st.GoTo(st.Buffer.Pos(strings.Index(source, "word") + 1))
	h.tt.Frame()
	label := commandLabel("regex.playground")
	if !h.tt.HasText(label) {
		t.Fatalf("contextual action missing at regex caret: %v", h.tt.Texts())
	}
	if err := h.tt.Click(label); err != nil {
		t.Fatalf("click contextual action: %v", err)
	}
	h.tt.Frame()
	if p := h.a.regexPlayground; p == nil || p.pattern != "word" {
		t.Fatalf("contextual action opened wrong pattern: %+v", p)
	}
}

func TestRegexPlaygroundCopiesLongCaptureAndUsesSingularHeading(t *testing.T) {
	source := "package main\nimport \"regexp\"\nvar re = regexp.MustCompile(`(?P<long>é+)`)\n"
	h := newHarness(t, writeTree(t, map[string]string{"a.go": source}))
	d := h.open("a.go")
	st := h.a.focus.eds[d.path]
	st.GoTo(st.Buffer.Pos(strings.Index(source, "é") + 1))
	h.a.openRegexPlaygroundFor(h.a.focus, d, st)
	h.tt.Frame()
	p := h.a.regexPlayground
	if p == nil {
		t.Fatal("playground did not open")
	}
	sample := strings.Repeat("é", 80)
	h.tt.Type(sample)
	h.tt.Frame()
	if err := h.tt.Click("Copy capture 1"); err != nil {
		t.Fatalf("click long capture copy: %v; visible=%v", err, h.tt.Texts())
	}
	h.tt.Frame()
	if got := h.tt.Clipboard(); got != sample {
		t.Fatalf("copied capture = %q, want full %q", got, sample)
	}
}

func TestRegexPlaygroundCopiesLongWholeMatchWithoutCaptures(t *testing.T) {
	source := "package main\nimport \"regexp\"\nvar re = regexp.MustCompile(`a+`)\n"
	h := newHarness(t, writeTree(t, map[string]string{"a.go": source}))
	d := h.open("a.go")
	st := h.a.focus.eds[d.path]
	st.GoTo(st.Buffer.Pos(strings.Index(source, "a+")))
	h.a.openRegexPlaygroundFor(h.a.focus, d, st)
	h.tt.Frame()
	p := h.a.regexPlayground
	if p == nil {
		t.Fatal("playground did not open")
	}
	sample := strings.Repeat("a", 80)
	h.tt.Type(sample)
	h.tt.Frame()
	if err := h.tt.Click(locales.Copy()); err != nil {
		t.Fatalf("click whole-match copy: %v; visible=%v", err, h.tt.Texts())
	}
	h.tt.Frame()
	if got := h.tt.Clipboard(); got != sample {
		t.Fatalf("copied whole match = %q, want full %q", got, sample)
	}
}

func TestRegexPlaygroundSingularMatchHeading(t *testing.T) {
	source := "package main\nimport \"regexp\"\nvar re = regexp.MustCompile(`a`)\n"
	h := newHarness(t, writeTree(t, map[string]string{"a.go": source}))
	d := h.open("a.go")
	st := h.a.focus.eds[d.path]
	st.GoTo(st.Buffer.Pos(strings.LastIndex(source, "`a`") + 1))
	h.a.openRegexPlaygroundFor(h.a.focus, d, st)
	h.tt.Frame()
	h.tt.Type("a")
	h.tt.Frame()
	texts := h.tt.Texts()
	if textCount(texts, "Match 1") != 1 || textCount(texts, "Matches 1") != 0 {
		t.Fatalf("match detail heading is not singular: %v", texts)
	}
}

func textCount(texts []string, want string) int {
	count := 0
	for _, text := range texts {
		if text == want {
			count++
		}
	}
	return count
}

func saveRegexPlaygroundScreenshot(t *testing.T, h *harness, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, h.tt.Image()); err != nil {
		t.Fatal(err)
	}
	t.Logf("Screenshot: %s", path)
}
