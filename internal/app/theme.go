package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// palette is the app's colors in one appearance, after Xcode's. The
// separators and secondary text are AppKit's separatorColor and
// secondaryLabelColor: translucent, so that they read the same over the
// sidebar, the toolbar and the editor. The toolbar is the editor's color,
// as macOS 27's uniform toolbar over the content.
type palette struct {
	editor, sidebar, toolbar, tabBar, tabActive, tabHover ui.Color
	separator, hairline                                   ui.Color
	text, muted, dim, err                                 ui.Color
	warnBg, infoBg, pill                                  ui.Color
	page, folder, link, match                             ui.Color
	popover, popoverEdge, dropZone                        ui.Color
	welcomeLeft, welcomeRight, welcomeTile, welcomeHover  ui.Color
	settings, card, selectedRow                           ui.Color
}

var darkPalette = palette{
	editor:       ui.RGB(0x1f, 0x1f, 0x24),
	sidebar:      ui.RGB(0x29, 0x29, 0x2d),
	toolbar:      ui.RGB(0x1f, 0x1f, 0x24),
	tabBar:       ui.RGB(0x25, 0x25, 0x29),
	tabActive:    ui.RGB(0x1f, 0x1f, 0x24),
	tabHover:     ui.RGBA(0xff, 0xff, 0xff, 0.04),
	separator:    ui.RGBA(0xff, 0xff, 0xff, 0.10),
	hairline:     ui.RGBA(0xff, 0xff, 0xff, 0.08),
	text:         ui.RGB(0xdf, 0xdf, 0xe0),
	muted:        ui.RGBA(0xff, 0xff, 0xff, 0.55),
	dim:          ui.RGB(0x6c, 0x6c, 0x70),
	err:          ui.RGB(0xff, 0x6b, 0x60),
	warnBg:       ui.RGB(0x3a, 0x30, 0x12),
	infoBg:       ui.RGB(0x13, 0x2f, 0x4f),
	pill:         ui.RGBA(0xff, 0xff, 0xff, 0.07),
	page:         ui.RGB(0xdc, 0xdc, 0xe0),
	folder:       ui.RGB(0x5a, 0xb0, 0xf7),
	link:         ui.RGBA(0x0a, 0x84, 0xff, 0.28),
	match:        ui.RGBA(0xff, 0xd6, 0x0a, 0.30),
	popover:      ui.RGB(0x2c, 0x2c, 0x30),
	popoverEdge:  ui.RGBA(0xff, 0xff, 0xff, 0.10),
	dropZone:     ui.RGBA(0x0a, 0x84, 0xff, 0.22),
	welcomeLeft:  ui.RGB(0x2a, 0x2a, 0x2c),
	welcomeRight: ui.RGB(0x1e, 0x1e, 0x20),
	welcomeTile:  ui.RGB(0x3a, 0x3a, 0x3e),
	welcomeHover: ui.RGBA(0xff, 0xff, 0xff, 0.07),
	settings:     ui.RGB(0x1e, 0x1e, 0x20),
	card:         ui.RGBA(0xff, 0xff, 0xff, 0.05),
	selectedRow:  ui.RGBA(0xff, 0xff, 0xff, 0.10),
}

var lightPalette = palette{
	editor:       ui.RGB(0xff, 0xff, 0xff),
	sidebar:      ui.RGB(0xf2, 0xf2, 0xf4),
	toolbar:      ui.RGB(0xff, 0xff, 0xff),
	tabBar:       ui.RGB(0xeb, 0xeb, 0xed),
	tabActive:    ui.RGB(0xff, 0xff, 0xff),
	tabHover:     ui.RGBA(0, 0, 0, 0.04),
	separator:    ui.RGBA(0, 0, 0, 0.10),
	hairline:     ui.RGBA(0, 0, 0, 0.08),
	text:         ui.RGB(0x1d, 0x1d, 0x1f),
	muted:        ui.RGBA(0, 0, 0, 0.50),
	dim:          ui.RGB(0xa1, 0xa1, 0xa6),
	err:          ui.RGB(0xd7, 0x00, 0x15),
	warnBg:       ui.RGB(0xff, 0xf4, 0xd4),
	infoBg:       ui.RGB(0xe3, 0xee, 0xfc),
	pill:         ui.RGBA(0, 0, 0, 0.05),
	page:         ui.RGB(0xd2, 0xd2, 0xd8),
	folder:       ui.RGB(0x2f, 0x8c, 0xe8),
	link:         ui.RGBA(0x00, 0x7a, 0xff, 0.16),
	match:        ui.RGBA(0xff, 0xcc, 0x00, 0.45),
	popover:      ui.RGB(0xff, 0xff, 0xff),
	popoverEdge:  ui.RGBA(0, 0, 0, 0.10),
	dropZone:     ui.RGBA(0x00, 0x7a, 0xff, 0.14),
	welcomeLeft:  ui.RGB(0xf5, 0xf5, 0xf7),
	welcomeRight: ui.RGB(0xff, 0xff, 0xff),
	welcomeTile:  ui.RGB(0xe5, 0xe5, 0xea),
	welcomeHover: ui.RGBA(0, 0, 0, 0.05),
	settings:     ui.RGB(0xf7, 0xf7, 0xf7),
	card:         ui.RGBA(0, 0, 0, 0.04),
	selectedRow:  ui.RGBA(0, 0, 0, 0.07),
}

// colors returns the palette of the appearance the window of c shows.
// The accent is the theme's: c.Theme().Accent.
func colors(c *ui.Context) *palette {
	if c.Theme().Dark {
		return &darkPalette
	}
	return &lightPalette
}

// colors returns the palette of the appearance the app last drew in, for
// what is not drawn in a frame of its own.
func (a *App) colors() *palette {
	if a.theme != nil && !a.theme.Dark {
		return &lightPalette
	}
	return &darkPalette
}

// applyAppearance takes the window's appearance, light or dark, which
// follows the system unless the user chose one, and its accent color:
// the app's theme and colors follow, and the editors' themes with them.
// It does the work only when the appearance changes.
func (a *App) applyAppearance(c *ui.Context) {
	base := c.Theme() // the default theme: the system's appearance and accent
	if a.theme != nil && a.themeDark == base.Dark && a.themeAccent == base.Accent {
		return
	}
	a.themeDark, a.themeAccent = base.Dark, base.Accent
	p := &lightPalette
	if base.Dark {
		p = &darkPalette
	}
	t := *base // keeps the desktop's accent, contrast and text size
	t.Background = p.editor
	t.Text = p.text
	t.TextMuted = p.muted
	t.Border = p.separator
	t.Spacing = 3
	t.FontSize = 13 * max(1, c.Preferences().TextScale)
	t.Radius = 6
	if base.Dark {
		t.Selection = ui.RGB(0x3f, 0x63, 0x8b)
		t.Surface = ui.RGB(0x3a, 0x3a, 0x3e)
		t.SurfaceHover = ui.RGB(0x44, 0x44, 0x48)
		t.SurfacePressed = ui.RGB(0x4e, 0x4e, 0x52)
	} else {
		t.Selection = ui.RGB(0xb3, 0xd7, 0xff)
		t.Surface = ui.RGB(0xff, 0xff, 0xff)
		t.SurfaceHover = ui.RGB(0xf3, 0xf3, 0xf5)
		t.SurfacePressed = ui.RGB(0xe6, 0xe6, 0xea)
	}
	a.theme = &t
	a.edTheme = editor.DefaultTheme(&t)
	a.edTheme.Background = p.editor
	for _, g := range a.groups() {
		for _, v := range g.diffs {
			v.state.Theme = a.edTheme
		}
	}
	if a.git.peek != nil {
		a.git.peek.diff.Theme = a.edTheme
	}
	for _, g := range a.groups() {
		for _, st := range g.eds {
			st.Theme = a.edTheme
		}
	}
}

// chooseAppearance forces light or dark, or "" follows the system, and
// remembers it.
func (a *App) chooseAppearance(s string) {
	a.cfg.Appearance = s
	a.saveConfig()
	a.host.setAppearance(s)
	a.host.refreshMenu() // its radio items show the choice
}
