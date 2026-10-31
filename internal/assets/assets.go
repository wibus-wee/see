// Package assets bundles the images the app draws but does not generate:
// its own icon, which the welcome window shows.
//
// The packaging icon stays at the repository root, where the mygo tool
// looks for resources/icon.png.
package assets

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
)

//go:embed resources/icon.svg
var iconSVG []byte

// Icon is the app's icon, drawn in the welcome window.
var Icon = ui.MustParseSVG(iconSVG)
