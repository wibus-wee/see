package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/colorliteral"
	"see/internal/workspace"
	"see/locales"
)

const maxColorPreviewLiterals = 4096

type colorLiteralCache struct {
	version  uint64
	parsed   bool
	literals []colorliteral.Literal
}

type colorPreviewState struct {
	path    string
	start   int
	version uint64
	literal colorliteral.Literal
	open    bool
	seen    bool
}

func (g *group) dismissColorPreview() {
	if g.colorPreview != nil {
		g.colorPreview.open = false
		g.colorPreview = nil
	}
}

func (d *doc) colorLiterals() []colorliteral.Literal {
	if d.buf == nil || d.plain || d.lossy || d.buf.Len() > workspace.PlainLimit {
		return nil
	}
	version := d.buf.Version()
	if d.colorCache.parsed && d.colorCache.version == version {
		return d.colorCache.literals
	}
	d.colorCache = colorLiteralCache{
		version:  version,
		parsed:   true,
		literals: colorliteral.ParseLimit(d.buf.String(), maxColorPreviewLiterals),
	}
	return d.colorCache.literals
}

func (a *App) colorReplacements(g *group, d *doc, st *editor.State) []editor.Replacement {
	literals := d.colorLiterals()
	if len(literals) == 0 {
		return nil
	}
	fontSize := st.Font.Size
	if fontSize <= 0 {
		fontSize = 13
	}
	const dotSize, gap = float32(10), float32(4)
	replacements := make([]editor.Replacement, 0, len(literals))
	for _, literal := range literals {
		literal := literal
		glyph := literal.Raw[:1]
		glyphWidth := fontSize * 0.6
		replacements = append(replacements, editor.Replacement{
			Start:       literal.Start,
			End:         literal.Start + 1,
			Width:       dotSize + gap + glyphWidth,
			Interactive: true,
			Content: func(c *ui.Context, e ui.Element, r editor.Replacement) {
				var dot ui.Element
				e.Children(func() {
					ui.Row(c).Fill().Gap(gap).AlignItems(ui.Center).Children(func() {
						label := fmt.Sprintf(locales.ColorPreviewButton(), literal.Raw)
						key := struct {
							group  int
							path   string
							offset int
						}{g.id, d.path, r.Start}
						dot = ui.ButtonBase(c.Key(key)).Size(dotSize, dotSize).Radius(dotSize / 2).
							Shrink(0).KeepFocus().Cursor(ui.CursorPointer).
							Background(colorliteralColor(literal.Color)).BorderWidth(1).BorderColor(c.Theme().TextMuted).
							Label(label).Tooltip(label)
						if dot.Hovered() || dot.Pressed() {
							dot.BorderWidth(2).BorderColor(c.Theme().Accent)
						}
						dot.OnClick(func() {
							a.focus = g
							g.colorPreview = &colorPreviewState{
								path:    d.path,
								start:   r.Start,
								version: d.buf.Version(),
								literal: literal,
								open:    true,
							}
						})
						ui.Text(c, glyph).Font(st.Font.Family).FontSize(fontSize).TextColor(c.Theme().Text).
							HandleInput(func(ev ui.InputEvent) bool {
								if ev.Kind != ui.InputPointerDown || ev.Button != 0 {
									return false
								}
								offset := r.Start
								if ev.X >= glyphWidth/2 {
									offset = r.End
								}
								st.SetSelections(editor.Selection{Anchor: offset, Head: offset})
								return true
							})
					})
				})
				preview := g.colorPreview
				if preview == nil || !preview.open || preview.path != d.path || preview.start != r.Start || preview.version != d.buf.Version() {
					return
				}
				preview.seen = true
				popover := ui.Popover(c, dot, &preview.open, func() {
					colorPreviewPanel(c, g, preview)
				})
				if popover.Dismissed() {
					g.dismissColorPreview()
				}
			},
		})
	}
	return replacements
}

func colorPreviewPanel(c *ui.Context, g *group, preview *colorPreviewState) {
	ui.Column(c.Key("color-preview-card")).Width(280).Padding(10).Gap(8).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, preview.literal.Raw).Font("monospace").FontSize(12).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1).Tooltip(preview.literal.Raw)
			ui.Box(c).Grow(1).MinWidth(0)
			close := ui.ButtonBase(c.Key("color-preview-close")).Size(22, 22).Radius(5).KeepFocus().
				Label(locales.ColorPreviewClose()).Tooltip(locales.ColorPreviewClose())
			close.Children(func() { symbolIcon(c, icClose, 10, colors(c).Muted) })
			if close.Clicked() {
				preview.open = false
				if g.colorPreview == preview {
					g.colorPreview = nil
				}
			}
		})
		ui.Text(c, colorPreviewHex(preview.literal.Color)).Font("monospace").FontSize(11).TextColor(colors(c).Muted).SingleLine()
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			for _, sample := range []struct {
				label      string
				background ui.Color
				text       ui.Color
				border     ui.Color
				rgb        [3]uint8
			}{
				{locales.ColorPreviewLight(), ui.RGBA(255, 255, 255, 1), ui.RGBA(32, 32, 32, 1), ui.RGBA(0, 0, 0, 0.16), [3]uint8{255, 255, 255}},
				{locales.ColorPreviewDark(), ui.RGBA(24, 24, 27, 1), ui.RGBA(245, 245, 245, 1), ui.RGBA(255, 255, 255, 0.18), [3]uint8{24, 24, 27}},
			} {
				sample := sample
				ui.Column(c.Key(sample.label)).Grow(1).Padding(8).Gap(6).Radius(8).
					Background(sample.background).BorderWidth(1).BorderColor(sample.border).Children(func() {
					ui.Text(c, sample.label).FontSize(11).TextColor(sample.text)
					ui.Box(c).FillWidth().Height(46).Radius(6).Background(colorliteralColor(preview.literal.Color)).
						BorderWidth(1).BorderColor(sample.border).Center().Children(func() {
						ui.Text(c, "Aa").Font("system-ui").FontSize(20).
							TextColor(colorPreviewSampleText(preview.literal.Color, sample.rgb))
					})
				})
			}
		})
	})
}

func colorliteralColor(color colorliteral.Color) ui.Color {
	return ui.RGBA(color.R, color.G, color.B, float32(color.A)/255)
}

func colorPreviewHex(color colorliteral.Color) string {
	if color.A == 255 {
		return fmt.Sprintf("#%02X%02X%02X", color.R, color.G, color.B)
	}
	return fmt.Sprintf("#%02X%02X%02X%02X", color.R, color.G, color.B, color.A)
}

func colorPreviewSampleText(color colorliteral.Color, background [3]uint8) ui.Color {
	alpha := float64(color.A) / 255
	channel := func(foreground, behind uint8) float64 {
		return float64(foreground)*alpha + float64(behind)*(1-alpha)
	}
	luminance := 0.2126*channel(color.R, background[0]) + 0.7152*channel(color.G, background[1]) + 0.0722*channel(color.B, background[2])
	if luminance > 140 {
		return ui.RGBA(30, 30, 30, 1)
	}
	return ui.RGBA(250, 250, 250, 1)
}
