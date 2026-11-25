package app

import (
	"fmt"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"see/internal/regexplayground"
	"see/locales"
)

type regexCaptureColor struct {
	stroke ui.Color
	fill   ui.Color
}

type regexRailPoint struct {
	x, y float32
}

type regexRailLayout struct {
	node        regexplayground.DiagramNode
	width       float32
	height      float32
	children    []regexRailLayout
	positions   []regexRailPoint
	groupTitle  string
	groupValue  string
	groupColor  regexCaptureColor
	repeatLabel string
}

func regexCapturePalette(c *ui.Context, number int) regexCaptureColor {
	light := [...]regexCaptureColor{
		{stroke: ui.RGB(0x16, 0x67, 0xb1), fill: ui.RGB(0x55, 0xa5, 0xec).Alpha(0.20)},
		{stroke: ui.RGB(0x83, 0x50, 0xa5), fill: ui.RGB(0xb2, 0x82, 0xd5).Alpha(0.20)},
		{stroke: ui.RGB(0x1f, 0x79, 0x4d), fill: ui.RGB(0x5f, 0xb8, 0x7e).Alpha(0.20)},
		{stroke: ui.RGB(0xa2, 0x5b, 0x11), fill: ui.RGB(0xe5, 0xa0, 0x4d).Alpha(0.20)},
	}
	dark := [...]regexCaptureColor{
		{stroke: ui.RGB(0x72, 0xc7, 0xff), fill: ui.RGB(0x1b, 0x7d, 0xc2).Alpha(0.26)},
		{stroke: ui.RGB(0xc9, 0xa4, 0xff), fill: ui.RGB(0x84, 0x50, 0xb7).Alpha(0.27)},
		{stroke: ui.RGB(0x79, 0xd9, 0xa3), fill: ui.RGB(0x27, 0x9c, 0x60).Alpha(0.26)},
		{stroke: ui.RGB(0xff, 0xbd, 0x75), fill: ui.RGB(0xc5, 0x72, 0x28).Alpha(0.28)},
	}
	index := (max(number, 1) - 1) % len(light)
	if c.Theme().Dark {
		return dark[index]
	}
	return light[index]
}

func (a *App) regexPlaygroundDiagram(c *ui.Context, p *regexPlayground, availableWidth, maxHeight float32) {
	var selected *regexplayground.Match
	captures := map[int]regexplayground.Capture{}
	if p.selected >= 0 && p.selected < len(p.result.Matches) {
		selected = &p.result.Matches[p.selected]
		for _, capture := range selected.Captures {
			captures[capture.Number] = capture
		}
	}
	var layout regexRailLayout
	if p.diagramValid {
		layout = layoutRegexRail(p.diagram.Root, captures, selected != nil, c)
	}
	lineColor := colors(c).Text.Alpha(0.62)
	textColor := colors(c).Text
	mutedColor := colors(c).Muted
	surfaceColor := c.Theme().Surface
	viewportWidth := max32(200, availableWidth-24)
	viewportHeight := float32(48)
	contentWidth, contentHeight := viewportWidth, viewportHeight
	if p.diagramValid {
		viewportHeight = regexPlaygroundDiagramViewportHeight(layout.height, maxHeight)
		contentWidth = max32(viewportWidth, layout.width+76)
		contentHeight = max32(viewportHeight, layout.height)
	}

	ui.Column(c).FillWidth().Shrink(0).Gap(2).Padding(3, 12, 4).BorderWidth(1, 0, 0, 0).BorderColor(colors(c).Separator).Children(func() {
		ui.Row(c).FillWidth().Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.RegexDiagramTitle()).FontSize(11).FontWeight(600)
			if p.diagramValid && p.diagram.Abbreviated {
				ui.Text(c, locales.RegexDiagramAbbreviated()).FontSize(9).TextColor(mutedColor).SingleLine()
			}
			ui.Box(c).Grow(1)
		})
		if !p.diagramValid {
			message := locales.RegexDiagramUnavailable()
			if p.pattern == "" {
				message = locales.RegexEnterPattern()
			}
			ui.Box(c).FillWidth().Height(viewportHeight).Shrink(0).Center().Children(func() {
				ui.Text(c, message).FontSize(11).TextColor(mutedColor)
			})
		} else {
			startLabel, endLabel := locales.RegexDiagramStart(), locales.RegexDiagramEnd()
			ui.ScrollBoth(c.Key("regex-playground-diagram")).FillWidth().Height(viewportHeight).Shrink(0).Children(func() {
				ui.Box(c).Size(contentWidth, contentHeight).Shrink(0).Draw(func(paint *ui.Painter, r ui.Rect) {
					drawRegexRailDiagram(paint, r, layout, startLabel, endLabel, lineColor, textColor, mutedColor, surfaceColor)
				})
			})
		}
		ui.Text(c, locales.RegexDiagramNote()).FontSize(9).TextColor(mutedColor).SingleLine().Ellipsis("…")
	})
}

func layoutRegexRail(node regexplayground.DiagramNode, captures map[int]regexplayground.Capture, hasMatch bool, c *ui.Context) regexRailLayout {
	layout := regexRailLayout{node: node}
	for _, child := range node.Children {
		layout.children = append(layout.children, layoutRegexRail(child, captures, hasMatch, c))
	}
	switch node.Kind {
	case regexplayground.DiagramTerminal, regexplayground.DiagramEmpty, regexplayground.DiagramEllipsis:
		label := node.Label
		if node.Class != regexplayground.DiagramClassNone {
			label = regexDiagramClassLabel(node.Class)
		}
		if node.Kind == regexplayground.DiagramEmpty {
			label = "ε"
		} else if node.Kind == regexplayground.DiagramEllipsis {
			label = "…"
		}
		label = regexRailTrim(label, 18)
		layout.node.Label = label
		layout.width = max32(42, regexRailTextWidth(label, 11)+20)
		layout.height = 30
	case regexplayground.DiagramSequence:
		layoutSequence(&layout)
	case regexplayground.DiagramAlternate:
		layoutAlternate(&layout)
	case regexplayground.DiagramCapture:
		capture := captures[node.Capture]
		layout.groupTitle = fmt.Sprintf(locales.RegexDiagramGroup(), node.Capture)
		if node.Name != "" {
			layout.groupTitle += " · " + node.Name
		}
		layout.groupTitle = regexRailTrim(layout.groupTitle, 28)
		layout.groupColor = regexCapturePalette(c, node.Capture)
		switch {
		case !hasMatch:
			layout.groupValue = locales.RegexNoMatches()
		case !capture.Matched:
			layout.groupValue = locales.RegexNoCapture()
		case capture.Start == capture.End:
			layout.groupValue = locales.RegexEmptyCapture()
		default:
			layout.groupValue = regexCaptureDiagramValue(capture.Value)
		}
		childWidth, childHeight := float32(28), float32(30)
		if len(layout.children) > 0 {
			childWidth, childHeight = layout.children[0].width, layout.children[0].height
		}
		layout.width = max32(childWidth+16, regexRailTextWidth(layout.groupTitle, 10)+14, regexRailTextWidth(layout.groupValue, 10)+14)
		layout.height = childHeight + 26
		layout.positions = []regexRailPoint{{x: (layout.width - childWidth) / 2, y: 13}}
	case regexplayground.DiagramRepeat:
		childWidth, childHeight := float32(28), float32(30)
		if len(layout.children) > 0 {
			childWidth, childHeight = layout.children[0].width, layout.children[0].height
		}
		layout.repeatLabel = regexDiagramRepeatLabel(node)
		layout.width = max32(childWidth+28, regexRailTextWidth(layout.repeatLabel, 11)+14)
		layout.height = childHeight + 36
		layout.positions = []regexRailPoint{{x: (layout.width - childWidth) / 2, y: (layout.height - childHeight) / 2}}
	default:
		layout.width, layout.height = 38, 28
	}
	return layout
}

func regexPlaygroundDiagramMaxHeight(bodyHeight float32) float32 {
	return min32(148, max32(72, bodyHeight*0.36))
}

func regexPlaygroundDiagramViewportHeight(layoutHeight, maxHeight float32) float32 {
	return min32(max32(layoutHeight, 72), maxHeight)
}

func regexDiagramClassLabel(class regexplayground.DiagramClass) string {
	switch class {
	case regexplayground.DiagramClassLetter:
		return locales.RegexDiagramLetter()
	case regexplayground.DiagramClassDigit:
		return locales.RegexDiagramDigit()
	case regexplayground.DiagramClassWhitespace:
		return locales.RegexDiagramWhitespace()
	case regexplayground.DiagramClassWord:
		return locales.RegexDiagramWord()
	case regexplayground.DiagramClassNonDigit:
		return locales.RegexDiagramNonDigit()
	case regexplayground.DiagramClassNonWhitespace:
		return locales.RegexDiagramNonWhitespace()
	case regexplayground.DiagramClassNonWord:
		return locales.RegexDiagramNonWord()
	default:
		return ""
	}
}

func regexDiagramRepeatLabel(node regexplayground.DiagramNode) string {
	label := node.Label
	if node.Lazy && len(label) > 0 && label[len(label)-1] == '?' {
		label = label[:len(label)-1]
	}
	switch {
	case node.RepeatMin == 0 && node.RepeatMax == 1:
		label = locales.RegexDiagramOptional()
	case node.RepeatMin == 1 && node.RepeatMax < 0:
		label = "1+"
	case node.RepeatMin == 0 && node.RepeatMax < 0:
		label = "0+"
	}
	if node.Lazy {
		label += " · " + locales.RegexDiagramLazy()
	}
	return label
}

func layoutSequence(layout *regexRailLayout) {
	if len(layout.children) == 0 {
		layout.width, layout.height = 28, 28
		return
	}
	const gap = float32(10)
	var totalWidth, maxHeight float32
	for i, child := range layout.children {
		if i > 0 {
			totalWidth += gap
		}
		layout.positions = append(layout.positions, regexRailPoint{x: totalWidth})
		totalWidth += child.width
		maxHeight = max32(maxHeight, child.height)
	}
	layout.width, layout.height = totalWidth, maxHeight
	for i := range layout.positions {
		layout.positions[i].y = (maxHeight - layout.children[i].height) / 2
	}
}

func layoutAlternate(layout *regexRailLayout) {
	if len(layout.children) == 0 {
		layout.width, layout.height = 28, 28
		return
	}
	const gap = float32(6)
	var maxWidth, totalHeight float32
	for i, child := range layout.children {
		maxWidth = max32(maxWidth, child.width)
		if i > 0 {
			totalHeight += gap
		}
		totalHeight += child.height
	}
	layout.width, layout.height = maxWidth+96, totalHeight
	y := float32(0)
	for _, child := range layout.children {
		layout.positions = append(layout.positions, regexRailPoint{x: 32, y: y})
		y += child.height + gap
	}
}

func drawRegexRailDiagram(paint *ui.Painter, bounds ui.Rect, layout regexRailLayout, startLabel, endLabel string, lineColor, textColor, mutedColor, surfaceColor ui.Color) {
	if layout.width <= 0 || layout.height <= 0 {
		return
	}
	const terminalSpace = float32(76)
	width := layout.width + terminalSpace
	x := bounds.X + (bounds.W-width)/2 + terminalSpace/2
	y := bounds.Y + (bounds.H-layout.height)/2
	mid := y + layout.height/2
	startX, endX := x-24, x+layout.width+24

	paint.Text(startX-14, mid-17, startLabel, 11, mutedColor)
	drawRailStraight(paint, startX+4, mid, x, mid, 1.8, lineColor)
	var start ui.Path
	start.Circle(startX, mid, 3)
	paint.FillPath(&start, lineColor)

	drawRegexRailNode(paint, layout, x, y, 1, lineColor, textColor, mutedColor, surfaceColor)
	drawRailStraight(paint, x+layout.width, mid, endX-5, mid, 1.8, lineColor)
	var end ui.Path
	end.Circle(endX, mid, 5)
	paint.StrokePath(&end, 1.5, lineColor)
	var endDot ui.Path
	endDot.Circle(endX, mid, 2)
	paint.FillPath(&endDot, lineColor)
	paint.Text(endX-11, mid-17, endLabel, 11, mutedColor)
}

func drawRegexRailNode(paint *ui.Painter, layout regexRailLayout, x, y, scale float32, lineColor, textColor, mutedColor, surfaceColor ui.Color) {
	width, height := layout.width*scale, layout.height*scale
	mid := y + height/2
	switch layout.node.Kind {
	case regexplayground.DiagramSequence:
		if len(layout.children) == 0 {
			drawRailStraight(paint, x, mid, x+width, mid, 1.8*scale, lineColor)
			return
		}
		for i, child := range layout.children {
			pos := layout.positions[i]
			childX, childY := x+pos.x*scale, y+pos.y*scale
			drawRegexRailNode(paint, child, childX, childY, scale, lineColor, textColor, mutedColor, surfaceColor)
		}
		drawRegexRailSequenceConnections(paint, layout, x, y, mid, width, scale, lineColor)
	case regexplayground.DiagramAlternate:
		if len(layout.children) == 0 {
			drawRailStraight(paint, x, mid, x+width, mid, 1.8*scale, lineColor)
			return
		}
		left, right := x+12*scale, x+width-12*scale
		exitLane := x + width - 48*scale
		for i, child := range layout.children {
			pos := layout.positions[i]
			branchY := y + (pos.y+child.height/2)*scale
			drawRailCurve(paint, left, mid, x+pos.x*scale, branchY, scale, lineColor, false)
			drawRegexRailNode(paint, child, x+pos.x*scale, y+pos.y*scale, scale, lineColor, textColor, mutedColor, surfaceColor)
			childRight := x + (pos.x+child.width)*scale
			drawRailStraight(paint, childRight, branchY, exitLane, branchY, 1.8*scale, lineColor)
			drawRailConnection(paint, exitLane, branchY, right, mid, scale, lineColor, true)
		}
		drawRailStraight(paint, x, mid, left, mid, 1.8*scale, lineColor)
		drawRailStraight(paint, right, mid, x+width, mid, 1.8*scale, lineColor)
		drawRailJunction(paint, left, mid, scale, lineColor)
		drawRailJunction(paint, right, mid, scale, lineColor)
	case regexplayground.DiagramCapture:
		color := layout.groupColor
		r := ui.Rect{X: x + 0.5*scale, Y: y + 0.5*scale, W: max32(0, width-scale), H: max32(0, height-scale)}
		paint.Fill(r, color.fill, 6*scale)
		paint.Stroke(r, color.stroke, 6*scale, 1.1*scale)
		paint.Text(x+7*scale, y+3*scale, layout.groupTitle, 10*scale, color.stroke)
		valueColor := color.stroke
		if layout.groupValue == locales.RegexNoCapture() || layout.groupValue == locales.RegexNoMatches() {
			valueColor = mutedColor
		}
		paint.Text(x+7*scale, y+height-13*scale, layout.groupValue, 10*scale, valueColor)
		if len(layout.children) > 0 {
			child, pos := layout.children[0], layout.positions[0]
			childX, childY := x+pos.x*scale, y+pos.y*scale
			drawRailConnection(paint, x, mid, childX, childY+child.height*scale/2, scale, lineColor, false)
			drawRegexRailNode(paint, child, childX, childY, scale, lineColor, textColor, mutedColor, surfaceColor)
			drawRailConnection(paint, childX+child.width*scale, childY+child.height*scale/2, x+width, mid, scale, lineColor, false)
		} else {
			drawRailStraight(paint, x, mid, x+width, mid, 1.8*scale, lineColor)
		}
	case regexplayground.DiagramRepeat:
		if len(layout.children) == 0 {
			drawRailStraight(paint, x, mid, x+width, mid, 1.8*scale, lineColor)
			return
		}
		child, pos := layout.children[0], layout.positions[0]
		childX, childY := x+pos.x*scale, y+pos.y*scale
		childMid := childY + child.height*scale/2
		childRight := childX + child.width*scale
		drawRegexRailNode(paint, child, childX, childY, scale, lineColor, textColor, mutedColor, surfaceColor)
		hasLoop := layout.node.RepeatMax < 0 || layout.node.RepeatMax > 1
		labelWidth := regexRailTextWidth(layout.repeatLabel, 11)*scale + 10*scale
		labelRect := ui.Rect{X: x + (width-labelWidth)/2, Y: y + 1*scale, W: labelWidth, H: 17 * scale}
		paint.Fill(labelRect, surfaceColor, 4*scale)
		paint.Stroke(labelRect, lineColor.Alpha(0.35), 4*scale, 0.8*scale)
		paint.Text(labelRect.X+5*scale, labelRect.Y+3*scale, layout.repeatLabel, 11*scale, mutedColor)
		if hasLoop {
			left, right, top := childX-8*scale, childRight+8*scale, y+18*scale
			var loop ui.Path
			loop.MoveTo(right, childMid).CubeTo(right, top, left, top, left, childMid)
			paint.StrokePath(&loop, 1.8*scale, lineColor)
			drawRailArrowDown(paint, left, childMid, scale, lineColor)
		}
		if layout.node.RepeatMin == 0 {
			bottom := y + height - 3*scale
			var bypass ui.Path
			bypass.MoveTo(x+2*scale, mid).CubeTo(x+10*scale, mid, x+10*scale, bottom, x+18*scale, bottom)
			bypass.LineTo(x+width-18*scale, bottom).CubeTo(x+width-10*scale, bottom, x+width-10*scale, mid, x+width-2*scale, mid)
			paint.StrokePath(&bypass, 1.8*scale, lineColor)
			drawRailArrowRight(paint, x+width-2*scale, mid, scale, lineColor)
		}
		drawRailStraight(paint, x, mid, childX, childMid, 1.8*scale, lineColor)
		drawRailStraight(paint, childRight, childMid, x+width, mid, 1.8*scale, lineColor)
	case regexplayground.DiagramEmpty, regexplayground.DiagramEllipsis, regexplayground.DiagramTerminal:
		label := layout.node.Label
		if layout.node.Kind == regexplayground.DiagramEmpty {
			label = "ε"
		} else if layout.node.Kind == regexplayground.DiagramEllipsis {
			label = "…"
		}
		box := ui.Rect{X: x + 5*scale, Y: y + (height-22*scale)/2, W: max32(0, width-10*scale), H: 22 * scale}
		drawRailStraight(paint, x, mid, box.X, mid, 1.8*scale, lineColor)
		drawRailStraight(paint, box.X+box.W, mid, x+width, mid, 1.8*scale, lineColor)
		paint.Fill(box, surfaceColor, 5*scale)
		paint.Stroke(box, lineColor, 5*scale, 1*scale)
		textWidth := regexRailTextWidth(label, 11) * scale
		paint.Text(box.X+(box.W-textWidth)/2, box.Y+5*scale, label, 11*scale, textColor)
	}
}
func drawRegexRailSequenceConnections(paint *ui.Painter, layout regexRailLayout, x, y, mid, width, scale float32, color ui.Color) {
	if len(layout.children) == 0 {
		return
	}
	first, firstPos := layout.children[0], layout.positions[0]
	drawRailConnection(paint, x, mid, x+firstPos.x*scale, y+(firstPos.y+first.height/2)*scale, scale, color, false)
	for i, child := range layout.children[:len(layout.children)-1] {
		pos := layout.positions[i]
		next, nextPos := layout.children[i+1], layout.positions[i+1]
		drawRailConnection(paint,
			x+(pos.x+child.width)*scale, y+(pos.y+child.height/2)*scale,
			x+nextPos.x*scale, y+(nextPos.y+next.height/2)*scale,
			scale, color, true,
		)
	}
	last, lastPos := layout.children[len(layout.children)-1], layout.positions[len(layout.positions)-1]
	drawRailConnection(paint, x+(lastPos.x+last.width)*scale, y+(lastPos.y+last.height/2)*scale, x+width, mid, scale, color, false)
}

func drawRailConnection(paint *ui.Painter, x1, y1, x2, y2, scale float32, color ui.Color, arrow bool) {
	if abs32(y2-y1) < 0.5*scale {
		drawRailStraight(paint, x1, y1, x2, y2, 1.8*scale, color)
	} else {
		drawRailCurve(paint, x1, y1, x2, y2, scale, color, false)
	}
	if arrow {
		drawRailArrowRight(paint, x2, y2, scale, color)
	}
}

func drawRailCurve(paint *ui.Painter, x1, y1, x2, y2, scale float32, color ui.Color, arrow bool) {
	dx := max32(6*scale, (x2-x1)*0.48)
	var path ui.Path
	path.MoveTo(x1, y1).CubeTo(x1+dx, y1, x2-dx, y2, x2, y2)
	paint.StrokePath(&path, 1.8*scale, color)
	if arrow {
		drawRailArrowRight(paint, x2, y2, scale, color)
	}
}

func drawRailStraight(paint *ui.Painter, x1, y1, x2, y2, width float32, color ui.Color) {
	if x1 == x2 && y1 == y2 {
		return
	}
	var path ui.Path
	path.MoveTo(x1, y1).LineTo(x2, y2)
	paint.StrokePath(&path, width, color)
}

func drawRailJunction(paint *ui.Painter, x, y, scale float32, color ui.Color) {
	var dot ui.Path
	dot.Circle(x, y, 2.2*scale)
	paint.FillPath(&dot, color)
}

func drawRailArrowRight(paint *ui.Painter, x, y, scale float32, color ui.Color) {
	var arrow ui.Path
	arrow.MoveTo(x-4*scale, y-3*scale).LineTo(x, y).LineTo(x-4*scale, y+3*scale).Close()
	paint.FillPath(&arrow, color)
}

func drawRailArrowDown(paint *ui.Painter, x, y, scale float32, color ui.Color) {
	var arrow ui.Path
	arrow.MoveTo(x-3*scale, y-4*scale).LineTo(x, y).LineTo(x+3*scale, y-4*scale).Close()
	paint.FillPath(&arrow, color)
}

func regexCaptureDiagramValue(value string) string {
	runes := []rune(fmt.Sprintf("%q", value))
	if len(runes) > 17 {
		return string(runes[:16]) + "…"
	}
	return string(runes)
}

func regexRailTrim(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes-1]) + "…"
	}
	return value
}

func regexRailTextWidth(value string, size float32) float32 {
	return float32(utf8.RuneCountInString(value)) * size * 0.62
}

func max32(values ...float32) float32 {
	result := values[0]
	for _, value := range values[1:] {
		if value > result {
			result = value
		}
	}
	return result
}

func min32(values ...float32) float32 {
	result := values[0]
	for _, value := range values[1:] {
		if value < result {
			result = value
		}
	}
	return result
}

func abs32(value float32) float32 {
	if value < 0 {
		return -value
	}
	return value
}
