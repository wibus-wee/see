package app

import (
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"see/internal/theme"
)

// colors returns the palette of the appearance the window of c shows. The
// accent is the theme's: c.Theme().Accent.
func colors(c *ui.Context) *theme.Palette { return theme.Of(c) }

// popoverGlass is the material of the app's floating popovers: large
// enough for the glass's Clarity, their content reads through them.
func popoverGlass() glass.Glass { return glass.Glass{Clarity: 0.5} }

// popoverMenuPanel styles the panel of a drawn menu — and of each of its
// submenus — as the app's popovers (PopoverMenuButtonOptions.Panel).
func popoverMenuPanel(panel ui.Element) { panel.Material(popoverGlass()) }

// colors returns the palette of the appearance the app last drew in, for
// what is not drawn in a frame of its own.
func (a *App) colors() *theme.Palette {
	if a.theme != nil && !a.theme.Dark {
		return &theme.Light
	}
	return &theme.Dark
}

// applyAppearance takes the window's appearance, light or dark, which
// follows the system unless the user chose one, and its accent color:
// the app's theme and colors follow, and the editors' themes with them.
// It does the work only when the appearance changes.
//
// The theme objects never change: their contents are replaced, so every
// editor and diff holding them — a preview pinned in a panel or a peek
// as much as a tab's editor — follows without being found and re-pointed.
func (a *App) applyAppearance(c *ui.Context) {
	base := c.Theme() // the default theme: the system's appearance and accent
	if a.theme != nil && a.themeDark == base.Dark && a.themeAccent == base.Accent {
		return
	}
	a.themeDark, a.themeAccent = base.Dark, base.Accent
	a.themeGen++
	ut, ed := theme.New(base, c.Preferences().TextScale)
	if a.theme == nil {
		a.theme, a.edTheme = ut, ed
		return
	}
	*a.theme, *a.edTheme = *ut, *ed
}

// chooseAppearance forces light or dark, or "" follows the system, and
// remembers it.
func (a *App) chooseAppearance(s string) {
	a.cfg.Appearance = s
	a.saveConfig()
	a.host.SetAppearance(s)
	a.host.RefreshMenu() // its radio items show the choice
}
