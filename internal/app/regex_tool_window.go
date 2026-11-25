package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/config"
	"see/locales"
)

const (
	regexToolWindowCornerRadius = float32(9)
	regexToolWindowBorderWidth  = float32(1)
	regexToolCornerGripSize     = float32(30)
)

type regexToolWindowState struct {
	rect              ui.Rect
	initialized       bool
	handle            ui.Handle
	workspaceHandle   ui.Handle
	minWidth          float32
	minHeight         float32
	minimumSet        bool
	fitToSourceOnOpen bool
}

func (s *regexToolWindowState) resetWorkspace() {
	s.rect, s.initialized, s.minimumSet, s.fitToSourceOnOpen = ui.Rect{}, false, false, false
	s.minWidth, s.minHeight = 0, 0
}

func (a *App) restoreRegexToolWindow(s config.Workspace) {
	a.regexToolWindow.minimumSet = false
	a.regexToolWindow.minWidth, a.regexToolWindow.minHeight = 0, 0
	a.regexToolWindow.fitToSourceOnOpen = false
	if finiteRegexGeometry(s.RegexToolX) && finiteRegexGeometry(s.RegexToolY) &&
		finiteRegexGeometry(s.RegexToolWidth) && s.RegexToolWidth > 0 &&
		finiteRegexGeometry(s.RegexToolHeight) && s.RegexToolHeight > 0 {
		a.regexToolWindow.rect = ui.Rect{X: s.RegexToolX, Y: s.RegexToolY, W: s.RegexToolWidth, H: s.RegexToolHeight}
		a.regexToolWindow.initialized = true
	} else {
		a.regexToolWindow.rect, a.regexToolWindow.initialized = ui.Rect{}, false
	}
}

func (a *App) ensureRegexToolRect(bounds, sourceBounds ui.Rect) ui.Rect {
	if sourceBounds.W <= 0 || sourceBounds.H <= 0 {
		sourceBounds = bounds
	}
	if !a.regexToolWindow.minimumSet {
		a.regexToolWindow.minWidth, a.regexToolWindow.minHeight = regexToolMinimumSize(bounds, sourceBounds)
		a.regexToolWindow.minimumSet = true
	}
	if !a.regexToolWindow.initialized {
		a.regexToolWindow.rect = regexToolDefaultRect(sourceBounds)
		a.regexToolWindow.initialized = true
		a.regexToolWindow.fitToSourceOnOpen = false
		a.saveConfigSoon()
		return a.regexToolWindow.rect
	}
	clampBounds := bounds
	if a.regexToolWindow.fitToSourceOnOpen {
		clampBounds = sourceBounds
	}
	clamped := clampRegexToolRectWithMinimum(a.regexToolWindow.rect, clampBounds, a.regexToolWindow.minWidth, a.regexToolWindow.minHeight)
	a.regexToolWindow.fitToSourceOnOpen = false
	if clamped != a.regexToolWindow.rect {
		a.regexToolWindow.rect = clamped
		a.saveConfigSoon()
	}
	return a.regexToolWindow.rect
}

func (a *App) regexToolSourceVisible(p *regexPlayground) bool {
	if !a.regexPlaygroundSourceLive(p) || p.group.active != p.doc.path {
		return false
	}
	return a.maximized == nil || a.maximized == p.group || a.layout.find(a.maximized) == nil || len(a.groups()) <= 1
}

func (a *App) regexToolWindowView(c *ui.Context, p *regexPlayground, bounds, sourceBounds ui.Rect) {
	windowWidth, windowHeight := c.Size()
	bounds = regexToolBoundsWithinViewport(bounds, windowWidth, windowHeight)
	sourceBounds = regexToolBoundsWithinViewport(sourceBounds, windowWidth, windowHeight)
	rect := a.ensureRegexToolRect(bounds, sourceBounds)
	ui.Overlay(c, func() {
		window := ui.Column(c.Key("regex-tool-window")).Bind(&a.regexToolWindow.handle).Absolute().Left(rect.X).Top(rect.Y).Width(rect.W).Height(rect.H)
		window.Label(locales.RegexToolWindow()).Background(c.Theme().Surface).BorderWidth(regexToolWindowBorderWidth, regexToolWindowBorderWidth, regexToolWindowBorderWidth, regexToolWindowBorderWidth).BorderColor(colors(c).Separator).Radius(regexToolWindowCornerRadius).Clip().Children(func() {
			a.regexToolTitlebar(c, p, bounds)
			a.regexPlaygroundBody(c, p, rect.W, max(float32(0), rect.H-40))
			a.regexToolResizeHandles(c, bounds)
		})
		a.regexToolWindow.handle.OnShortcut(c, 0, ui.KeyEscape, func() { a.closeRegexPlayground(p) })
	})
}

func (a *App) regexToolTitlebar(c *ui.Context, p *regexPlayground, bounds ui.Rect) {
	ui.Row(c).FillWidth().Height(38).Shrink(0).Padding(4, 10).Gap(6).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(colors(c).Separator).Children(func() {
		drag := ui.Row(c.Key("regex-tool-title-drag")).Grow(1).FillHeight().Cursor(ui.CursorMove).Label(locales.RegexToolTitlebar()).Justify(ui.Start).AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.RegexPlaygroundTitle()).FontSize(12).FontWeight(600).SingleLine()
		})
		if dx, dy, dragged := drag.Dragged(); dragged {
			if dx != 0 || dy != 0 {
				a.regexToolWindow.rect.X += dx
				a.regexToolWindow.rect.Y += dy
				a.regexToolWindow.rect = clampRegexToolRectWithMinimum(a.regexToolWindow.rect, bounds, a.regexToolWindow.minWidth, a.regexToolWindow.minHeight)
				a.saveConfigSoon()
			}
		}
		close := ui.ButtonBase(c.Key("regex-tool-close")).Label(locales.Close()).Tooltip(locales.Close()).Size(28, 26).Shrink(0).KeepFocus().Center().Cursor(ui.CursorPointer)
		close.Children(func() {
			ui.Text(c, "×").FontSize(16)
		})
		if close.Clicked() {
			a.closeRegexPlayground(p)
		}
	})
}

func (a *App) regexToolResizeHandles(c *ui.Context, bounds ui.Rect) {
	const (
		edgeGrip          = float32(10)
		northEastTopGrip  = float32(5)
		northEastSideGrip = float32(8)
	)
	addTarget := func(key, label string, edge regexToolResizeEdge, cursor ui.Cursor, style func(ui.Element) ui.Element) {
		target := ui.Box(c.Key(key)).Absolute().Cursor(cursor)
		if label != "" {
			target.Label(label)
		}
		target = style(target)
		corner := edge&(regexToolResizeLeft|regexToolResizeRight) != 0 &&
			edge&(regexToolResizeTop|regexToolResizeBottom) != 0
		if corner && target.Hovered() {
			gripColor := colors(c).Muted.Alpha(0.65)
			resizeEdge := edge
			target.DrawOver(func(p *ui.Painter, r ui.Rect) {
				gripBounds := regexToolCornerGripBounds(r, resizeEdge, regexToolCornerGripSize)
				drawRegexToolCornerGrip(p, gripBounds, resizeEdge, gripColor)
			})
		}
		if dx, dy, dragged := target.Dragged(); dragged && (dx != 0 || dy != 0) {
			a.regexToolWindow.rect = resizeRegexToolRectWithMinimum(a.regexToolWindow.rect, edge, dx, dy, bounds, a.regexToolWindow.minWidth, a.regexToolWindow.minHeight)
			a.saveConfigSoon()
		}
	}
	for _, handle := range []struct {
		key    string
		label  string
		edge   regexToolResizeEdge
		cursor ui.Cursor
		style  func(ui.Element) ui.Element
	}{
		{"north", locales.RegexToolResizeNorth(), regexToolResizeTop, ui.CursorResizeN, func(e ui.Element) ui.Element {
			return e.Left(regexToolCornerGripSize).Right(regexToolCornerGripSize).Top(0).Height(edgeGrip)
		}},
		{"south", locales.RegexToolResizeSouth(), regexToolResizeBottom, ui.CursorResizeS, func(e ui.Element) ui.Element {
			return e.Left(regexToolCornerGripSize).Right(regexToolCornerGripSize).Bottom(0).Height(edgeGrip)
		}},
		{"west", locales.RegexToolResizeWest(), regexToolResizeLeft, ui.CursorResizeW, func(e ui.Element) ui.Element {
			return e.Left(0).Top(regexToolCornerGripSize).Bottom(regexToolCornerGripSize).Width(edgeGrip)
		}},
		{"east", locales.RegexToolResizeEast(), regexToolResizeRight, ui.CursorResizeE, func(e ui.Element) ui.Element {
			return e.Right(0).Top(regexToolCornerGripSize).Bottom(regexToolCornerGripSize).Width(edgeGrip)
		}},
		{"north-west", locales.RegexToolResizeNorthWest(), regexToolResizeTop | regexToolResizeLeft, ui.CursorResizeNWSE, func(e ui.Element) ui.Element {
			return e.Left(0).Top(0).Size(regexToolCornerGripSize, regexToolCornerGripSize)
		}},
		{"north-east", locales.RegexToolResizeNorthEast(), regexToolResizeTop | regexToolResizeRight, ui.CursorResizeNESW, func(e ui.Element) ui.Element {
			return e.Right(0).Top(0).Width(northEastSideGrip).Height(regexToolCornerGripSize)
		}},
		{"south-west", locales.RegexToolResizeSouthWest(), regexToolResizeBottom | regexToolResizeLeft, ui.CursorResizeNESW, func(e ui.Element) ui.Element {
			return e.Left(0).Bottom(0).Size(regexToolCornerGripSize, regexToolCornerGripSize)
		}},
		{"south-east", locales.RegexToolResizeSouthEast(), regexToolResizeBottom | regexToolResizeRight, ui.CursorResizeNWSE, func(e ui.Element) ui.Element {
			return e.Right(0).Bottom(0).Size(regexToolCornerGripSize, regexToolCornerGripSize)
		}},
	} {
		if handle.key == "north-east" {
			addTarget("north-east-top", "", handle.edge, handle.cursor, func(e ui.Element) ui.Element {
				return e.Right(0).Top(0).Width(regexToolCornerGripSize).Height(northEastTopGrip)
			})
		}
		addTarget(handle.key, handle.label, handle.edge, handle.cursor, handle.style)
	}
}

func regexToolCornerGripBounds(r ui.Rect, edge regexToolResizeEdge, size float32) ui.Rect {
	bounds := ui.Rect{W: size, H: size}
	bounds.X, bounds.Y = r.X, r.Y
	if edge&regexToolResizeRight != 0 {
		bounds.X = r.X + r.W - size
	}
	if edge&regexToolResizeBottom != 0 {
		bounds.Y = r.Y + r.H - size
	}
	return bounds
}

func drawRegexToolCornerGrip(p *ui.Painter, r ui.Rect, edge regexToolResizeEdge, color ui.Color) {
	cornerRadius := regexToolWindowCornerRadius - regexToolWindowBorderWidth
	radius := cornerRadius - 2
	path := regexToolCornerGripPath(r, edge, cornerRadius, radius)
	p.StrokePath(&path, 1.4, color)
}

func regexToolCornerGripPath(r ui.Rect, edge regexToolResizeEdge, cornerRadius, radius float32) ui.Path {
	sx, sy := regexToolCornerSigns(edge)
	cornerX := r.X + r.W/2 + sx*r.W/2
	cornerY := r.Y + r.H/2 + sy*r.H/2
	centerX, centerY := cornerX-sx*cornerRadius, cornerY-sy*cornerRadius
	const kappa = float32(0.5522848)
	startX, startY := centerX, centerY+sy*radius
	control1X, control1Y := centerX+sx*kappa*radius, startY
	control2X, control2Y := centerX+sx*radius, centerY+sy*kappa*radius
	endX, endY := centerX+sx*radius, centerY
	var path ui.Path
	path.MoveTo(startX, startY).CubeTo(control1X, control1Y, control2X, control2Y, endX, endY)
	return path
}

func regexToolCornerSigns(edge regexToolResizeEdge) (float32, float32) {
	sx, sy := float32(-1), float32(-1)
	if edge&regexToolResizeRight != 0 {
		sx = 1
	}
	if edge&regexToolResizeBottom != 0 {
		sy = 1
	}
	return sx, sy
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
