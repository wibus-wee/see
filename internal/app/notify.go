package app

import (
	"strings"

	"see/locales"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
)

// Desktop notifications, the Settings → Notifications switches wired up:
// a terminal asking for attention, a task that finished, and a fetch that
// found incoming commits. Everything below respects the window's focus:
// nothing fires while it has the keyboard.

// notifyFocused reports whether any of the app's windows has focus. It is
// mygo.FocusedWindow in the app; tests set the focus they want.
var notifyFocused = func() bool { return mygo.FocusedWindow() != nil }

// notifySend shows a notification; it is mygo.NewNotification + Show in
// the app, so tests see what would reach the desktop. Show runs off the
// UI thread: on macOS the first notification waits for the user's
// authorization answer.
var notifySend = func(title, body string, silent bool) {
	go func() {
		_ = mygo.NewNotification(mygo.NotificationOptions{Title: title, Body: body, Silent: silent}).Show()
	}()
}

// notifyDockBadge and notifyDockBounce are the Dock icon's attention
// requests, mygo.App.Dock's in the app; tests record them.
var (
	notifyDockBadge  = func(text string) { mygo.App.Dock.SetBadge(text) }
	notifyDockBounce = func() { mygo.App.Dock.Bounce(false) }
)

// notify shows a notification, and bounces the Dock icon when the user
// asked for it, while the window is unfocused.
func (a *App) notify(title, body string) {
	if notifyFocused() {
		return
	}
	notifySend(title, body, !a.cfg.NotifySound)
	if a.cfg.BounceDock {
		notifyDockBounce()
	}
}

// notifyTaskDone reports a finished build or test, the NotifyDone and
// NotifyFailed switches picking which results reach the desktop.
func (a *App) notifyTaskDone(run *taskRun) {
	if run.status == "taskCancelled" {
		return
	}
	if run.status == "taskFailed" && !a.cfg.NotifyFailed ||
		run.status == "taskSucceeded" && !a.cfg.NotifyDone {
		return
	}
	body := "go " + strings.Join(run.kind.Args(), " ") + " — " + taskMessage(run.status)
	a.notify("See", body)
}

// notifyTerminal reports a terminal's bell or notification escape, under
// NotifyWaiting.
func (a *App) notifyTerminal(title, body string) {
	if !a.cfg.NotifyWaiting {
		return
	}
	if title == "" {
		title = "See"
	}
	if body == "" {
		body = notifyMessage("notifyTerminalBell", nil)
	}
	a.notify(title, body)
}

// notifyIncoming announces commits a background fetch discovered: a
// badge and a bounce of the Dock icon as their switches say, while the
// window is unfocused. notifiedBehind is the count already told, so
// fetches finding nothing new stay quiet; the window's focus, or the
// count coming back down, resets it.
func (a *App) notifyIncoming(behind int) {
	if behind <= a.git.notifiedBehind {
		return
	}
	a.git.notifiedBehind = behind
	if notifyFocused() {
		return
	}
	if a.cfg.DockBadge {
		notifyDockBadge("●")
	}
	if a.cfg.BounceDock {
		notifyDockBounce()
	}
}

// clearNotifyIncoming drops the badge incoming commits left once the
// window has the user's attention, and lets a later fetch tell again.
func (a *App) clearNotifyIncoming() {
	a.git.notifiedBehind = 0
	notifyDockBadge("")
}

func notifyMessage(key string, args map[string]any) string {
	return locales.Catalog.Format(i18n.Locale(), key, args)
}
