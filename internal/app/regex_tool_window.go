package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/config"
	"see/locales"
)

type regexToolWindowState struct {
	rect        ui.Rect
	initialized bool
	handle      ui.Handle
}

func (s *regexToolWindowState) resetWorkspace() {
	s.rect, s.initialized = ui.Rect{}, false
}

func (a *App) restoreRegexToolWindow(s config.Workspace) {
	if finiteRegexGeometry(s.RegexToolX) && finiteRegexGeometry(s.RegexToolY) &&
		finiteRegexGeometry(s.RegexToolWidth) && s.RegexToolWidth > 0 &&
		finiteRegexGeometry(s.RegexToolHeight) && s.RegexToolHeight > 0 {
		a.regexToolWindow.rect = ui.Rect{X: s.RegexToolX, Y: s.RegexToolY, W: s.RegexToolWidth, H: s.RegexToolHeight}
		a.regexToolWindow.initialized = true
	} else {
		a.regexToolWindow.rect, a.regexToolWindow.initialized = ui.Rect{}, false
	}
}

func (a *App) ensureRegexToolRect(bounds ui.Rect) ui.Rect {
	if !a.regexToolWindow.initialized {
		a.regexToolWindow.rect = regexToolDefaultRect(bounds)
		a.regexToolWindow.initialized = true
		a.saveConfigSoon()
		return a.regexToolWindow.rect
	}
	clamped := clampRegexToolRect(a.regexToolWindow.rect, bounds)
	if clamped != a.regexToolWindow.rect {
		a.regexToolWindow.rect = clamped
		a.saveConfigSoon()
	}
	return a.regexToolWindow.rect
}

func (a *App) regexToolWindowView(c *ui.Context, p *regexPlayground, bounds ui.Rect) {
	rect := a.ensureRegexToolRect(bounds)
	ui.Overlay(c, func() {
		window := ui.Column(c.Key("regex-tool-window")).Bind(&a.regexToolWindow.handle).Absolute().Left(rect.X).Top(rect.Y).Width(rect.W).Height(rect.H)
		window.Label(locales.RegexToolWindow()).Background(c.Theme().Surface).BorderWidth(1, 1, 1, 1).BorderColor(colors(c).Separator).Radius(9).Clip().Children(func() {
			a.regexToolTitlebar(c, p, bounds)
			a.regexPlaygroundBody(c, p, rect.W, max(float32(0), rect.H-40))
			a.regexToolResizeHandles(c, bounds)
		})
		a.regexToolWindow.handle.OnShortcut(c, 0, ui.KeyEscape, func() { a.closeRegexPlayground(p) })
	})
}

func (a *App) regexToolTitlebar(c *ui.Context, p *regexPlayground, bounds ui.Rect) {
	ui.Row(c).FillWidth().Height(38).Shrink(0).Padding(4, 10).Gap(6).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(colors(c).Separator).Children(func() {
		drag := ui.Box(c.Key("regex-tool-title-drag")).Grow(1).FillHeight().Cursor(ui.CursorMove).Label(locales.RegexToolTitlebar()).AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.RegexPlaygroundTitle()).FontSize(12).FontWeight(600).SingleLine()
		})
		if dx, dy, dragged := drag.Dragged(); dragged {
			if dx != 0 || dy != 0 {
				a.regexToolWindow.rect.X += dx
				a.regexToolWindow.rect.Y += dy
				a.regexToolWindow.rect = clampRegexToolRect(a.regexToolWindow.rect, bounds)
				a.saveConfigSoon()
			}
		}
		close := ui.Button(c, "×").Label(locales.Close()).Tooltip(locales.Close()).Size(28, 26).Shrink(0).KeepFocus()
		if close.Clicked() {
			a.closeRegexPlayground(p)
		}
	})
}

func (a *App) regexToolResizeHandles(c *ui.Context, bounds ui.Rect) {
	const grip = float32(10)
	for _, handle := range []struct {
		key    string
		label  string
		edge   regexToolResizeEdge
		cursor ui.Cursor
		style  func(ui.Element) ui.Element
	}{
		{"north", locales.RegexToolResizeNorth(), regexToolResizeTop, ui.CursorResizeNS, func(e ui.Element) ui.Element { return e.Left(grip).Right(grip).Top(0).Height(grip) }},
		{"south", locales.RegexToolResizeSouth(), regexToolResizeBottom, ui.CursorResizeNS, func(e ui.Element) ui.Element { return e.Left(grip).Right(grip).Bottom(0).Height(grip) }},
		{"west", locales.RegexToolResizeWest(), regexToolResizeLeft, ui.CursorResizeEW, func(e ui.Element) ui.Element { return e.Left(0).Top(grip).Bottom(grip).Width(grip) }},
		{"east", locales.RegexToolResizeEast(), regexToolResizeRight, ui.CursorResizeEW, func(e ui.Element) ui.Element { return e.Right(0).Top(grip).Bottom(grip).Width(grip) }},
		{"north-west", locales.RegexToolResizeNorthWest(), regexToolResizeTop | regexToolResizeLeft, ui.CursorResizeNWSE, func(e ui.Element) ui.Element { return e.Left(0).Top(0).Size(grip, grip) }},
		{"north-east", locales.RegexToolResizeNorthEast(), regexToolResizeTop | regexToolResizeRight, ui.CursorResizeNESW, func(e ui.Element) ui.Element { return e.Right(0).Top(0).Size(grip, grip) }},
		{"south-west", locales.RegexToolResizeSouthWest(), regexToolResizeBottom | regexToolResizeLeft, ui.CursorResizeNESW, func(e ui.Element) ui.Element { return e.Left(0).Bottom(0).Size(grip, grip) }},
		{"south-east", locales.RegexToolResizeSouthEast(), regexToolResizeBottom | regexToolResizeRight, ui.CursorResizeNWSE, func(e ui.Element) ui.Element { return e.Right(0).Bottom(0).Size(grip, grip) }},
	} {
		target := ui.Box(c.Key(handle.key)).Absolute().Cursor(handle.cursor).Label(handle.label)
		target = handle.style(target)
		if dx, dy, dragged := target.Dragged(); dragged && (dx != 0 || dy != 0) {
			a.regexToolWindow.rect = resizeRegexToolRect(a.regexToolWindow.rect, handle.edge, dx, dy, bounds)
			a.saveConfigSoon()
		}
	}
}

func (a *App) regexPlaygroundBody(c *ui.Context, p *regexPlayground, width, height float32) {
	if height < 0 {
		height = 0
	}
	ui.Scroll(c.Key("regex-tool-body-scroll")).Grow(1).FillWidth().MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().MinHeight(height).Children(func() {
			if width < 540 {
				ui.Column(c).FillWidth().Shrink(0).Padding(8, 10, 3).Gap(5).Children(func() {
					ui.Text(c, locales.RegexPattern()+":").FontSize(10).TextColor(colors(c).Muted)
					a.regexPatternInput(c, p, true)
					if width < 440 {
						ui.Column(c).FillWidth().Gap(2).Children(func() { a.regexPlaygroundFlags(c, p) })
					} else {
						ui.Row(c).FillWidth().Gap(8).Children(func() { a.regexPlaygroundFlags(c, p) })
					}
				})
			} else {
				ui.Row(c).FillWidth().Shrink(0).Padding(8, 10, 3).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, locales.RegexPattern()+":").FontSize(11).TextColor(colors(c).Muted).Width(72).Shrink(0)
					a.regexPatternInput(c, p, false)
					ui.Text(c, locales.RegexEngineBadge()).FontSize(11).TextColor(c.Theme().Accent).SingleLine()
				})
				ui.Row(c).FillWidth().Shrink(0).Padding(1, 10, 5).Gap(10).AlignItems(ui.Center).Children(func() { a.regexPlaygroundFlags(c, p) })
			}
			p.refresh(c)
			if p.pattern != "" {
				a.regexPlaygroundDiagram(c, p, width-20, regexPlaygroundDiagramMaxHeight(height))
			}
			if width < 580 {
				resultsMinHeight := float32(72)
				if p.selected >= 0 && p.selected < len(p.result.Matches) && len(p.result.Matches[p.selected].Captures) > 1 {
					resultsMinHeight = 240
				}
				ui.Column(c).Grow(1).FillWidth().MinHeight(0).Gap(0).BorderWidth(1, 0, 0, 0).BorderColor(colors(c).Separator).Children(func() {
					ui.Column(c).Grow(1).FillWidth().MinHeight(72).Padding(7, 10, 6).Children(func() {
						ui.Text(c, locales.RegexSample()+":").FontSize(10).TextColor(colors(c).Muted).Padding(0, 0, 4)
						view := editor.View(c.Key("regex-playground-sample"), p.sample).Grow(1).FillWidth().MinHeight(0)
						if a.focusEditor == p.sample {
							view.Focus()
							a.focusEditor = nil
						}
					})
					ui.Column(c).Grow(1).FillWidth().MinHeight(resultsMinHeight).Padding(7, 10, 6).BorderWidth(1, 0, 0, 0).BorderColor(colors(c).Separator).Children(func() { a.regexPlaygroundResults(c, p) })
				})
				return
			}
			ui.Row(c).Grow(1).FillWidth().MinHeight(0).Gap(0).BorderWidth(1, 0, 0, 0).BorderColor(colors(c).Separator).Children(func() {
				ui.Column(c).Grow(11).Fill().MinWidth(0).Padding(8, 10, 8).Children(func() {
					ui.Text(c, locales.RegexSample()+":").FontSize(10).TextColor(colors(c).Muted).Padding(0, 0, 4)
					view := editor.View(c.Key("regex-playground-sample"), p.sample).Grow(1).FillWidth().MinHeight(0)
					if a.focusEditor == p.sample {
						view.Focus()
						a.focusEditor = nil
					}
				})
				ui.Column(c).Grow(9).Fill().MinWidth(0).Padding(8, 10, 8).BorderWidth(0, 0, 0, 1).BorderColor(colors(c).Separator).Children(func() { a.regexPlaygroundResults(c, p) })
			})
		})
	})
}

func (a *App) regexPatternInput(c *ui.Context, p *regexPlayground, full bool) {
	if full {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			input := ui.TextInput(c, &p.pattern).Label(locales.RegexPattern()).Font("monospace").FontSize(13).Grow(1).MinWidth(80)
			if input.Changed() {
				p.patternVersion++
				p.unsupportedFlags = nil
			}
			ui.Text(c, locales.RegexEngineBadge()).FontSize(10).TextColor(c.Theme().Accent).SingleLine()
		})
		return
	}
	input := ui.TextInput(c, &p.pattern).Label(locales.RegexPattern()).Font("monospace").FontSize(13).Grow(1).MinWidth(80)
	if input.Changed() {
		p.patternVersion++
		p.unsupportedFlags = nil
	}
}

func (a *App) regexPlaygroundFlags(c *ui.Context, p *regexPlayground) {
	ui.Checkbox(c, &p.flags.IgnoreCase, locales.RegexIgnoreCase())
	ui.Checkbox(c, &p.flags.Multiline, locales.RegexMultiline())
	ui.Checkbox(c, &p.flags.DotAll, locales.RegexDotAll())
}
