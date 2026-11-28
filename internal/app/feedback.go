package app

import (
	"slices"

	"github.com/egoist/mygo/ui"

	"see/locales"
)

// Feedback to the user follows attention semantics. Success and pure
// information are events: they need nothing from the user, so they go to
// self-erasing toasts. Errors need the user to act — fix, retry or at
// least acknowledge — so they persist in a banner until dismissed or
// resolved, never auto-dismissed, never overwritten by a success.
//
// Feedback is produced in business logic, often in post callbacks, but
// toasts are only callable while the view builds: flash and fail only
// record, and drainFeedback converts the events to toasts in the view
// pass, the pattern counterpartFeedback uses.
type feedbackState struct {
	events []string        // success and information for the next frame's toasts
	errors []feedbackError // unacknowledged, newest last
}

// feedbackError is an error shown in the window's banner. key names the
// failing operation so its success resolves it: a save that goes through
// drops the save-failure banner without the user dismissing it.
type feedbackError struct {
	key, msg string
	action   *action // the recovery the call site offers, as Retry
}

// feedbackEntry is one piece of feedback as reported, for feedbackLog.
type feedbackEntry struct {
	msg string
	err bool
}

// feedbackLog records everything flash and fail report, for tests: the
// events drain away into toasts, after which nothing observes what the
// user saw. Production leaves it nil.
var feedbackLog *[]feedbackEntry

// flash records a success or information event for the next frame's
// toasts.
func (a *App) flash(msg string) {
	a.feedback.events = append(a.feedback.events, msg)
	if feedbackLog != nil {
		*feedbackLog = append(*feedbackLog, feedbackEntry{msg: msg})
	}
}

// fail records an error for the banner, where it stays until the user
// dismisses it or the operation it reports succeeds. The newest error
// shows; dismissing it reveals the one before, so several errors never
// lose one another. One message arriving twice replaces instead of
// stacking: a failed operation retried and failed again is one error,
// and the banner must not demand a dismissal per attempt.
func (a *App) fail(msg string) {
	a.failKeyed("", msg, nil)
}

// failAction is fail with the recovery the user runs from the banner,
// as Retry of a failed save. The banner closes when the action runs:
// a retried operation reports its own outcome — success resolves the
// key, failure records the error anew — and an action that cannot fail
// again, as Review, would leave a banner the user already acted on.
func (a *App) failAction(key, msg string, recovery action) {
	a.failKeyed(key, msg, &recovery)
}

func (a *App) failKeyed(key, msg string, recovery *action) {
	f := &a.feedback
	if i := slices.IndexFunc(f.errors, func(e feedbackError) bool { return e.msg == msg }); i >= 0 {
		f.errors = slices.Delete(f.errors, i, i+1)
	}
	f.errors = append(f.errors, feedbackError{key: key, msg: msg, action: recovery})
	if feedbackLog != nil {
		*feedbackLog = append(*feedbackLog, feedbackEntry{msg: msg, err: true})
	}
}

// resolveFeedback drops the errors of the failing operation key, as a
// save going through after an earlier save failed.
func (a *App) resolveFeedback(key string) {
	f := &a.feedback
	f.errors = slices.DeleteFunc(f.errors, func(e feedbackError) bool { return e.key == key })
}

// drainFeedback moves the events recorded since the last frame into
// toasts. c.Toast dedups on the message as the toast's ID, so repeating
// an action refreshes one toast instead of stacking three copies of it.
func (a *App) drainFeedback(c *ui.Context) {
	f := &a.feedback
	for _, msg := range f.events {
		c.Toast(msg)
	}
	f.events = nil
}

// errorBanner shows the newest unacknowledged error in the window, as
// the confirm banners do, until the user dismisses it — which reveals
// the error before it — or the failing operation succeeds. Where the
// others wait behind it the message says how many. Several errors queue
// rather than replace: the user answers the newest first, and none is
// lost as the overwrite-prone notice slot lost them.
func (a *App) errorBanner(c *ui.Context) {
	f := &a.feedback
	if len(f.errors) == 0 {
		return
	}
	latest := f.errors[len(f.errors)-1]
	actions := make([]action, 0, 2)
	if latest.action != nil {
		recovery := *latest.action
		actions = append(actions, action{recovery.label, func() {
			a.dismissFeedbackError()
			recovery.fn()
		}})
	}
	actions = append(actions, action{locales.Close(), a.dismissFeedbackError})
	msg := latest.msg
	if extra := len(f.errors) - 1; extra > 0 {
		msg += locales.ErrorBannerMore(extra)
	}
	a.banner(c, colors(c).WarnBg, msg, actions)
}

// dismissFeedbackError acknowledges the newest error.
func (a *App) dismissFeedbackError() {
	if n := len(a.feedback.errors); n > 0 {
		a.feedback.errors = a.feedback.errors[:n-1]
	}
}
