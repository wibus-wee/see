package app

import (
	"os"
	"testing"
	"time"

	"see/locales"
)

// The attention semantics the feedback layer guarantees: events erase
// themselves, errors stay until they are no longer true, and neither
// masks the other. The tests drive saveDoc, the one flow that produces
// both, plus the state itself where no flow is needed.

func TestSuccessFeedbackDrainsToAToast(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "a\n"}))
	d := h.open("a.txt")
	d.buf.Set("changed\n")

	if !h.a.saveDoc(d, false) {
		t.Fatal("save refused")
	}
	want := locales.MsgSaved() + d.name
	if !h.hasFeedback(want) {
		t.Fatalf("the save reported nothing: %v", h.feedbackTexts())
	}
	// The event is recorded until the view pass, which drains it into a
	// toast: before a frame nothing shows, after it the toast does.
	if h.tt.HasText(want) {
		t.Fatal("the toast showed before the view pass drained it")
	}
	h.tt.Frame()
	if len(h.a.feedback.events) != 0 {
		t.Fatal("the view pass did not drain the events")
	}
	if !h.tt.HasText(want) {
		t.Fatalf("the toast does not show: %q", h.tt.Texts())
	}
	// And it erases itself: a few seconds on, it is gone.
	h.tt.Advance(5 * time.Second)
	if h.tt.HasText(want) {
		t.Fatal("the toast did not erase itself")
	}
}

func TestErrorSurvivesSuccessEvents(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "a\n", "b.txt": "b\n"}))
	d := h.open("a.txt")
	d.buf.Set("changed\n")
	if err := os.Chmod(d.path, 0400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(d.path, 0600) })
	if h.a.saveDoc(d, false) {
		t.Fatal("saved a read-only file")
	}
	errmsg, isErr := h.feedbackError()
	if !isErr {
		t.Fatalf("the failed save reported no error: %v", h.feedbackTexts())
	}
	h.tt.Frame()
	if !h.tt.HasText(errmsg) {
		t.Fatalf("the error banner does not show: %q", h.tt.Texts())
	}

	// A success after it, of another file's save, neither overwrites nor
	// erases it: the banner carries both pieces of feedback apart.
	e := h.open("b.txt")
	e.buf.Set("changed b\n")
	if !h.a.saveDoc(e, false) {
		t.Fatal("second save refused")
	}
	h.tt.Frame()
	if !h.tt.HasText(errmsg) {
		t.Fatalf("a success overwrote the error: %q", h.tt.Texts())
	}
	if !h.tt.HasText(locales.MsgSaved() + e.name) {
		t.Fatal("the success did not toast")
	}
	if len(h.a.feedback.errors) != 1 || h.a.feedback.errors[0].msg != errmsg {
		t.Fatalf("the error state changed: %+v", h.a.feedback.errors)
	}
}

func TestErrorBannerStaysUntilAcknowledged(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "a\n"}))
	h.a.fail("first error")
	h.a.fail("second error")
	h.tt.Frame()
	if !h.tt.HasText("second error") {
		t.Fatal("the newest error does not show")
	}
	// The older one is not lost: the banner says errors wait behind it.
	if !h.tt.HasText("+1 more") {
		t.Fatalf("the count of errors waiting does not show: %q", h.tt.Texts())
	}

	// No time passes an error: it stays, where a toast would have gone.
	h.tt.Advance(10 * time.Second)
	h.tt.Frame()
	if !h.tt.HasText("second error") {
		t.Fatal("time dismissed the error")
	}

	// Dismissing the newest reveals the one it hid.
	if err := h.tt.Click(locales.Close()); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if !h.tt.HasText("first error") {
		t.Fatalf("dismiss did not reveal the error behind: %q", h.tt.Texts())
	}
	if err := h.tt.Click(locales.Close()); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if h.tt.HasText("first error") {
		t.Fatal("the dismissed error still shows")
	}
	if len(h.a.feedback.errors) != 0 {
		t.Fatalf("errors left: %+v", h.a.feedback.errors)
	}
}

func TestSaveErrorBannerRetriesAndResolves(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "a\n"}))
	d := h.open("a.txt")
	d.buf.Set("changed\n")
	if err := os.Chmod(d.path, 0400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(d.path, 0600) })
	if h.a.saveDoc(d, false) {
		t.Fatal("saved a read-only file")
	}
	latest := h.a.feedback.errors[len(h.a.feedback.errors)-1]
	if latest.action == nil || latest.action.label != locales.Retry() {
		t.Fatalf("the save error carries no Retry: %+v", latest)
	}
	h.tt.Frame()

	// Retry while the file is still read-only: the failure reports anew,
	// one error still, not a second copy.
	if err := h.tt.Click(locales.Retry()); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if len(h.a.feedback.errors) != 1 {
		t.Fatalf("the retried failure stacked: %+v", h.a.feedback.errors)
	}

	// The file writable again, the banner's Retry saves it, and the
	// success resolves the error without a dismissal.
	if err := os.Chmod(d.path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.Click(locales.Retry()); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if len(h.a.feedback.errors) != 0 {
		t.Fatalf("the successful retry did not resolve the error: %+v", h.a.feedback.errors)
	}
	on, err := os.ReadFile(d.path)
	if err != nil || string(on) != "changed\n" {
		t.Fatalf("the retry did not save: %q err=%v", on, err)
	}
	if d.buf.Modified() {
		t.Fatal("the retried save left the document dirty")
	}
	if !h.tt.HasText(locales.MsgSaved() + d.name) {
		t.Fatal("the retried save did not toast its success")
	}
}

func TestSaveConflictFeedbackYieldsToTheEditorBanner(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "a\n", "b.txt": "b\n"}))
	d := h.open("a.txt")
	d.buf.Set("changed\n")
	writeFile(t, h.a.root, "a.txt", "external\n")

	// The file shows: its editor banner already carries the conflict, so
	// the feedback one does not repeat it.
	if h.a.saveDoc(d, false) {
		t.Fatal("saved over the change on disk")
	}
	if len(h.a.feedback.errors) != 0 {
		t.Fatalf("the conflict double-reported: %+v", h.a.feedback.errors)
	}

	// Another file active, the conflicted one hidden behind a tab: the
	// user cannot see its banner, so feedback reports it with a Review
	// that goes to the file.
	h.open("b.txt")
	if h.a.saveDoc(d, false) {
		t.Fatal("saved over the change on disk")
	}
	if len(h.a.feedback.errors) != 1 {
		t.Fatalf("the hidden conflict did not report: %+v", h.a.feedback.errors)
	}
	latest := h.a.feedback.errors[0]
	if latest.action == nil || latest.action.label != locales.Review() {
		t.Fatalf("the conflict error carries no Review: %+v", latest)
	}
	h.tt.Frame()
	if err := h.tt.Click(locales.Review()); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if h.a.activePath() != d.path {
		t.Fatalf("Review did not go to the conflicted file: %q", h.a.activePath())
	}

	// Overwriting resolves it: the banner is gone without a dismissal.
	if !h.a.saveDoc(d, true) {
		t.Fatal("overwrite refused")
	}
	if len(h.a.feedback.errors) != 0 {
		t.Fatalf("the overwrite did not resolve the conflict feedback: %+v", h.a.feedback.errors)
	}
}

func TestEventsDoNotLeakAcrossWorkspaces(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "a\n"}))
	h.a.flash("an event")
	h.a.fail("an error")
	other := writeTree(t, map[string]string{"b.txt": "b\n"})
	h.a.openWorkspace(other)
	if len(h.a.feedback.events) != 0 || len(h.a.feedback.errors) != 0 {
		t.Fatalf("feedback crossed the workspace switch: %+v", h.a.feedback)
	}
}
