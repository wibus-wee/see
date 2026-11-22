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
					colorPreviewPanel(c, preview)
				})
				if popover.Dismissed() {
					g.dismissColorPreview()
				}
			},
		})
	}
	return replacements
}

func colorPreviewPanel(c *ui.Context, preview *colorPreviewState) {
	label := fmt.Sprintf(locales.ColorPreviewButton(), preview.literal.Raw)
	ui.Box(c.Key("color-preview-panel")).Width(240).Height(92).Center().Children(func() {
		ui.Box(c.Key("color-preview-canvas")).Width(224).Height(76).
			Role(ui.RoleGroup).Label(label).Children(func() {
			ui.Row(c).Fill().Children(func() {
				ui.Box(c.Key("color-preview-light")).Grow(1).FillHeight().
					Background(ui.RGB(255, 255, 255)).Role(ui.RoleGroup).Label(locales.ColorPreviewLight())
				ui.Box(c.Key("color-preview-dark")).Grow(1).FillHeight().
					Background(ui.RGB(24, 24, 27)).Role(ui.RoleGroup).Label(locales.ColorPreviewDark())
			})
			ui.Box(c.Key("color-preview-band")).Absolute().Left(12).Right(12).Top(20).Height(36).
				Radius(5).Background(colorliteralColor(preview.literal.Color))
		})
	})
}

func colorliteralColor(color colorliteral.Color) ui.Color {
	return ui.RGBA(color.R, color.G, color.B, float32(color.A)/255)
}
