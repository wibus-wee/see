package app

import (
	"math"

	"github.com/egoist/mygo/ui"
)

const (
	regexToolDefaultWidth      = float32(680)
	regexToolDefaultHeight     = float32(420)
	regexToolMinWidth          = float32(440)
	regexToolMinHeight         = float32(280)
	regexToolMargin            = float32(12)
	regexToolDividerHeight     = float32(8)
	regexToolDefaultDockHeight = float32(260)
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

func clampRegexToolRect(rect, bounds ui.Rect) ui.Rect {
	bounds = regexToolBoundsSanitized(bounds)
	marginX, marginY := min(regexToolMargin, bounds.W/2), min(regexToolMargin, bounds.H/2)
	maxW, maxH := max(float32(0), bounds.W-2*marginX), max(float32(0), bounds.H-2*marginY)
	minW, minH := min(regexToolMinWidth, maxW), min(regexToolMinHeight, maxH)
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
	bounds = regexToolBoundsSanitized(bounds)
	rect = clampRegexToolRect(rect, bounds)
	if !finiteRegexGeometry(dx) {
		dx = 0
	}
	if !finiteRegexGeometry(dy) {
		dy = 0
	}
	marginX, marginY := min(regexToolMargin, bounds.W/2), min(regexToolMargin, bounds.H/2)
	minW := min(regexToolMinWidth, max(float32(0), bounds.W-2*marginX))
	minH := min(regexToolMinHeight, max(float32(0), bounds.H-2*marginY))
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
	return clampRegexToolRect(ui.Rect{X: left, Y: top, W: right - left, H: bottom - top}, bounds)
}

func clampRegexToolDockHeight(height, availableHeight float32) float32 {
	if !finiteRegexGeometry(availableHeight) || availableHeight < 0 {
		availableHeight = 0
	}
	if !finiteRegexGeometry(height) || height <= 0 {
		height = regexToolDefaultDockHeight
	}
	usableEditor := min(float32(160), availableHeight*0.4)
	maxDock := max(float32(0), availableHeight-usableEditor)
	minDock := min(float32(180), maxDock)
	return min(max(height, minDock), maxDock)
}
