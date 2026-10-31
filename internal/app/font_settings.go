package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

func (a *App) fontPreview(c *ui.Context, terminal bool) {
	family, size := a.cfg.FontFamily, a.cfg.EditorFontSize()
	if terminal {
		family, size = a.cfg.TermFontFamily, a.cfg.TerminalFontSize()
	}
	if family == "" {
		family = "monospace"
	}
	ui.Column(c).FillWidth().Padding(12, 14).Gap(2).Radius(10).Background(colors(c).Card).Border(1, colors(c).Separator).Children(func() {
		if terminal {
			ui.Text(c, "~/project $ go test ./...").Font(family).FontSize(float32(size)).SingleLine()
			ui.Text(c, "ok    example/project    0.24s").Font(family).FontSize(float32(size)).TextColor(colors(c).Muted).SingleLine()
		} else {
			ui.Text(c, "func main() {").Font(family).FontSize(float32(size)).SingleLine()
			ui.Text(c, "    fmt.Println(\"Hello, world!\")").Font(family).FontSize(float32(size)).TextColor(colors(c).Muted).SingleLine()
			ui.Text(c, "}").Font(family).FontSize(float32(size)).SingleLine()
		}
	})
}

func (a *App) fontPicker(c *ui.Context, terminal bool) {
	s := &a.settings
	family := a.cfg.FontFamily
	if terminal {
		family = a.cfg.TermFontFamily
	}
	label := family
	if label == "" {
		label = "System Monospace"
	}
	labeled(c, "Font Family", "Choose a font for code and text.")
	ui.Text(c, label).TextColor(colors(c).Muted).SingleLine().Shrink(1)
	button := ui.Button(c, "Choose…").Label("Choose Font").Bind(&s.fontButton)
	button.OnClick(func() {
		a.host.openFontPopover(s.fontButton.Bounds(c), terminal)
	})
}

func (a *App) fontPopover(c *ui.Context, terminal bool) {
	a.applyAppearance(c)
	c.SetTheme(a.theme)
	s := &a.settings
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	} else {
		c.Root().Background(colors(c).Popover)
	}
	services := c.Services()
	if s.fontFamilies == nil && s.fontLoading == nil {
		s.fontLoading = make(chan []ui.FontFamily, 1)
		ch := s.fontLoading
		go func() { ch <- ui.FontFamilies(); services.Invalidate() }()
	}
	if s.fontLoading != nil {
		select {
		case families := <-s.fontLoading:
			s.fontFamilies = families
			s.fontLoading = nil
		default:
		}
	}
	family := a.cfg.FontFamily
	if terminal {
		family = a.cfg.TermFontFamily
	}
	ui.Column(c).Fill().Padding(8).Gap(6).Children(func() {
		ui.SearchField(c, &s.fontQuery).Label("Search Fonts").Placeholder("Search fonts").FillWidth().AutoFocus()
		ui.Segmented(c, &s.fontFilter, "Monospaced", "All Fonts").Label("Font Filter").FillWidth()
		ui.Scroll(c).FillWidth().Grow(1).Children(func() {
			ui.Column(c).FillWidth().Gap(1).Children(func() {
				if s.fontLoading != nil {
					ui.Text(c, "Loading fonts…").TextColor(colors(c).Muted).Padding(8)
					return
				}
				query := strings.ToLower(strings.TrimSpace(s.fontQuery))
				count := 0
				if query == "" {
					a.fontOption(c, "", "System Monospace", family == "", terminal)
					count++
				}
				for _, f := range s.fontFamilies {
					if s.fontFilter == 0 && !f.Monospaced || !strings.Contains(strings.ToLower(f.Name), query) {
						continue
					}
					a.fontOption(c, f.Name, f.Name, family == f.Name, terminal)
					count++
				}
				if count == 0 {
					ui.Text(c, "No fonts found").TextColor(colors(c).Muted).Padding(8)
				}
			})
		})
	})
}

func (a *App) fontOption(c *ui.Context, family, label string, selected, terminal bool) {
	row := ui.ButtonBase(c.Key("font:"+family)).Label(label).FillWidth().Padding(7, 10).Radius(8)
	if selected {
		row.Background(c.Theme().Accent)
	} else if row.Hovered() {
		row.Background(colors(c).Pill)
	}
	row.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, label).Grow(1).SingleLine().TextColor(rowText(selected, colors(c).Text))
			if selected {
				symbolIcon(c, icCheck, 12, rowText(true, colors(c).Text))
			}
		})
	})
	row.OnClick(func() {
		if terminal {
			a.cfg.TermFontFamily = family
			a.applyTerminalPrefs()
		} else {
			a.cfg.FontFamily = family
			a.applyEditorPrefsAll()
		}
		a.settings.fontOpen = false
		a.settingsChanged()
		a.host.closeFontPopover()
	})
}
