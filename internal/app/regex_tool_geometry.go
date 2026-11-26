package app

import (
	"math"

	"github.com/egoist/mygo/ui"
)

const (
	regexToolDefaultWidth  = float32(680)
	regexToolDefaultHeight = float32(420)
	regexToolMinWidth      = float32(440)
	regexToolMinHeight     = float32(280)
	regexToolMargin        = float32(12)
)

type regexToolResizeEdge uint8

const (
	regexToolResizeLeft regexToolResizeEdge = 1 << iota
	regexToolResizeRight
	regexToolResizeTop
	regexToolResizeBottom
)

func finiteRegexGeometry(v float32) bool {
	return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
}

func regexToolDefaultRect(bounds ui.Rect) ui.Rect {
	return clampRegexToolRect(ui.Rect{
		X: bounds.X + (bounds.W-regexToolDefaultWidth)/2,
		Y: bounds.Y + (bounds.H-regexToolDefaultHeight)/2,
		W: regexToolDefaultWidth,
		H: regexToolDefaultHeight,
	}, bounds)
}

func regexToolBoundsSanitized(bounds ui.Rect) ui.Rect {
	if !finiteRegexGeometry(bounds.X) || !finiteRegexGeometry(bounds.Y) {
		bounds.X, bounds.Y = 0, 0
	}
	if !finiteRegexGeometry(bounds.W) || bounds.W < 0 {
		bounds.W = 0
	}
	if !finiteRegexGeometry(bounds.H) || bounds.H < 0 {
		bounds.H = 0
	}
	return bounds
}

func regexToolBoundsWithinViewport(bounds ui.Rect, width, height float32) ui.Rect {
	bounds = regexToolBoundsSanitized(bounds)
	if !finiteRegexGeometry(width) || width < 0 {
		width = 0
	}
	if !finiteRegexGeometry(height) || height < 0 {
		height = 0
	}
	x := min(max(bounds.X, 0), width)
	y := min(max(bounds.Y, 0), height)
	right := min(bounds.X+bounds.W, width)
	bottom := min(bounds.Y+bounds.H, height)
	return ui.Rect{X: x, Y: y, W: max(float32(0), right-x), H: max(float32(0), bottom-y)}
}

func regexToolMinimumSize(bounds, sourceBounds ui.Rect) (float32, float32) {
	bounds, sourceBounds = regexToolBoundsSanitized(bounds), regexToolBoundsSanitized(sourceBounds)
	marginX, marginY := min(regexToolMargin, bounds.W/2), min(regexToolMargin, bounds.H/2)
	sourceMarginX, sourceMarginY := min(regexToolMargin, sourceBounds.W/2), min(regexToolMargin, sourceBounds.H/2)
	maxW, maxH := max(float32(0), bounds.W-2*marginX), max(float32(0), bounds.H-2*marginY)
	sourceMaxW, sourceMaxH := max(float32(0), sourceBounds.W-2*sourceMarginX), max(float32(0), sourceBounds.H-2*sourceMarginY)
	return min(regexToolMinWidth, maxW, sourceMaxW), min(regexToolMinHeight, maxH, sourceMaxH)
}

func clampRegexToolRect(rect, bounds ui.Rect) ui.Rect {
	return clampRegexToolRectWithMinimum(rect, bounds, regexToolMinWidth, regexToolMinHeight)
}

func clampRegexToolRectWithMinimum(rect, bounds ui.Rect, minWidth, minHeight float32) ui.Rect {
	bounds = regexToolBoundsSanitized(bounds)
	marginX, marginY := min(regexToolMargin, bounds.W/2), min(regexToolMargin, bounds.H/2)
	maxW, maxH := max(float32(0), bounds.W-2*marginX), max(float32(0), bounds.H-2*marginY)
	if !finiteRegexGeometry(minWidth) || minWidth < 0 {
		minWidth = regexToolMinWidth
	}
	if !finiteRegexGeometry(minHeight) || minHeight < 0 {
		minHeight = regexToolMinHeight
	}
	minW, minH := min(minWidth, maxW), min(minHeight, maxH)
	if !finiteRegexGeometry(rect.W) || rect.W <= 0 {
		rect.W = regexToolDefaultWidth
	}
	if !finiteRegexGeometry(rect.H) || rect.H <= 0 {
		rect.H = regexToolDefaultHeight
	}
	rect.W, rect.H = min(max(rect.W, minW), maxW), min(max(rect.H, minH), maxH)
	if !finiteRegexGeometry(rect.X) {
		rect.X = bounds.X + (bounds.W-rect.W)/2
	}
	if !finiteRegexGeometry(rect.Y) {
		rect.Y = bounds.Y + (bounds.H-rect.H)/2
	}
	rect.X = min(max(rect.X, bounds.X+marginX), bounds.X+bounds.W-marginX-rect.W)
	rect.Y = min(max(rect.Y, bounds.Y+marginY), bounds.Y+bounds.H-marginY-rect.H)
	return rect
}

func resizeRegexToolRect(rect ui.Rect, edge regexToolResizeEdge, dx, dy float32, bounds ui.Rect) ui.Rect {
	return resizeRegexToolRectWithMinimum(rect, edge, dx, dy, bounds, regexToolMinWidth, regexToolMinHeight)
}

func resizeRegexToolRectWithMinimum(rect ui.Rect, edge regexToolResizeEdge, dx, dy float32, bounds ui.Rect, minWidth, minHeight float32) ui.Rect {
	bounds = regexToolBoundsSanitized(bounds)
	rect = clampRegexToolRectWithMinimum(rect, bounds, minWidth, minHeight)
	if !finiteRegexGeometry(dx) {
		dx = 0
	}
	if !finiteRegexGeometry(dy) {
		dy = 0
	}
	marginX, marginY := min(regexToolMargin, bounds.W/2), min(regexToolMargin, bounds.H/2)
	minW := min(minWidth, max(float32(0), bounds.W-2*marginX))
	minH := min(minHeight, max(float32(0), bounds.H-2*marginY))
	left, right := rect.X, rect.X+rect.W
	top, bottom := rect.Y, rect.Y+rect.H
	if edge&regexToolResizeLeft != 0 {
		left = min(max(left+dx, bounds.X+marginX), right-minW)
	}
	if edge&regexToolResizeRight != 0 {
		right = max(min(right+dx, bounds.X+bounds.W-marginX), left+minW)
	}
	if edge&regexToolResizeTop != 0 {
		top = min(max(top+dy, bounds.Y+marginY), bottom-minH)
	}
	if edge&regexToolResizeBottom != 0 {
		bottom = max(min(bottom+dy, bounds.Y+bounds.H-marginY), top+minH)
	}
	return clampRegexToolRectWithMinimum(ui.Rect{X: left, Y: top, W: right - left, H: bottom - top}, bounds, minWidth, minHeight)
}
