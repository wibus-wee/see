//go:build darwin

package app

import (
	"fmt"
	"runtime"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui/editor"
)

type appKitTestPoint struct{ X, Y float64 }
type appKitTestSize struct{ Width, Height float64 }
type appKitTestRect struct {
	Origin appKitTestPoint
	Size   appKitTestSize
}

type appKitTestWindow struct{ handle uintptr }

func (w *appKitTestWindow) Show()                     {}
func (w *appKitTestWindow) Focus()                    {}
func (w *appKitTestWindow) Update(func())             {}
func (w *appKitTestWindow) IsDestroyed() bool         { return false }
func (w *appKitTestWindow) NativeHandle() uintptr     { return w.handle }
func (w *appKitTestWindow) SetVibrancy(mygo.Vibrancy) {}

func TestMergeProjectWindowsUsesAppKitTabGroup(t *testing.T) {
	bufferA := editor.NewBuffer("saved a")
	bufferA.Set("unsaved a")
	bufferB := editor.NewBuffer("saved b")
	bufferB.Set("unsaved b")
	docA := &doc{path: "/a/file.txt", buf: bufferA}
	docB := &doc{path: "/b/file.txt", buf: bufferB}
	appA := &App{root: "/a", docs: map[string]*doc{docA.path: docA}}
	appB := &App{root: "/b", docs: map[string]*doc{docB.path: docB}}
	beforeTextA, beforeVersionA := bufferA.Snapshot().String(), bufferA.Version()
	beforeTextB, beforeVersionB := bufferB.Snapshot().String(), bufferB.Version()
	beforeModifiedA, beforeModifiedB := bufferA.Modified(), bufferB.Modified()

	var projectA, projectB *projectWindow
	err := runOnAppKitTestMainThread(func() error {
		fixture, err := newAppKitTestWindows()
		if err != nil {
			return err
		}
		defer fixture.Close()

		projectA = &projectWindow{app: appA, win: &appKitTestWindow{handle: fixture.projects[0]}}
		projectB = &projectWindow{app: appB, win: &appKitTestWindow{handle: fixture.projects[1]}}
		auxiliary := &projectWindow{app: &App{showWelcome: true}, win: &appKitTestWindow{handle: fixture.auxiliary}}
		projects := []*projectWindow{projectA, projectB, auxiliary}
		identifier := appKitTestString(projectWindowTabbingIdentifier)
		for _, window := range fixture.projects {
			configureNativeProjectWindowTabs(window)
		}
		for _, window := range fixture.projects {
			if windowTabsMessage(window, "tabbingMode") != nsWindowTabbingModeAutomatic {
				return fmt.Errorf("project window %x did not enable automatic tab grouping", window)
			}
			windowIdentifier := windowTabsMessage(window, "tabbingIdentifier")
			if windowTabsMessage(windowIdentifier, "isEqualToString:", identifier) == 0 {
				return fmt.Errorf("project window %x did not receive the shared tabbing identifier", window)
			}
		}
		if len(projectWindowHandles(projects)) != 2 {
			return fmt.Errorf("project eligibility included auxiliary window %x", fixture.auxiliary)
		}
		windowTabsMessage(fixture.projects[0], "orderFront:", 0)
		windowTabsMessage(fixture.projects[1], "makeKeyAndOrderFront:", 0)
		if !canMergeProjectWindows(projects, projectB) {
			return fmt.Errorf("separate native project windows were not mergeable before the merge")
		}
		if !mergeProjectWindows(projects, projectB) {
			return fmt.Errorf("production merge bridge did not merge native project windows")
		}

		members := appKitWindowTabGroup(fixture.projects[1])
		if !containsWindowHandle(members, fixture.projects[0]) || !containsWindowHandle(members, fixture.projects[1]) {
			return fmt.Errorf("native tab group %v does not contain both project windows %v", members, fixture.projects)
		}
		if containsWindowHandle(members, fixture.auxiliary) {
			return fmt.Errorf("auxiliary window %x joined project tab group %v", fixture.auxiliary, members)
		}
		tabGroup := windowTabsMessage(fixture.projects[1], "tabGroup")
		if selected := windowTabsMessage(tabGroup, "selectedWindow"); selected != fixture.projects[1] {
			return fmt.Errorf("selected native tab = %x, want active project window %x", selected, fixture.projects[1])
		}
		if canMergeProjectWindows(projects, projectB) {
			return fmt.Errorf("Merge All Windows remained available after both projects joined one group")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if projectA.app != appA || projectB.app != appB || appA.docs[docA.path] != docA || appB.docs[docB.path] != docB {
		t.Fatal("native merge changed project app or document state")
	}
	if !bufferA.Modified() || !bufferB.Modified() || bufferA.Modified() != beforeModifiedA || bufferB.Modified() != beforeModifiedB || bufferA.Snapshot().String() != beforeTextA || bufferB.Snapshot().String() != beforeTextB || bufferA.Version() != beforeVersionA || bufferB.Version() != beforeVersionB {
		t.Fatal("native merge changed a project's dirty buffer state")
	}
}

type appKitTestWindows struct {
	projects           [2]uintptr
	auxiliary          uintptr
	all                []uintptr
	pool               uintptr
	application        uintptr
	nsWindowClass      uintptr
	previousAutomatic  uintptr
	previousActivation uintptr
}

func newAppKitTestWindows() (*appKitTestWindows, error) {
	fixture := &appKitTestWindows{}
	fixture.pool = windowTabsMessage(uintptr(objc.GetClass("NSAutoreleasePool")), "alloc")
	fixture.pool = windowTabsMessage(fixture.pool, "init")
	if fixture.pool == 0 {
		return nil, fmt.Errorf("could not create AppKit autorelease pool")
	}
	fail := func(err error) (*appKitTestWindows, error) {
		fixture.Close()
		return nil, err
	}
	fixture.application = windowTabsMessage(uintptr(objc.GetClass("NSApplication")), "sharedApplication")
	if fixture.application == 0 {
		return fail(fmt.Errorf("could not create NSApplication"))
	}
	fixture.nsWindowClass = uintptr(objc.GetClass("NSWindow"))
	if fixture.nsWindowClass == 0 {
		return fail(fmt.Errorf("could not load NSWindow class"))
	}
	fixture.previousAutomatic = windowTabsMessage(fixture.nsWindowClass, "allowsAutomaticWindowTabbing")
	fixture.previousActivation = windowTabsMessage(fixture.application, "activationPolicy")
	windowTabsMessage(fixture.nsWindowClass, "setAllowsAutomaticWindowTabbing:", 0)
	windowTabsMessage(fixture.application, "setActivationPolicy:", 0)
	windowTabsMessage(fixture.application, "activateIgnoringOtherApps:", 1)

	for range 3 {
		window, err := newAppKitTestWindow()
		if err != nil {
			return fail(err)
		}
		fixture.all = append(fixture.all, window)
	}
	fixture.projects = [2]uintptr{fixture.all[0], fixture.all[1]}
	fixture.auxiliary = fixture.all[2]
	windowTabsMessage(fixture.auxiliary, "setTabbingIdentifier:", appKitTestString("com.wibus-wee.see.utility"))
	return fixture, nil
}

func (fixture *appKitTestWindows) Close() {
	closeAppKitTestWindows(fixture.all)
	if fixture.nsWindowClass != 0 {
		windowTabsMessage(fixture.nsWindowClass, "setAllowsAutomaticWindowTabbing:", fixture.previousAutomatic)
	}
	if fixture.application != 0 {
		windowTabsMessage(fixture.application, "setActivationPolicy:", fixture.previousActivation)
	}
	if fixture.pool != 0 {
		windowTabsMessage(fixture.pool, "drain")
	}
}

func newAppKitTestWindow() (uintptr, error) {
	message := symbolRuntime()
	if message == 0 {
		return 0, fmt.Errorf("could not load objc_msgSend")
	}
	var initWindow func(objc.ID, objc.SEL, appKitTestRect, uint, uint, bool) objc.ID
	purego.RegisterFunc(&initWindow, message)
	class := objc.GetClass("NSWindow")
	if class == 0 {
		return 0, fmt.Errorf("could not load NSWindow class")
	}
	allocated := windowTabsMessage(uintptr(class), "alloc")
	window := initWindow(objc.ID(allocated), objc.RegisterName("initWithContentRect:styleMask:backing:defer:"), appKitTestRect{Size: appKitTestSize{Width: 320, Height: 240}}, 15, 2, false)
	if window == 0 {
		windowTabsMessage(allocated, "release")
		return 0, fmt.Errorf("could not create NSWindow")
	}
	windowTabsMessage(uintptr(window), "setReleasedWhenClosed:", 0)
	return uintptr(window), nil
}

func closeAppKitTestWindows(windows []uintptr) {
	for _, window := range windows {
		if window == 0 {
			continue
		}
		windowTabsMessage(window, "orderOut:", 0)
		windowTabsMessage(window, "close")
		windowTabsMessage(window, "release")
	}
}

func appKitTestString(value string) uintptr {
	bytes := append([]byte(value), 0)
	result := windowTabsMessage(uintptr(objc.GetClass("NSString")), "stringWithUTF8String:", uintptr(unsafe.Pointer(&bytes[0])))
	runtime.KeepAlive(bytes)
	return result
}

func containsWindowHandle(windows []uintptr, target uintptr) bool {
	for _, window := range windows {
		if window == target {
			return true
		}
	}
	return false
}
