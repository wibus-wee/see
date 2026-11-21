package app

import (
	"fmt"
	"strings"
	"unicode/utf8"

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
}

func (a *App) closeRegexPlayground(p *regexPlayground) {
	if a.regexPlayground != p {
		return
	}
	a.regexPlayground = nil
	a.focus = p.group
	a.focusEditor = p.sourceEditor
}

func (a *App) regexPlaygroundView(c *ui.Context, bounds ui.Rect, st *editor.State, p *regexPlayground) {
	if c.Shortcut(0, ui.KeyEscape) {
		a.closeRegexPlayground(p)
		return
	}
	p.sample.Theme = a.edTheme
	a.editorPeekSized(c, bounds, st, p.line, locales.RegexPlaygroundTitle(), locales.Close(), 500, 520, nil,
		func() { a.closeRegexPlayground(p) }, func() {
			ui.Column(c).Grow(1).FillWidth().Children(func() {
				ui.Row(c).FillWidth().Shrink(0).Padding(10, 12, 4).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, locales.RegexPattern()+":").FontSize(11).TextColor(colors(c).Muted).Width(72).Shrink(0)
					patternInput := ui.TextInput(c, &p.pattern).Label(locales.RegexPattern()).Font("monospace").FontSize(13).Grow(1).MinWidth(80)
					if patternInput.Shortcut(0, ui.KeyEscape) {
						a.closeRegexPlayground(p)
					}
					if patternInput.Changed() {
						p.patternVersion++
						p.unsupportedFlags = nil
					}
					ui.Text(c, locales.RegexEngineBadge()).FontSize(11).TextColor(c.Theme().Accent).SingleLine()
				})
				ui.Row(c).FillWidth().Shrink(0).Padding(2, 12, 8).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Checkbox(c, &p.flags.IgnoreCase, locales.RegexIgnoreCase())
					ui.Checkbox(c, &p.flags.Multiline, locales.RegexMultiline())
					ui.Checkbox(c, &p.flags.DotAll, locales.RegexDotAll())
				})
				ui.Row(c).Grow(1).FillWidth().Gap(0).BorderWidth(1, 0, 0, 0).BorderColor(colors(c).Separator).Children(func() {
					ui.Column(c).Grow(11).Fill().Padding(10, 12, 12).Children(func() {
						ui.Text(c, locales.RegexSample()+":").FontSize(11).TextColor(colors(c).Muted).Padding(0, 0, 6)
						view := editor.View(c.Key("regex-playground-sample"), p.sample).Grow(1).FillWidth()
						if view.Shortcut(0, ui.KeyEscape) {
							a.closeRegexPlayground(p)
						}
						if a.focusEditor == p.sample {
							view.Focus()
							a.focusEditor = nil
						}
					})
					p.refresh(colors(c).Match)
					ui.Column(c).Grow(9).Fill().Padding(10, 12, 12).BorderWidth(0, 0, 0, 1).BorderColor(colors(c).Separator).Children(func() {
						a.regexPlaygroundResults(c, p)
					})
				})
			})
		})
}

func (p *regexPlayground) refresh(matchColor ui.Color) {
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
	p.sample.TextBackgrounds = nil
	if p.pattern == "" || p.result.Error != "" || p.result.Limited != "" {
		return
	}
	for _, match := range p.result.Matches {
		if match.Start != match.End {
			p.sample.TextBackgrounds = append(p.sample.TextBackgrounds, editor.TextBackground{Start: match.Start, End: match.End, Color: matchColor})
		}
	}
}

func (a *App) regexPlaygroundResults(c *ui.Context, p *regexPlayground) {
	ui.Row(c).FillWidth().Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.RegexMatches()).FontSize(12).FontWeight(600)
		if len(p.result.Matches) > 0 && p.pattern != "" {
			ui.Text(c, fmt.Sprintf(locales.RegexMatchCount(), len(p.result.Matches))).FontSize(11).TextColor(colors(c).Muted)
		}
		ui.Box(c).Grow(1)
	})
	ui.Text(c, locales.RegexRangeNote()).FontSize(10).TextColor(colors(c).Muted).Padding(0, 0, 8)
	ui.Text(c, locales.RegexEngineNote()).FontSize(9).TextColor(colors(c).Muted).Padding(0, 0, 8)
	switch {
	case len(p.unsupportedFlags) > 0:
		ui.Text(c, fmt.Sprintf(locales.RegexUnsupportedFlags(), strings.Join(p.unsupportedFlags, ", "))).FontSize(12).TextColor(colors(c).Err)
	case len(p.activeUnsupportedFeatures) > 0:
		ui.Text(c, fmt.Sprintf(locales.RegexUnsupportedFeatures(), strings.Join(p.activeUnsupportedFeatures, ", "))).FontSize(12).TextColor(colors(c).Err)
	case p.pattern == "":
		ui.Text(c, locales.RegexEnterPattern()).FontSize(12).TextColor(colors(c).Muted).Padding(8, 0)
	case len(p.pattern) > regexplayground.MaxPatternBytes:
		ui.Text(c, fmt.Sprintf(locales.RegexPatternTooLarge(), regexplayground.MaxPatternBytes)).FontSize(12).TextColor(colors(c).Err)
	case len(p.cacheSample) > regexplayground.MaxSampleBytes:
		ui.Text(c, fmt.Sprintf(locales.RegexSampleTooLarge(), regexplayground.MaxSampleBytes)).FontSize(12).TextColor(colors(c).Err)
	case p.result.Error != "":
		ui.Column(c).Gap(4).Padding(8, 0).Children(func() {
			ui.Text(c, locales.RegexInvalidPattern()).FontSize(12).TextColor(colors(c).Err)
			ui.Text(c, p.result.Error).FontSize(11).TextColor(colors(c).Muted)
		})
	case p.cacheSample == "" && len(p.result.Matches) == 0:
		ui.Text(c, locales.RegexEmptySample()).FontSize(12).TextColor(colors(c).Muted).Padding(8, 0)
	case len(p.result.Matches) == 0:
		ui.Column(c).Gap(4).Padding(8, 0).Children(func() {
			ui.Text(c, locales.RegexNoMatches()).FontSize(12)
			ui.Text(c, locales.RegexNoMatchesDetail()).FontSize(11).TextColor(colors(c).Muted)
		})
	default:
		if p.result.Truncated {
			ui.Text(c, fmt.Sprintf(locales.RegexMatchesLimited(), regexplayground.MaxMatches)).FontSize(10).TextColor(colors(c).Muted).Padding(0, 0, 6)
		}
		a.regexMatchDetails(c, p)
	}
}

func (a *App) regexMatchDetails(c *ui.Context, p *regexPlayground) {
	ui.Scroll(c).Height(132).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Gap(3).Children(func() {
			for i, match := range p.result.Matches {
				index := i
				label := fmt.Sprintf("%d   %s", i+1, regexCharacterRange(p.cacheSample, match.Start, match.End))
				button := ui.Button(c.Key(struct {
					Playground *regexPlayground
					Match      int
				}{p, i}), label).FillWidth().Label(fmt.Sprintf("Match %d, %s", i+1, regexCharacterRange(p.cacheSample, match.Start, match.End)))
				if index == p.selected {
					button.Background(colors(c).SelectedRow).Border(1, c.Theme().Accent).TextColor(c.Theme().Accent)
				}
				button.OnClick(func() { p.selected = index })
			}
		})
	})
	if p.selected >= len(p.result.Matches) {
		return
	}
	match := p.result.Matches[p.selected]
	ui.Column(c).Grow(1).FillWidth().BorderWidth(1, 0, 0, 0).BorderColor(colors(c).Separator).Padding(8, 0, 0).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf("%s %d", locales.RegexMatch(), p.selected+1)).FontSize(11).FontWeight(600)
			ui.Text(c, regexCharacterRange(p.cacheSample, match.Start, match.End)).FontSize(10).TextColor(colors(c).Muted)
		})
		ui.Text(c, locales.RegexCaptures()).FontSize(10).TextColor(colors(c).Muted).Padding(0, 0, 4)
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Gap(4).Children(func() {
				for _, capture := range match.Captures {
					capture := capture
					ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
						name := fmt.Sprintf("%d", capture.Number)
						if capture.Name != "" {
							name += " · " + capture.Name
						}
						ui.Text(c, name).FontSize(10).TextColor(colors(c).Muted).Width(96).Shrink(0).SingleLine().Ellipsis("…")
						if !capture.Matched {
							ui.Text(c, locales.RegexUnmatched()).FontSize(11).TextColor(colors(c).Muted)
						} else {
							value := capturePreview(capture.Value)
							if capture.Start == capture.End {
								value = locales.RegexEmptyCapture()
							}
							ui.Text(c, value).FontSize(11).Font("monospace").Shrink(1).SingleLine().Ellipsis("…")
							ui.Text(c, regexCharacterRange(p.cacheSample, capture.Start, capture.End)).FontSize(10).TextColor(colors(c).Muted).SingleLine()
							if utf8.RuneCountInString(capture.Value) > maxCapturePreviewRunes {
								services := c.Services()
								copy := ui.Button(c.Key(struct {
									Playground *regexPlayground
									Capture    int
								}{p, capture.Number}), locales.Copy()).FontSize(10).Padding(1, 5).Shrink(0).Label(fmt.Sprintf(locales.RegexCopyCapture(), capture.Number))
								copy.OnClick(func() { services.WriteClipboard(capture.Value) })
							}
						}
					})
				}
			})
		})
	})
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
