package app

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"see/internal/config"
	"see/locales"
)

func clickAccess(t *testing.T, tt *ui.Tester, access ui.AccessInfo) {
	t.Helper()
	tt.ClickAt(access.Bounds.X+access.Bounds.W/2, access.Bounds.Y+access.Bounds.H/2)
	tt.Frame()
}

func TestBetaSettingsPaneControlsVibrancyFeature(t *testing.T) {
	h := newHarness(t, t.TempDir())
	path := filepath.Join(t.TempDir(), "config.json")
	h.a.cfg = config.Load(path)
	applied := 0
	h.a.vibrancyChanged = func() { applied++ }
	h.a.settings.pane = "beta"
	settings := ui.NewTester(h.a.settingsView, 900, 680)
	settings.Frame()

	if !settings.HasText(locales.BetaFeatures()) || !settings.HasText(locales.BetaFeaturesDescription()) || !settings.HasText(locales.SidebarVibrancy()) || !settings.HasText(locales.BetaMultiRootWorkspaces()) {
		t.Fatalf("beta pane missing feature copy: %v", settings.AccessAll())
	}
	if settings.HasText(locales.BetaVibrancyMaterial()) {
		t.Fatal("beta parameter visible before opting in")
	}

	control, ok := settings.AccessByRole(ui.RoleSwitch, locales.SidebarVibrancy())
	if !ok {
		t.Fatalf("no sidebar vibrancy switch: %v", settings.AccessAll())
	}
	clickAccess(t, settings, control)
	if applied == 0 {
		t.Fatal("vibrancy change was not applied")
	}
	if !h.a.cfg.BetaEnabled(config.BetaSidebarVibrancy) || !settings.HasText(locales.BetaVibrancyMaterial()) {
		t.Fatalf("beta feature did not opt in: %+v", h.a.cfg.Beta)
	}

	glass, ok := settings.AccessByRole(ui.RoleRadio, locales.BetaVibrancyMaterialGlass())
	if !ok {
		t.Fatalf("no glass material choice: %v", settings.AccessAll())
	}
	clickAccess(t, settings, glass)
	if got := h.a.cfg.BetaString(config.BetaSidebarVibrancy, "material"); got != "glass" {
		t.Fatalf("material = %q, want glass", got)
	}

	loaded := config.Load(path)
	if !loaded.BetaEnabled(config.BetaSidebarVibrancy) || loaded.BetaString(config.BetaSidebarVibrancy, "material") != "glass" {
		t.Fatalf("beta settings did not persist: %+v", loaded.Beta)
	}
}
