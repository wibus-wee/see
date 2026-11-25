package app

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestRegexToolRectClampsInvalidAndOffscreenGeometry(t *testing.T) {
	bounds := ui.Rect{W: 1000, H: 800}
	got := clampRegexToolRect(ui.Rect{X: -900, Y: 9000, W: 2000, H: 900}, bounds)
	if got.X != regexToolMargin || got.Y != regexToolMargin || got.W != 976 || got.H != 776 {
		t.Fatalf("clamped offscreen rect = %+v", got)
	}

	got = clampRegexToolRect(ui.Rect{X: float32(math.NaN()), Y: float32(math.Inf(1)), W: float32(math.NaN()), H: 0}, bounds)
	if got != (ui.Rect{X: 160, Y: 190, W: regexToolDefaultWidth, H: regexToolDefaultHeight}) {
		t.Fatalf("invalid rect did not return to centered defaults: %+v", got)
	}

	tiny := clampRegexToolRect(ui.Rect{}, ui.Rect{W: 300, H: 200})
	if tiny.W != 276 || tiny.H != 176 || tiny.X != 12 || tiny.Y != 12 {
		t.Fatalf("small workspace did not shrink effective minimum: %+v", tiny)
	}
}

func TestRegexToolResizeKeepsOppositeEdgesAnchored(t *testing.T) {
	bounds := ui.Rect{W: 1000, H: 800}
	start := ui.Rect{X: 120, Y: 100, W: 680, H: 420}
	tests := []struct {
		name   string
		edge   regexToolResizeEdge
		dx, dy float32
		keep   func(ui.Rect) bool
	}{
		{"left", regexToolResizeLeft, 30, 0, func(r ui.Rect) bool { return r.X+r.W == start.X+start.W }},
		{"right", regexToolResizeRight, 30, 0, func(r ui.Rect) bool { return r.X == start.X }},
		{"top", regexToolResizeTop, 0, 30, func(r ui.Rect) bool { return r.Y+r.H == start.Y+start.H }},
		{"bottom", regexToolResizeBottom, 0, 30, func(r ui.Rect) bool { return r.Y == start.Y }},
		{"north-west", regexToolResizeTop | regexToolResizeLeft, 30, 30, func(r ui.Rect) bool { return r.X+r.W == start.X+start.W && r.Y+r.H == start.Y+start.H }},
		{"north-east", regexToolResizeTop | regexToolResizeRight, 30, 30, func(r ui.Rect) bool { return r.X == start.X && r.Y+r.H == start.Y+start.H }},
		{"south-west", regexToolResizeBottom | regexToolResizeLeft, 30, 30, func(r ui.Rect) bool { return r.X+r.W == start.X+start.W && r.Y == start.Y }},
		{"south-east", regexToolResizeBottom | regexToolResizeRight, 30, 30, func(r ui.Rect) bool { return r.X == start.X && r.Y == start.Y }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := resizeRegexToolRect(start, test.edge, test.dx, test.dy, bounds)
			if !test.keep(got) {
				t.Fatalf("opposite edge moved: start=%+v got=%+v", start, got)
			}
			if got.W < regexToolMinWidth || got.H < regexToolMinHeight || got.X < regexToolMargin || got.Y < regexToolMargin || got.X+got.W > bounds.W-regexToolMargin || got.Y+got.H > bounds.H-regexToolMargin {
				t.Fatalf("resize escaped bounds or minimum: %+v", got)
			}
		})
	}
}

func TestRegexToolResizeClampsMinimumAndWorkspaceEdges(t *testing.T) {
	bounds := ui.Rect{W: 1000, H: 800}
	start := ui.Rect{X: 120, Y: 100, W: 680, H: 420}
	got := resizeRegexToolRect(start, regexToolResizeLeft|regexToolResizeTop, 1000, 1000, bounds)
	if got.W != regexToolMinWidth || got.H != regexToolMinHeight || got.X+got.W != start.X+start.W || got.Y+got.H != start.Y+start.H {
		t.Fatalf("resize did not preserve anchors at minimum: %+v", got)
	}
	got = resizeRegexToolRect(start, regexToolResizeRight|regexToolResizeBottom, 1000, 1000, bounds)
	if got.X != start.X || got.Y != start.Y || got.X+got.W != bounds.W-regexToolMargin || got.Y+got.H != bounds.H-regexToolMargin {
		t.Fatalf("resize did not clamp at workspace edges: %+v", got)
	}
	got = resizeRegexToolRect(ui.Rect{W: 260, H: 150}, regexToolResizeLeft|regexToolResizeTop, -100, -100, ui.Rect{W: 300, H: 200})
	if got.W != 276 || got.H != 176 || got.X != 12 || got.Y != 12 {
		t.Fatalf("effective resize minimum did not shrink for tiny workspace: %+v", got)
	}
	got = resizeRegexToolRect(start, regexToolResizeRight|regexToolResizeBottom, float32(math.NaN()), float32(math.Inf(1)), ui.Rect{X: float32(math.NaN()), Y: float32(math.Inf(1)), W: 300, H: 200})
	if got != (ui.Rect{X: 12, Y: 12, W: 276, H: 176}) {
		t.Fatalf("resize did not normalize invalid inputs: %+v", got)
	}
}

func TestRegexToolDockHeightLeavesUsableEditorArea(t *testing.T) {
	if got := clampRegexToolDockHeight(900, 600); got != 440 {
		t.Fatalf("dock height = %v, want 440", got)
	}
	if got := clampRegexToolDockHeight(1, 200); got != 120 {
		t.Fatalf("tiny dock height = %v, want 120", got)
	}
	if got := clampRegexToolDockHeight(200, float32(math.NaN())); got != 0 {
		t.Fatalf("invalid available height = %v, want 0", got)
	}
}
