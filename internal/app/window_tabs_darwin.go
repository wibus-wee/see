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

type appKitProjectWindowTabGroup struct{}

func canMergeProjectWindows(projects []*projectWindow, active *projectWindow) bool {
	return canMergeWindowHandles(projectWindowHandles(projects), projectWindowHandle(active), appKitProjectWindowTabGroup{})
}

func mergeProjectWindows(projects []*projectWindow, active *projectWindow) bool {
	return mergeWindowHandles(projectWindowHandles(projects), projectWindowHandle(active), appKitProjectWindowTabGroup{})
}

func (appKitProjectWindowTabGroup) windowsInTabGroup(window uintptr) []uintptr {
	return appKitWindowTabGroup(window)
}

func (appKitProjectWindowTabGroup) addWindowToTabGroup(anchor, window uintptr) {
	windowTabsMessage(anchor, "addTabbedWindow:ordered:", window, nsWindowAbove)
}

func (appKitProjectWindowTabGroup) selectWindow(window uintptr) {
	windowTabsMessage(window, "makeKeyAndOrderFront:", 0)
}

func appKitWindowTabGroup(window uintptr) []uintptr {
	if window == 0 {
		return nil
	}
	tabGroupSelector := uintptr(objc.RegisterName("tabGroup"))
	if windowTabsMessage(window, "respondsToSelector:", tabGroupSelector) != 0 {
		if group := windowTabsMessage(window, "tabGroup"); group != 0 {
			if windows := windowTabsMessage(group, "windows"); windows != 0 {
				return appKitWindowsArray(windows)
			}
		}
	}
	if windows := windowTabsMessage(window, "tabbedWindows"); windows != 0 {
		return appKitWindowsArray(windows)
	}
	return nil
}

func appKitWindowsArray(windows uintptr) []uintptr {
	count := windowTabsMessage(windows, "count")
	tabs := make([]uintptr, 0, count)
	for i := uintptr(0); i < count; i++ {
		if handle := windowTabsMessage(windows, "objectAtIndex:", i); handle != 0 {
			tabs = append(tabs, handle)
		}
	}
	return tabs
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
