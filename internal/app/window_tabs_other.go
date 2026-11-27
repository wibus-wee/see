//go:build !darwin

package app

import "github.com/egoist/mygo"

func configureProjectWindowTabs(*mygo.Window) {}

func canMergeProjectWindows([]*projectWindow, *projectWindow) bool { return false }

func mergeProjectWindows([]*projectWindow, *projectWindow) bool { return false }
