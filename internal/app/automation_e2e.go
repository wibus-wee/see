//go:build e2e

// The automation plugin drives the built app from another process, as
// the screenshot capture does; it listens only with MYGO_AUTOMATION set
// and release builds strip it with the inspector.
package app

import _ "github.com/egoist/mygo/plugins/automation"
