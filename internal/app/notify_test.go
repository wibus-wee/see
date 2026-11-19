package app

import (
	"testing"
	"time"

	"see/internal/tasks"
)

// The settings switches and the window's focus gate every notification
// path: these drive the seams, not a desktop.

func TestNotifyTaskDoneFollowsPrefsAndFocus(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.go": "package a\n"}))
	run := &taskRun{kind: tasks.Build, output: &tasks.Output{}, status: "taskSucceeded"}

	// Pref off, window unfocused: nothing.
	notifyFocusedOn = false
	h.a.notifyTaskDone(run)
	if len(notifySent) != 0 {
		t.Fatalf("notified with the switch off: %+v", notifySent)
	}

	// Pref on, window focused: nothing either.
	h.a.cfg.NotifyDone = true
	notifyFocusedOn = true
	h.a.notifyTaskDone(run)
	if len(notifySent) != 0 {
		t.Fatalf("notified a focused window: %+v", notifySent)
	}

	// Pref on, window unfocused.
	notifyFocusedOn = false
	h.a.notifyTaskDone(run)
	if len(notifySent) != 1 || notifySent[0].body == "" {
		t.Fatalf("task done did not notify: %+v", notifySent)
	}
	if !notifySent[0].silent {
		t.Fatal("sound played with NotifySound off")
	}

	// A failure needs its own switch; NotifyDone alone stays quiet.
	resetNotify()
	notifyFocusedOn = false
	failed := &taskRun{kind: tasks.Test, output: &tasks.Output{}, status: "taskFailed"}
	h.a.notifyTaskDone(failed)
	if len(notifySent) != 0 {
		t.Fatalf("failure notified under NotifyDone alone: %+v", notifySent)
	}
	h.a.cfg.NotifyFailed = true
	h.a.notifyTaskDone(failed)
	if len(notifySent) != 1 {
		t.Fatalf("failure did not notify under NotifyFailed: %+v", notifySent)
	}

	// A cancelled task never does.
	resetNotify()
	notifyFocusedOn = false
	h.a.notifyTaskDone(&taskRun{kind: tasks.Build, status: "taskCancelled"})
	if len(notifySent) != 0 {
		t.Fatalf("cancelled task notified: %+v", notifySent)
	}
}

func TestNotifyTerminalWaiting(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.go": "package a\n"}))
	h.a.cfg.NotifyWaiting = true

	// Focused: the bell the user can hear stays quiet.
	h.a.notifyTerminal("zsh", "")
	if len(notifySent) != 0 {
		t.Fatalf("bell notified a focused window: %+v", notifySent)
	}

	// Unfocused: the bell carries the terminal's title.
	notifyFocusedOn = false
	h.a.notifyTerminal("zsh", "")
	if len(notifySent) != 1 || notifySent[0].title != "zsh" {
		t.Fatalf("bell notification: %+v", notifySent)
	}

	// OSC 9 carries its own title and body.
	h.a.notifyTerminal("agent", "needs you")
	if notifySent[len(notifySent)-1].title != "agent" || notifySent[len(notifySent)-1].body != "needs you" {
		t.Fatalf("notification escape: %+v", notifySent[len(notifySent)-1])
	}

	// The switch off keeps escapes quiet too.
	h.a.cfg.NotifyWaiting = false
	h.a.notifyTerminal("agent", "needs you")
	if len(notifySent) != 2 {
		t.Fatalf("notified with the switch off: %+v", notifySent)
	}
}

func TestNotifyIncomingOncePerCount(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.go": "package a\n"}))
	h.a.cfg.DockBadge, h.a.cfg.BounceDock = true, true

	// Unfocused fetches badge and bounce at a new count, once.
	notifyFocusedOn = false
	h.a.notifyIncoming(2)
	if len(notifyBadges) != 1 || notifyBadges[0] != "●" || notifyBounces != 1 {
		t.Fatalf("first fetch: badges=%v bounces=%d", notifyBadges, notifyBounces)
	}
	h.a.notifyIncoming(2)
	if len(notifyBadges) != 1 || notifyBounces != 1 {
		t.Fatalf("same count announced again: badges=%v bounces=%d", notifyBadges, notifyBounces)
	}
	h.a.notifyIncoming(3)
	if len(notifyBadges) != 2 || notifyBounces != 2 {
		t.Fatalf("a grown count stayed quiet: badges=%v bounces=%d", notifyBadges, notifyBounces)
	}

	// Focus resets what was told, so the next fetch tells again; the
	// badge goes with the user's attention.
	h.a.clearNotifyIncoming()
	if len(notifyBadges) != 3 || notifyBadges[2] != "" {
		t.Fatalf("badge left after focus: %v", notifyBadges)
	}
	h.a.notifyIncoming(3)
	if len(notifyBadges) != 4 || notifyBadges[3] != "●" || notifyBounces != 3 {
		t.Fatalf("post-focus fetch stayed quiet: badges=%v bounces=%d", notifyBadges, notifyBounces)
	}
}

func TestNotifyIncomingWhileFocusedStaysQuiet(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.go": "package a\n"}))
	h.a.cfg.DockBadge, h.a.cfg.BounceDock = true, true

	// Focused: nothing shows, but the count is told, so unfocused
	// fetches at the same count stay quiet too.
	h.a.notifyIncoming(1)
	if len(notifyBadges) != 0 || notifyBounces != 0 {
		t.Fatalf("notified a focused window: badges=%v bounces=%d", notifyBadges, notifyBounces)
	}
	notifyFocusedOn = false
	h.a.notifyIncoming(1)
	if len(notifyBadges) != 0 {
		t.Fatalf("told again at the same count: %v", notifyBadges)
	}
}

func TestAutoFetchAnnouncesNewIncoming(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"})
	bare := t.TempDir()
	gitRun(t, bare, "init", "-q", "--bare")
	gitRun(t, root, "remote", "add", "origin", bare)
	gitRun(t, root, "push", "-qu", "origin", "main")
	h := newHarness(t, root)
	t.Cleanup(h.a.wsCancel)
	h.a.git.autoFetch = true
	h.a.cfg.DockBadge, h.a.cfg.BounceDock = true, true
	idle := func() bool { return !h.a.git.busy && !h.a.git.loading }
	h.readGit() // settles the repository; the interval is fresh
	h.a.git.lastFetch = time.Now().Add(-gitFetchInterval)

	// A background fetch finding nothing new announces nothing.
	h.readGit()
	h.settle(idle)
	if len(notifyBadges) != 0 {
		t.Fatalf("quiet fetch announced: %v", notifyBadges)
	}

	// Someone else pushes; the next background fetch finds it.
	other := t.TempDir()
	gitRun(t, other, "clone", "-q", "-b", "main", bare, ".")
	writeFile(t, other, "incoming.go", "package a\n")
	gitRun(t, other, "add", ".")
	gitRun(t, other, "commit", "-qm", "incoming work")
	gitRun(t, other, "push", "-q", "origin", "main")
	h.a.git.lastFetch = time.Now().Add(-gitFetchInterval)
	h.readGit()
	h.settle(idle)
	if h.a.git.behind != 1 {
		t.Fatalf("fetch found no incoming commit: behind=%d", h.a.git.behind)
	}
	// The test's window is focused: told, but nothing shown.
	if h.a.git.notifiedBehind != 1 || len(notifyBadges) != 0 || notifyBounces != 0 {
		t.Fatalf("focused fetch: notifiedBehind=%d badges=%v bounces=%d", h.a.git.notifiedBehind, notifyBadges, notifyBounces)
	}

	// The user's own fetch announces nothing: it clears the badge,
	// never re-announces the incoming count they see.
	h.a.git.notifiedBehind = 0
	h.a.gitFetch()
	h.settle(idle)
	if len(notifyBadges) != 1 || notifyBadges[0] != "" || notifyBounces != 0 {
		t.Fatalf("user fetch announced: badges=%v bounces=%d", notifyBadges, notifyBounces)
	}
}
