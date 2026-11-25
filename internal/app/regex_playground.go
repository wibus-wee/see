package app

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/regexplayground"
	"see/locales"
)

type regexPlayground struct {
	group                     *group
	doc                       *doc
	sourceEditor              *editor.State
	line                      int
	pattern                   string
	patternVersion            uint64
	unsupportedFlags          []string
	javascript                bool
	activeUnsupportedFeatures []string
	flags                     regexplayground.Flags
	sample                    *editor.State
	selected                  int
	cachePatternVersion       uint64
	cacheSampleVersion        uint64
	cacheSample               string
	cacheFlags                regexplayground.Flags
	cached                    bool
	result                    regexplayground.Result
	diagramPattern            string
	diagramFlags              regexplayground.Flags
	diagramCached             bool
	diagramParseFailed        bool
	diagram                   regexplayground.Diagram
	diagramValid              bool
	backgroundPattern         string
	backgroundPatternVersion  uint64
	backgroundSampleVersion   uint64
	backgroundFlags           regexplayground.Flags
	backgroundSelected        int
	backgroundDark            bool
	backgroundsCached         bool
}

type regexActionCache struct {
	doc     *doc
	editor  *editor.State
	path    string
	version uint64
	line    int
	column  int
	literal regexplayground.Literal
	found   bool
}

const maxCapturePreviewRunes = 72

func (g *group) regexAtCaret(d *doc, st *editor.State) (regexplayground.Literal, bool) {
	pos := st.CursorPos()
	version := st.Buffer.Version()
	cache := &g.regexAction
	if cache.doc != d || cache.editor != st || cache.path != d.path || cache.version != version || cache.line != pos.Line || cache.column != pos.Col {
		cache.doc, cache.editor, cache.path, cache.version = d, st, d.path, version
		cache.line, cache.column = pos.Line, pos.Col
		cache.literal, cache.found = regexplayground.Detect(st.Buffer.String(), d.path, st.Buffer.Offset(pos))
	}
	return cache.literal, cache.found
}

func (a *App) openRegexPlayground() {
	g := a.focus
	if g == nil {
		return
	}
	d := a.docs[tabPath(g.active)]
	st := g.eds[g.active]
	if d != nil && st != nil {
		a.openRegexPlaygroundFor(g, d, st)
	}
}

func (a *App) openRegexPlaygroundFor(g *group, d *doc, st *editor.State) {
	if g == nil || d == nil || st == nil || st.Buffer == nil {
		return
	}
	source := st.Buffer.String()
	offset := st.Buffer.Offset(st.CursorPos())
	literal, found := regexplayground.Detect(source, d.path, offset)
	if !found {
		selection := st.Selection()
		if !selection.Empty() {
			start, end := selection.Start(), selection.End()
			if start >= 0 && end <= len(source) {
				literal.Pattern = source[start:end]
			}
		}
	}
	line := st.CursorPos().Line
	sample := editor.NewBuffer("")
	sample.SetLanguage(editor.LanguageFor("sample.txt"))
	playground := &regexPlayground{
		group:            g,
		doc:              d,
		sourceEditor:     st,
		line:             line,
		pattern:          literal.Pattern,
		unsupportedFlags: literal.UnsupportedFlags,
		javascript:       regexplayground.IsJavaScriptPath(d.path),
		flags: regexplayground.Flags{
			IgnoreCase: strings.ContainsRune(literal.Flags, 'i'),
			Multiline:  strings.ContainsRune(literal.Flags, 'm'),
			DotAll:     strings.ContainsRune(literal.Flags, 's'),
		},
		sample: &editor.State{
			Buffer:       sample,
			Font:         st.Font,
			LineHeight:   1.4,
			SoftWrap:     true,
			NoFolding:    true,
			NoActiveLine: true,
			Label:        locales.RegexSample(),
			Placeholder:  locales.RegexSamplePlaceholder(),
			Theme:        a.edTheme,
		},
	}
	a.focus = g
	a.regexPlayground = playground
	a.focusEditor = playground.sample
	a.regexToolWindow.fitToSourceOnOpen = true
	a.regexToolWindow.minimumSet = false
	a.regexToolWindow.minWidth, a.regexToolWindow.minHeight = 0, 0
}

func (a *App) closeRegexPlayground(p *regexPlayground) {
	if a.regexPlayground != p {
		return
	}
	a.regexPlayground = nil
	if a.regexPlaygroundSourceLive(p) {
		a.focus = p.group
		if p.group.active != p.doc.path {
			a.activateIn(p.group, p.doc.path)
		}
		a.focusEditor = p.sourceEditor
		return
	}
	a.focusEditor = a.currentValidEditor()
}

func (a *App) regexPlaygroundSourceLive(p *regexPlayground) bool {
	return p != nil && p.group != nil && p.doc != nil && p.sourceEditor != nil && a.layout != nil &&
		a.layout.find(p.group) != nil && p.group.has(p.doc.path) && a.docs[p.doc.path] == p.doc &&
		p.group.eds[p.doc.path] == p.sourceEditor
}

func (a *App) closeRegexPlaygroundForSource(g *group, path string) {
	p := a.regexPlayground
	if p == nil || p.group != g || p.doc == nil || path != "" && p.doc.path != path {
		return
	}
	if !a.regexPlaygroundSourceLive(p) {
		a.closeRegexPlayground(p)
	}
}

func (a *App) currentValidEditor() *editor.State {
	if a.layout != nil && a.focus != nil && a.layout.find(a.focus) != nil {
		if st := a.activeEditor(); st != nil {
			return st
		}
	}
	for _, g := range a.groups() {
		if st := g.eds[g.active]; st != nil {
			a.focus = g
			return st
		}
	}
	return nil
}

func (p *regexPlayground) refresh(c *ui.Context) {
	sampleVersion := p.sample.Buffer.Version()
	if !p.cached || p.patternVersion != p.cachePatternVersion || sampleVersion != p.cacheSampleVersion || p.flags != p.cacheFlags {
		p.cachePatternVersion, p.cacheSampleVersion, p.cacheFlags, p.cached = p.patternVersion, sampleVersion, p.flags, true
		p.cacheSample = p.sample.Buffer.String()
		switch {
		case len(p.unsupportedFlags) > 0:
			p.result = regexplayground.Result{Error: "unsupported JavaScript flags"}
		case p.javascript && len(regexplayground.UnsupportedJSFeatures(p.pattern)) > 0:
			p.activeUnsupportedFeatures = regexplayground.UnsupportedJSFeatures(p.pattern)
			p.result = regexplayground.Result{Error: "unsupported JavaScript features"}
		default:
			p.activeUnsupportedFeatures = nil
			p.result = regexplayground.Evaluate(p.pattern, p.cacheSample, p.flags)
		}
		if p.selected >= len(p.result.Matches) {
			p.selected = 0
		}
	}
	p.refreshDiagram()
	p.refreshSampleHighlights(c)
}

func (p *regexPlayground) refreshSampleHighlights(c *ui.Context) {
	sampleVersion := p.sample.Buffer.Version()
	dark := c.Theme().Dark
	if p.backgroundsCached && p.backgroundPattern == p.pattern && p.backgroundPatternVersion == p.patternVersion && p.backgroundSampleVersion == sampleVersion && p.backgroundFlags == p.flags && p.backgroundSelected == p.selected && p.backgroundDark == dark {
		return
	}
	p.backgroundPattern, p.backgroundPatternVersion = p.pattern, p.patternVersion
	p.backgroundSampleVersion, p.backgroundFlags = sampleVersion, p.flags
	p.backgroundSelected, p.backgroundDark, p.backgroundsCached = p.selected, dark, true
	p.sample.TextBackgrounds = nil
	if p.pattern == "" || p.result.Error != "" || p.result.Limited != "" {
		return
	}
	for i, match := range p.result.Matches {
		if match.Start != match.End {
			color := colors(c).Muted.Alpha(0.08)
			if i == p.selected {
				color = colors(c).Match.Alpha(0.20)
			}
			p.sample.TextBackgrounds = append(p.sample.TextBackgrounds, editor.TextBackground{Start: match.Start, End: match.End, Color: color})
		}
		if i == p.selected {
			for _, span := range regexplayground.CaptureHighlights(match.Captures) {
				p.sample.TextBackgrounds = append(p.sample.TextBackgrounds, editor.TextBackground{
					Start: span.Start,
					End:   span.End,
					Color: regexCapturePalette(c, span.Number).fill,
				})
			}
		}
	}
}

func (p *regexPlayground) refreshDiagram() {
	if p.pattern == "" || len(p.pattern) > regexplayground.MaxPatternBytes || len(p.unsupportedFlags) > 0 || len(p.activeUnsupportedFeatures) > 0 {
		p.diagramValid = false
		return
	}
	if p.diagramCached && p.diagramPattern == p.pattern && p.diagramFlags == p.flags {
		p.diagramValid = !p.diagramParseFailed
		return
	}
	p.diagramPattern, p.diagramFlags, p.diagramCached = p.pattern, p.flags, true
	diagram, err := regexplayground.ParseDiagram(p.pattern, p.flags)
	if err != nil {
		p.diagram = regexplayground.Diagram{}
		p.diagramValid = false
		p.diagramParseFailed = true
		return
	}
	p.diagram, p.diagramValid = diagram, true
	p.diagramParseFailed = false
}

func (a *App) regexPlaygroundResults(c *ui.Context, p *regexPlayground) {
	ui.Column(c).Grow(1).FillWidth().Gap(4).Children(func() {
		ui.Row(c).FillWidth().Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.RegexMatches()).FontSize(12).FontWeight(600)
			if len(p.result.Matches) > 0 && p.pattern != "" {
				ui.Text(c, fmt.Sprintf(locales.RegexMatchCount(), len(p.result.Matches))).FontSize(10).TextColor(colors(c).Muted)
			}
			ui.Box(c).Grow(1)
		})
		switch {
		case len(p.unsupportedFlags) > 0:
			ui.Text(c, fmt.Sprintf(locales.RegexUnsupportedFlags(), strings.Join(p.unsupportedFlags, ", "))).FontSize(12).TextColor(colors(c).Err)
		case len(p.activeUnsupportedFeatures) > 0:
			ui.Text(c, fmt.Sprintf(locales.RegexUnsupportedFeatures(), strings.Join(p.activeUnsupportedFeatures, ", "))).FontSize(12).TextColor(colors(c).Err)
		case p.pattern == "":
			ui.Text(c, locales.RegexEnterPattern()).FontSize(12).TextColor(colors(c).Muted).Padding(5, 0)
		case len(p.pattern) > regexplayground.MaxPatternBytes:
			ui.Text(c, fmt.Sprintf(locales.RegexPatternTooLarge(), regexplayground.MaxPatternBytes)).FontSize(12).TextColor(colors(c).Err)
		case len(p.cacheSample) > regexplayground.MaxSampleBytes:
			ui.Text(c, fmt.Sprintf(locales.RegexSampleTooLarge(), regexplayground.MaxSampleBytes)).FontSize(12).TextColor(colors(c).Err)
		case p.result.Error != "":
			ui.Column(c).Gap(4).Padding(5, 0).Children(func() {
				ui.Text(c, locales.RegexInvalidPattern()).FontSize(12).TextColor(colors(c).Err)
				ui.Text(c, p.result.Error).FontSize(11).TextColor(colors(c).Muted)
			})
		case p.cacheSample == "" && len(p.result.Matches) == 0:
			ui.Text(c, locales.RegexEmptySample()).FontSize(12).TextColor(colors(c).Muted).Padding(5, 0)
		case len(p.result.Matches) == 0:
			ui.Column(c).Gap(4).Padding(5, 0).Children(func() {
				ui.Text(c, locales.RegexNoMatches()).FontSize(12)
				ui.Text(c, locales.RegexNoMatchesDetail()).FontSize(11).TextColor(colors(c).Muted)
			})
		default:
			if p.result.Truncated {
				ui.Text(c, fmt.Sprintf(locales.RegexMatchesLimited(), regexplayground.MaxMatches)).FontSize(10).TextColor(colors(c).Muted)
			}
			a.regexMatchDetails(c, p)
		}
		ui.Column(c).Shrink(0).Gap(1).Padding(4, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(colors(c).Separator).Children(func() {
			ui.Text(c, locales.RegexRangeNote()).FontSize(8).TextColor(colors(c).Muted)
			ui.Text(c, locales.RegexEngineNote()).FontSize(8).TextColor(colors(c).Muted)
		})
	})
}

func (a *App) regexMatchDetails(c *ui.Context, p *regexPlayground) {
	ui.Scroll(c).MaxHeight(92).FillWidth().Shrink(0).Children(func() {
		ui.Column(c).FillWidth().Gap(2).Children(func() {
			for i, match := range p.result.Matches {
				index := i
				label := locales.Catalog.Format(i18n.Locale(), "msgMatchNInSample", map[string]any{
					"n":     i + 1,
					"text":  regexMatchPreview(match),
					"range": regexCharacterRange(p.cacheSample, match.Start, match.End),
				})
				button := ui.ButtonBase(c.Key(struct {
					Playground *regexPlayground
					Match      int
				}{p, i}),
				).FillWidth().MinHeight(31).Shrink(0).Padding(3, 5).Radius(6).Label(label).AccessSelected(i == p.selected)
				if index == p.selected {
					button.Background(colors(c).SelectedRow).Border(1, c.Theme().Accent.Alpha(0.28))
				} else if button.Hovered() {
					button.Background(colors(c).Pill)
				}
				button.Children(func() {
					ui.Row(c).FillWidth().Gap(7).AlignItems(ui.Center).Children(func() {
						bar := colors(c).Separator
						if index == p.selected {
							bar = c.Theme().Accent
						}
						ui.Box(c).Size(3, 18).Shrink(0).Radius(2).Background(bar)
						ui.Text(c, fmt.Sprintf("%d", index+1)).FontSize(9).TextColor(colors(c).Muted).Width(14).Shrink(0)
						ui.Text(c, regexMatchPreview(match)).FontSize(11).Font("monospace").Shrink(1).SingleLine().Ellipsis("…")
						ui.Box(c).Grow(1)
						ui.Text(c, regexCharacterRange(p.cacheSample, match.Start, match.End)).FontSize(9).TextColor(colors(c).Muted).SingleLine()
					})
				})
				button.OnClick(func() { p.selected = index })
			}
		})
	})
	if p.selected < 0 || p.selected >= len(p.result.Matches) {
		return
	}
	match := p.result.Matches[p.selected]
	ui.Column(c).Grow(1).MinHeight(0).FillWidth().BorderWidth(1, 0, 0, 0).BorderColor(colors(c).Separator).Padding(5, 0, 0).Gap(3).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, regexMatchPreview(match)).FontSize(13).FontWeight(600).Font("monospace").Shrink(1).SingleLine().Ellipsis("…")
			ui.Text(c, fmt.Sprintf("%s %d", locales.RegexMatch(), p.selected+1)).FontSize(9).TextColor(colors(c).Muted).SingleLine()
			ui.Box(c).Grow(1)
			wholeMatch := regexWholeMatch(match)
			if utf8.RuneCountInString(wholeMatch) > maxCapturePreviewRunes {
				services := c.Services()
				copy := ui.Button(c.Key(struct {
					Playground *regexPlayground
					Match      int
					Whole      bool
				}{p, p.selected, true}), locales.Copy()).FontSize(9).Padding(1, 5).Shrink(0)
				copy.OnClick(func() { services.WriteClipboard(wholeMatch) })
			}
			ui.Text(c, regexCharacterRange(p.cacheSample, match.Start, match.End)).FontSize(9).TextColor(colors(c).Muted).SingleLine()
		})
		ui.Text(c, locales.RegexCaptures()).FontSize(9).TextColor(colors(c).Muted)
		ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Gap(3).Children(func() {
				foundCapture := false
				for _, capture := range match.Captures {
					if capture.Number == 0 {
						continue
					}
					foundCapture = true
					capture := capture
					palette := regexCapturePalette(c, capture.Number)
					value := capturePreview(capture.Value)
					valueColor := colors(c).Text
					if !capture.Matched {
						value, valueColor = locales.RegexNoCapture(), colors(c).Muted
					} else if capture.Start == capture.End {
						value = locales.RegexEmptyCapture()
					}
					ui.Row(c).FillWidth().MinHeight(32).Shrink(0).Gap(7).AlignItems(ui.Center).Padding(3, 6).Radius(6).Background(palette.fill.Alpha(0.09)).Border(1, palette.stroke.Alpha(0.28)).Children(func() {
						ui.Box(c).Width(3).Height(26).Shrink(0).Radius(2).Background(palette.stroke)
						ui.Column(c).Grow(1).MinWidth(0).Shrink(0).Gap(2).Children(func() {
							ui.Text(c, value).FontSize(11).Font("monospace").TextColor(valueColor).Shrink(1).SingleLine().Ellipsis("…")
							name := fmt.Sprintf("%d", capture.Number)
							if capture.Name != "" {
								name += " · " + capture.Name
							}
							if capture.Matched {
								name += " · " + regexCharacterRange(p.cacheSample, capture.Start, capture.End)
							}
							ui.Text(c, name).FontSize(10).TextColor(colors(c).Muted).SingleLine().Ellipsis("…")
						})
						if capture.Matched && utf8.RuneCountInString(capture.Value) > maxCapturePreviewRunes {
							services := c.Services()
							copy := ui.Button(c.Key(struct {
								Playground *regexPlayground
								Capture    int
							}{p, capture.Number}), locales.Copy()).FontSize(9).Padding(1, 5).Shrink(0).Label(fmt.Sprintf(locales.RegexCopyCapture(), capture.Number))
							copy.OnClick(func() { services.WriteClipboard(capture.Value) })
						}
					})
				}
				if !foundCapture {
					ui.Text(c, locales.RegexNoCapture()).FontSize(11).TextColor(colors(c).Muted).Padding(4, 0)
				}
			})
		})
	})
}

func regexMatchPreview(match regexplayground.Match) string {
	value := regexWholeMatch(match)
	if strings.ContainsAny(value, "\r\n\t") {
		value = fmt.Sprintf("%q", value)
	}
	return regexRailTrim(value, 40)
}

func regexWholeMatch(match regexplayground.Match) string {
	for _, capture := range match.Captures {
		if capture.Number == 0 {
			return capture.Value
		}
	}
	return ""
}

func regexCharacterRange(sample string, start, end int) string {
	start = min(max(start, 0), len(sample))
	end = min(max(end, start), len(sample))
	return fmt.Sprintf(locales.RegexCharacterRange(), utf8.RuneCountInString(sample[:start]), utf8.RuneCountInString(sample[:end]))
}

func capturePreview(value string) string {
	runes := []rune(value)
	if len(runes) > maxCapturePreviewRunes {
		return fmt.Sprintf("%q…", string(runes[:maxCapturePreviewRunes]))
	}
	return fmt.Sprintf("%q", value)
}
