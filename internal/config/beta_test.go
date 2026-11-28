package config

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func withBetaFeature(t *testing.T, id string, spec betaFeatureSpec) {
	t.Helper()
	old, existed := betaFeatures[id]
	betaFeatures[id] = spec
	t.Cleanup(func() {
		if existed {
			betaFeatures[id] = old
		} else {
			delete(betaFeatures, id)
		}
	})
}

func writeConfig(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func readConfigJSON(t *testing.T, p string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBetaSettingsPersistNamespacedAndTyped(t *testing.T) {
	withBetaFeature(t, "test-feature", betaFeatureSpec{settings: map[string]betaSettingSpec{
		"enabledExtra": {kind: betaBoolSetting, defaultValue: true},
		"mode":         {kind: betaChoiceSetting, defaultValue: "auto", choices: []string{"auto", "manual"}},
		"ratio":        {kind: betaNumberSetting, defaultValue: 1.5},
		"count":        {kind: betaIntSetting, defaultValue: 3},
		"title":        {kind: betaStringSetting, defaultValue: "draft"},
	}})

	p := filepath.Join(t.TempDir(), "config.json")
	c := Load(p)
	if c.BetaEnabled("test-feature") {
		t.Fatal("beta feature should default disabled")
	}
	if !c.BetaBool("test-feature", "enabledExtra") || c.BetaString("test-feature", "mode") != "auto" ||
		c.BetaNumber("test-feature", "ratio") != 1.5 || c.BetaInt("test-feature", "count") != 3 ||
		c.BetaString("test-feature", "title") != "draft" {
		t.Fatal("registered defaults were not exposed")
	}
	if !c.SetBetaEnabled("test-feature", true) {
		t.Fatal("registered beta feature was not accepted")
	}
	if !c.SetBetaSetting("test-feature", "mode", "manual") ||
		!c.SetBetaSetting("test-feature", "ratio", 2.5) ||
		!c.SetBetaSetting("test-feature", "count", 7) {
		t.Fatal("registered beta settings were not accepted")
	}
	if c.SetBetaSetting("test-feature", "mode", "invalid") ||
		c.SetBetaSetting("test-feature", "ratio", math.NaN()) ||
		c.SetBetaSetting("test-feature", "missing", true) ||
		c.SetBetaSetting("missing-feature", "mode", "manual") {
		t.Fatal("invalid beta settings were accepted")
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	loaded := Load(p)
	if !loaded.BetaEnabled("test-feature") || loaded.BetaString("test-feature", "mode") != "manual" ||
		loaded.BetaNumber("test-feature", "ratio") != 2.5 || loaded.BetaInt("test-feature", "count") != 7 {
		t.Fatalf("beta settings did not round trip: %+v", loaded.Beta)
	}
	raw := readConfigJSON(t, p)
	features, ok := raw["betaFeatures"].(map[string]any)
	if !ok || features["test-feature"] == nil {
		t.Fatalf("beta settings are not namespaced: %v", raw)
	}
}

func TestBetaSchemaPrunesObsoleteAndInvalidSettings(t *testing.T) {
	withBetaFeature(t, "test-feature", betaFeatureSpec{settings: map[string]betaSettingSpec{
		"mode":  {kind: betaChoiceSetting, defaultValue: "auto", choices: []string{"auto", "manual"}},
		"count": {kind: betaIntSetting, defaultValue: 3},
	}})
	p := writeConfig(t, `{
		"betaFeatures": {
			"test-feature": {
				"enabled": true,
				"settings": {
					"mode": "removed-mode",
					"count": 4,
					"obsolete": true
				}
			}
		}
	}`)

	c := Load(p)
	feature := c.Beta["test-feature"]
	if _, found := feature.Settings["obsolete"]; found {
		t.Fatalf("obsolete setting survived: %+v", feature.Settings)
	}
	if _, found := feature.Settings["mode"]; found {
		t.Fatalf("invalid choice survived: %+v", feature.Settings)
	}
	if c.BetaInt("test-feature", "count") != 4 || c.BetaString("test-feature", "mode") != "auto" {
		t.Fatalf("schema values wrong: %+v", feature.Settings)
	}

	raw := readConfigJSON(t, p)
	settings := raw["betaFeatures"].(map[string]any)["test-feature"].(map[string]any)["settings"].(map[string]any)
	if _, found := settings["obsolete"]; found {
		t.Fatalf("migration was not saved: %+v", settings)
	}
}

func TestBetaMigrationGraduatesSettingsIntoStableConfig(t *testing.T) {
	withBetaFeature(t, "test-feature", betaFeatureSpec{
		settings: map[string]betaSettingSpec{
			"width": {kind: betaNumberSetting},
		},
		graduate: func(c *Config, feature BetaFeature) {
			if !c.hadStableKey("vibrancy") {
				c.Vibrancy = feature.Enabled
			}
			if width, ok := betaNumberValue(feature.Settings, "width"); ok && !c.hadStableKey("sidebarWidth") {
				c.SidebarWidth = float32(width)
			}
		},
	})
	p := writeConfig(t, `{
		"vibrancy": false,
		"betaFeatures": {
			"test-feature": {
				"enabled": true,
				"settings": {"width": 240, "obsolete": "drop me"}
			}
		}
	}`)

	c := Load(p)
	if c.Vibrancy {
		t.Fatal("explicit stable config lost to beta migration")
	}
	if c.SidebarWidth != 240 {
		t.Fatalf("beta width did not graduate: %v", c.SidebarWidth)
	}
	if _, found := c.Beta["test-feature"]; found {
		t.Fatalf("graduated beta feature remains: %+v", c.Beta)
	}
	raw := readConfigJSON(t, p)
	if _, found := raw["betaFeatures"]; found {
		t.Fatalf("graduated beta namespace remains in saved config: %+v", raw)
	}
	if raw["sidebarWidth"] != 240.0 {
		t.Fatalf("graduated stable value missing from saved config: %+v", raw)
	}
}

func TestBetaMigrationGraduatesEnabledStateWhenStableKeyIsAbsent(t *testing.T) {
	withBetaFeature(t, "test-feature", betaFeatureSpec{
		graduate: func(c *Config, feature BetaFeature) {
			if !c.hadStableKey("vibrancy") {
				c.Vibrancy = feature.Enabled
			}
		},
	})
	p := writeConfig(t, `{
		"betaFeatures": {"test-feature": {"enabled": true}}
	}`)

	c := Load(p)
	if !c.Vibrancy {
		t.Fatal("beta enabled state did not graduate")
	}
	if raw := readConfigJSON(t, p); raw["vibrancy"] != true {
		t.Fatalf("graduated enabled state missing from saved config: %+v", raw)
	}
}

func TestBetaMigrationRetiresFeatureAndKeepsFutureUnknownFeatures(t *testing.T) {
	withBetaFeature(t, "retired-feature", betaFeatureSpec{retired: true})
	p := writeConfig(t, `{
		"betaFeatures": {
			"retired-feature": {"enabled": true, "settings": {"obsolete": 1}},
			"future-feature": {"enabled": true, "settings": {"unknown-shape": {"nested": true}}}
		}
	}`)

	c := Load(p)
	if _, found := c.Beta["retired-feature"]; found {
		t.Fatalf("retired feature survived: %+v", c.Beta)
	}
	future, found := c.Beta["future-feature"]
	if !found || !future.Enabled || future.Settings["unknown-shape"] == nil {
		t.Fatalf("unknown newer feature was not preserved: %+v", c.Beta)
	}
}

func TestBetaMigrationDropsEmptySettingObjects(t *testing.T) {
	withBetaFeature(t, "test-feature", betaFeatureSpec{settings: map[string]betaSettingSpec{
		"count": {kind: betaIntSetting},
	}})
	p := writeConfig(t, `{
		"betaFeatures": {"test-feature": {"enabled": true, "settings": {}}}
	}`)

	c := Load(p)
	if !c.BetaEnabled("test-feature") {
		t.Fatal("empty settings discarded the feature's enabled state")
	}
	raw := readConfigJSON(t, p)
	feature := raw["betaFeatures"].(map[string]any)["test-feature"].(map[string]any)
	if _, found := feature["settings"]; found {
		t.Fatalf("empty settings object survived: %+v", feature)
	}
}

func TestBetaMigrationIsIdempotentAndDropsMalformedBetaData(t *testing.T) {
	p := writeConfig(t, `{
		"vibrancy": true,
		"betaFeatures": "broken"
	}`)
	c := Load(p)
	if !c.Vibrancy {
		t.Fatal("malformed beta data discarded stable settings")
	}
	if c.Beta != nil {
		t.Fatalf("malformed beta data survived: %+v", c.Beta)
	}
	first := readConfigJSON(t, p)
	if _, found := first["betaFeatures"]; found {
		t.Fatalf("malformed beta data was not cleaned: %+v", first)
	}
	again := Load(p)
	if !again.Vibrancy || again.Beta != nil {
		t.Fatalf("second load changed migrated config: %+v", again)
	}
}
