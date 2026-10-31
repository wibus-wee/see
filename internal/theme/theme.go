// Package theme is the app's colors, after Xcode's: a palette for each
// appearance, the palette of the appearance a frame draws in, and the ui
// and editor themes built from them.
//
// The palettes are the only colors the app hard-codes; the accent, the
// contrast and the text size stay the desktop's.
package theme

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// Palette is the app's colors in one appearance. The separators and
// secondary text are AppKit's separatorColor and secondaryLabelColor:
// translucent, so that they read the same over the sidebar, the toolbar
// and the editor. The toolbar is the editor's color, as macOS 27's
// uniform toolbar over the content.
type Palette struct {
	Editor, Sidebar, Toolbar, TabBar, TabActive, TabHover ui.Color
	Separator, Hairline                                   ui.Color
	Text, Muted, Dim, Err                                 ui.Color
	WarnBg, InfoBg, Pill                                  ui.Color
	Page, Folder, Link, Match                             ui.Color
	Popover, PopoverEdge, DropZone                        ui.Color
	WelcomeLeft, WelcomeRight, WelcomeTile, WelcomeHover  ui.Color
	Settings, Card, SelectedRow                           ui.Color
}

// Dark and Light are the two appearances the app draws.
var (
	Dark = Palette{
		Editor:       ui.RGB(0x1f, 0x1f, 0x24),
		Sidebar:      ui.RGB(0x29, 0x29, 0x2d),
		Toolbar:      ui.RGB(0x1f, 0x1f, 0x24),
		TabBar:       ui.RGB(0x25, 0x25, 0x29),
		TabActive:    ui.RGB(0x1f, 0x1f, 0x24),
		TabHover:     ui.RGBA(0xff, 0xff, 0xff, 0.04),
		Separator:    ui.RGBA(0xff, 0xff, 0xff, 0.10),
		Hairline:     ui.RGBA(0xff, 0xff, 0xff, 0.08),
		Text:         ui.RGB(0xdf, 0xdf, 0xe0),
		Muted:        ui.RGBA(0xff, 0xff, 0xff, 0.55),
		Dim:          ui.RGB(0x6c, 0x6c, 0x70),
		Err:          ui.RGB(0xff, 0x6b, 0x60),
		WarnBg:       ui.RGB(0x3a, 0x30, 0x12),
		InfoBg:       ui.RGB(0x13, 0x2f, 0x4f),
		Pill:         ui.RGBA(0xff, 0xff, 0xff, 0.07),
		Page:         ui.RGB(0xdc, 0xdc, 0xe0),
		Folder:       ui.RGB(0x5a, 0xb0, 0xf7),
		Link:         ui.RGBA(0x0a, 0x84, 0xff, 0.28),
		Match:        ui.RGBA(0xff, 0xd6, 0x0a, 0.30),
		Popover:      ui.RGB(0x2c, 0x2c, 0x30),
		PopoverEdge:  ui.RGBA(0xff, 0xff, 0xff, 0.10),
		DropZone:     ui.RGBA(0x0a, 0x84, 0xff, 0.22),
		WelcomeLeft:  ui.RGB(0x2a, 0x2a, 0x2c),
		WelcomeRight: ui.RGB(0x1e, 0x1e, 0x20),
		WelcomeTile:  ui.RGB(0x3a, 0x3a, 0x3e),
		WelcomeHover: ui.RGBA(0xff, 0xff, 0xff, 0.07),
		Settings:     ui.RGB(0x1e, 0x1e, 0x20),
		Card:         ui.RGBA(0xff, 0xff, 0xff, 0.05),
		SelectedRow:  ui.RGBA(0xff, 0xff, 0xff, 0.10),
	}

	Light = Palette{
		Editor:       ui.RGB(0xff, 0xff, 0xff),
		Sidebar:      ui.RGB(0xf2, 0xf2, 0xf4),
		Toolbar:      ui.RGB(0xff, 0xff, 0xff),
		TabBar:       ui.RGB(0xeb, 0xeb, 0xed),
		TabActive:    ui.RGB(0xff, 0xff, 0xff),
		TabHover:     ui.RGBA(0, 0, 0, 0.04),
		Separator:    ui.RGBA(0, 0, 0, 0.10),
		Hairline:     ui.RGBA(0, 0, 0, 0.08),
		Text:         ui.RGB(0x1d, 0x1d, 0x1f),
		Muted:        ui.RGBA(0, 0, 0, 0.50),
		Dim:          ui.RGB(0xa1, 0xa1, 0xa6),
		Err:          ui.RGB(0xd7, 0x00, 0x15),
		WarnBg:       ui.RGB(0xff, 0xf4, 0xd4),
		InfoBg:       ui.RGB(0xe3, 0xee, 0xfc),
		Pill:         ui.RGBA(0, 0, 0, 0.05),
		Page:         ui.RGB(0xd2, 0xd2, 0xd8),
		Folder:       ui.RGB(0x2f, 0x8c, 0xe8),
		Link:         ui.RGBA(0x00, 0x7a, 0xff, 0.16),
		Match:        ui.RGBA(0xff, 0xcc, 0x00, 0.45),
		Popover:      ui.RGB(0xff, 0xff, 0xff),
		PopoverEdge:  ui.RGBA(0, 0, 0, 0.10),
		DropZone:     ui.RGBA(0x00, 0x7a, 0xff, 0.14),
		WelcomeLeft:  ui.RGB(0xf5, 0xf5, 0xf7),
		WelcomeRight: ui.RGB(0xff, 0xff, 0xff),
		WelcomeTile:  ui.RGB(0xe5, 0xe5, 0xea),
		WelcomeHover: ui.RGBA(0, 0, 0, 0.05),
		Settings:     ui.RGB(0xf7, 0xf7, 0xf7),
		Card:         ui.RGBA(0, 0, 0, 0.04),
		SelectedRow:  ui.RGBA(0, 0, 0, 0.07),
	}
)

// Of returns the palette of the appearance the window of c shows. The
// accent is the theme's: c.Theme().Accent.
func Of(c *ui.Context) *Palette { return For(c.Theme().Dark) }

// For returns the palette of an appearance.
func For(dark bool) *Palette {
	if dark {
		return &Dark
	}
	return &Light
}

// New takes the desktop's theme, its appearance and accent, and gives the
// app's: the palette's colors, with the desktop's accent, contrast and
// text size, and the editors' theme with them.
func New(base *ui.Theme, textScale float32) (*ui.Theme, *editor.Theme) {
	p := For(base.Dark)
	t := *base // keeps the desktop's accent, contrast and text size
	t.Background = p.Editor
	t.Text = p.Text
	t.TextMuted = p.Muted
	t.Border = p.Separator
	t.Spacing = 3
	t.FontSize = 13 * max(1, textScale)
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
	ed := editor.DefaultTheme(&t)
	ed.Background = p.Editor
	return &t, ed
}
