//go:build darwin

package app

import (
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

const (
	projectWindowTabbingIdentifier = "com.wibus-wee.see.project"
	nsWindowTabbingModeAutomatic   = uintptr(0)
	nsWindowAbove                  = uintptr(1)
)

func configureProjectWindowTabs(win *mygo.Window) {
	if win == nil || win.IsDestroyed() {
		return
	}
	handle := win.NativeHandle()
	if handle == 0 || symbolRuntime() == 0 {
		return
	}

	identifierBytes := append([]byte(projectWindowTabbingIdentifier), 0)
	identifier := windowTabsMessage(uintptr(objc.GetClass("NSString")), "stringWithUTF8String:", uintptr(unsafe.Pointer(&identifierBytes[0])))
	runtime.KeepAlive(identifierBytes)
	if identifier == 0 {
		return
	}
	windowTabsMessage(handle, "setTabbingIdentifier:", identifier)
	windowTabsMessage(handle, "setTabbingMode:", nsWindowTabbingModeAutomatic)
}

func canMergeProjectWindows(projects []*projectWindow, active *projectWindow) bool {
	windows := projectWindowHandles(projects)
	if len(windows) < 2 {
		return false
	}
	anchor := projectWindowHandle(active)
	if !containsWindow(windows, anchor) {
		anchor = windows[0]
	}
	group := windowTabGroup(anchor)
	for _, handle := range windows {
		if _, ok := group[handle]; !ok {
			return true
		}
	}
	return false
}

func mergeProjectWindows(projects []*projectWindow, active *projectWindow) bool {
	windows := projectWindowHandles(projects)
	if len(windows) < 2 {
		return false
	}
	anchor := projectWindowHandle(active)
	if !containsWindow(windows, anchor) {
		anchor = windows[0]
	}

	merged := false
	for _, handle := range windows {
		if handle == anchor {
			continue
		}
		if _, ok := windowTabGroup(anchor)[handle]; ok {
			continue
		}
		windowTabsMessage(anchor, "addTabbedWindow:ordered:", handle, nsWindowAbove)
		merged = true
	}
	if merged {
		windowTabsMessage(anchor, "makeKeyAndOrderFront:", 0)
	}
	return merged
}

func projectWindowHandles(projects []*projectWindow) []uintptr {
	seen := make(map[uintptr]struct{}, len(projects))
	handles := make([]uintptr, 0, len(projects))
	for _, project := range projects {
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

func containsWindow(windows []uintptr, target uintptr) bool {
	for _, window := range windows {
		if window == target {
			return true
		}
	}
	return false
}

func windowTabGroup(window uintptr) map[uintptr]struct{} {
	tabs := windowTabsMessage(window, "tabbedWindows")
	if tabs == 0 {
		return nil
	}
	count := windowTabsMessage(tabs, "count")
	group := make(map[uintptr]struct{}, count)
	for i := uintptr(0); i < count; i++ {
		if handle := windowTabsMessage(tabs, "objectAtIndex:", i); handle != 0 {
			group[handle] = struct{}{}
		}
	}
	return group
}

func windowTabsMessage(receiver uintptr, selector string, args ...uintptr) uintptr {
	message := symbolRuntime()
	if message == 0 || receiver == 0 {
		return 0
	}
	argv := []uintptr{receiver, uintptr(objc.RegisterName(selector))}
	argv = append(argv, args...)
	result, _, _ := purego.SyscallN(message, argv...)
	return result
}
