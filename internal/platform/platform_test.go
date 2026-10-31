package platform

import (
	"errors"
	"testing"
)

func TestNoDesktopShowsNothingAndAsksNothing(t *testing.T) {
	var d Desktop = NoDesktop{}
	if p, err := d.PickFolder(); p != "" || err != nil {
		t.Fatalf("PickFolder = %q, %v", p, err)
	}
	if p, err := d.PickNewFile("/w"); p != "" || err != nil {
		t.Fatalf("PickNewFile = %q, %v", p, err)
	}
	// Dialogs and the system's trash say they are not there, so the app
	// asks in its own window instead.
	if _, err := d.ConfirmDirty("1 file"); !errors.Is(err, ErrNoDialog) {
		t.Fatalf("ConfirmDirty = %v, want ErrNoDialog", err)
	}
	if err := d.TrashItem("/w/a"); !errors.Is(err, ErrNoDialog) {
		t.Fatalf("TrashItem = %v, want ErrNoDialog", err)
	}
	if err := d.CopyFiles("/w/a"); !errors.Is(err, ErrNoDialog) {
		t.Fatalf("CopyFiles = %v, want ErrNoDialog", err)
	}
	if d.OpenProjectWindow("/w") {
		t.Fatal("opened a window without a desktop")
	}
}

func TestSchedulerFuncRunsTheWork(t *testing.T) {
	ran := 0
	var s Scheduler = SchedulerFunc(func(fn func()) { ran++; fn() })
	called := false
	s.Post(func() { called = true })
	if !called || ran != 1 {
		t.Fatalf("ran %d times, called %v", ran, called)
	}
}
