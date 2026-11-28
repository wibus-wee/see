package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"see/locales"
)

// The settings card mirrors the outcome of install and uninstall inline,
// next to the buttons that ran them, while the toast or banner carries
// the same message to the window. The test runs the buttons in the card
// itself rather than the methods they call.
func TestShellCommandSettingsFeedback(t *testing.T) {
	h := newHarness(t, t.TempDir())
	settings := ui.NewTester(h.a.shellCommandCard, 900, 680)
	settings.Frame()
	if !settings.HasText("Install see in") {
		t.Fatalf("no shell command card: %q", settings.Texts())
	}
	// Install: the card carries the outcome inline, and the flash the
	// toast, once the main window drains it.
	control, ok := settings.AccessByRole(ui.RoleButton, locales.MsgInstallSeeCommand())
	if !ok {
		t.Fatalf("no install button: %v", settings.AccessAll())
	}
	clickAccess(t, settings, control)
	if h.a.settings.shellCommandMessage == "" {
		t.Fatal("install left the card without its inline feedback")
	}
	if !settings.HasText(h.a.settings.shellCommandMessage) {
		t.Fatalf("the card does not show the outcome inline: %q", settings.Texts())
	}
	if !h.hasFeedback(h.a.settings.shellCommandMessage) {
		t.Fatalf("install flashed nothing: %v", h.feedbackTexts())
	}

	// Uninstall: the card's inline feedback follows it, as does the flash.
	control, ok = settings.AccessByRole(ui.RoleButton, locales.MsgUninstallSeeCommand())
	if !ok {
		t.Fatalf("no uninstall button: %v", settings.AccessAll())
	}
	clickAccess(t, settings, control)
	if h.a.settings.shellCommandMessage == "" || !h.hasFeedback(h.a.settings.shellCommandMessage) {
		t.Fatalf("uninstall feedback: inline %q, flashes %v", h.a.settings.shellCommandMessage, h.feedbackTexts())
	}
	if !settings.HasText(h.a.settings.shellCommandMessage) {
		t.Fatalf("the card does not show the uninstall outcome: %q", settings.Texts())
	}
}
