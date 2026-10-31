package app

import (
	"github.com/egoist/mygo/ui"

	"see/internal/theme"
)

// colors returns the palette of the appearance the window of c shows. The
// accent is the theme's: c.Theme().Accent.
func colors(c *ui.Context) *theme.Palette { return theme.Of(c) }

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
func (a *App) applyAppearance(c *ui.Context) {
	base := c.Theme() // the default theme: the system's appearance and accent
	if a.theme != nil && a.themeDark == base.Dark && a.themeAccent == base.Accent {
		return
	}
	a.themeDark, a.themeAccent = base.Dark, base.Accent
	a.theme, a.edTheme = theme.New(base, c.Preferences().TextScale)
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
	a.host.SetAppearance(s)
	a.host.RefreshMenu() // its radio items show the choice
}
