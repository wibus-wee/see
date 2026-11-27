package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"

	"see/locales"
)

type projectWindowTabGroup interface {
	windowsInTabGroup(window uintptr) []uintptr
	addWindowToTabGroup(anchor, window uintptr)
	selectWindow(window uintptr)
}

func projectWindowMenu(isDarwin, canMerge bool, merge func()) *mygo.MenuItem {
	if !isDarwin {
		return &mygo.MenuItem{Role: mygo.RoleWindowMenu}
	}

	return &mygo.MenuItem{
		Role: mygo.RoleWindowMenu,
		Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleMinimize},
			{Role: mygo.RoleZoom},
			mygo.Separator(),
			{
				Label:    locales.Catalog.Format(i18n.Locale(), "msgMergeAllWindows", nil),
				Disabled: !canMerge,
				Click: func(*mygo.MenuItem, *mygo.Window) {
					if canMerge && merge != nil {
						merge()
					}
				},
			},
			{Role: mygo.RoleFront},
		},
	}
}

func projectWindowIsEligible(project *projectWindow) bool {
	if project == nil || project.app == nil || project.win == nil || project.win.IsDestroyed() {
		return false
	}
	app := project.app
	return app.root != "" || len(app.docs) > 0 || app.layout != nil && app.hasTabs()
}

func projectWindowHandles(projects []*projectWindow) []uintptr {
	seen := make(map[uintptr]struct{}, len(projects))
	handles := make([]uintptr, 0, len(projects))
	for _, project := range projects {
		if !projectWindowIsEligible(project) {
			continue
		}
		handle := projectWindowHandle(project)
		if handle == 0 {
			continue
		}
		if _, ok := seen[handle]; ok {
			continue
		}
		seen[handle] = struct{}{}
		handles = append(handles, handle)
	}
	return handles
}

func projectWindowHandle(project *projectWindow) uintptr {
	if project == nil || project.win == nil || project.win.IsDestroyed() {
		return 0
	}
	window, ok := project.win.(interface{ NativeHandle() uintptr })
	if !ok {
		return 0
	}
	return window.NativeHandle()
}

func canMergeWindowHandles(windows []uintptr, active uintptr, tabs projectWindowTabGroup) bool {
	if len(windows) < 2 || tabs == nil {
		return false
	}
	anchor := projectWindowAnchor(windows, active)
	group := makeWindowSet(tabs.windowsInTabGroup(anchor))
	for _, window := range windows {
		if _, ok := group[window]; !ok {
			return true
		}
	}
	return false
}

func mergeWindowHandles(windows []uintptr, active uintptr, tabs projectWindowTabGroup) bool {
	if len(windows) < 2 || tabs == nil {
		return false
	}
	anchor := projectWindowAnchor(windows, active)
	group := makeWindowSet(tabs.windowsInTabGroup(anchor))
	merged := false
	for _, window := range windows {
		if window == anchor {
			continue
		}
		if _, ok := group[window]; ok {
			continue
		}
		tabs.addWindowToTabGroup(anchor, window)
		group[window] = struct{}{}
		merged = true
	}
	if merged {
		tabs.selectWindow(anchor)
	}
	return merged
}

func projectWindowAnchor(windows []uintptr, active uintptr) uintptr {
	for _, window := range windows {
		if window == active {
			return active
		}
	}
	if len(windows) == 0 {
		return 0
	}
	return windows[0]
}

func makeWindowSet(windows []uintptr) map[uintptr]struct{} {
	set := make(map[uintptr]struct{}, len(windows))
	for _, window := range windows {
		if window != 0 {
			set[window] = struct{}{}
		}
	}
	return set
}
