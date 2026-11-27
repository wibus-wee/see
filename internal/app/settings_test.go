package app

import (
	"path/filepath"
	"testing"

	"see/internal/config"
)

func TestVibrancySettingChangedAppliesAndPersists(t *testing.T) {
	h := newHarness(t, t.TempDir())
	path := filepath.Join(t.TempDir(), "config.json")
	h.a.cfg = config.Load(path)
	applied := 0
	h.a.vibrancyChanged = func() { applied++ }

	h.a.cfg.Vibrancy = true
	h.a.vibrancySettingChanged()
	if applied != 1 {
		t.Fatalf("vibrancy applied %d times, want 1", applied)
	}
	if !config.Load(path).Vibrancy {
		t.Fatal("sidebar vibrancy was not persisted")
	}
}
