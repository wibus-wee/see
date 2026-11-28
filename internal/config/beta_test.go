package config

import (
	"bytes"
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

func TestMultiRootWorkspacesBetaFeatureIsRegistered(t *testing.T) {
	for _, feature := range ActiveBetaFeatures() {
		if feature.ID == BetaMultiRootWorkspaces {
			if len(feature.Settings) != 0 {
				t.Fatalf("multi-root workspaces should not have beta parameters: %+v", feature.Settings)
			}
			if Load("").BetaEnabled(BetaMultiRootWorkspaces) {
				t.Fatal("multi-root workspaces beta should default disabled")
			}
			return
		}
	}
	t.Fatalf("multi-root workspaces is not an active beta feature: %+v", ActiveBetaFeatures())
}

func TestSidebarVibrancyBetaFeatureIsRegistered(t *testing.T) {
	var feature BetaFeatureInfo
	for _, f := range ActiveBetaFeatures() {
		if f.ID == BetaSidebarVibrancy {
			feature = f
		}
	}
	if feature.ID != BetaSidebarVibrancy {
		t.Fatalf("sidebar vibrancy is not an active beta feature: %+v", ActiveBetaFeatures())
	}
	if len(feature.Settings) != 1 || feature.Settings[0].Key != "material" || feature.Settings[0].Kind != BetaChoiceSetting {
		t.Fatalf("sidebar vibrancy settings: %+v", feature.Settings)
	}
	c := Load("")
	if c.BetaEnabled(BetaSidebarVibrancy) {
		t.Fatal("sidebar vibrancy beta should default disabled")
	}
	if got := c.BetaString(BetaSidebarVibrancy, "material"); got != "sidebar" {
		t.Fatalf("default material = %q", got)
	}
}

func TestUnreleasedStableVibrancyMovesIntoBeta(t *testing.T) {
	p := writeConfig(t, `{"vibrancy": true}`)
	c := Load(p)
	if !c.BetaEnabled(BetaSidebarVibrancy) {
		t.Fatalf("unreleased vibrancy setting did not move into beta: %+v", c.Beta)
	}
	raw := readConfigJSON(t, p)
	if _, found := raw["vibrancy"]; found {
		t.Fatalf("legacy vibrancy key survived: %+v", raw)
	}
}

func TestCapitalizedUnreleasedStableVibrancyMovesIntoBeta(t *testing.T) {
	p := writeConfig(t, `{"Vibrancy": true}`)
	if !Load(p).BetaEnabled(BetaSidebarVibrancy) {
		t.Fatal("capitalized legacy vibrancy setting did not move into beta")
	}
}

func TestBetaSettingsPersistNamespacedAndTyped(t *testing.T) {
	withBetaFeature(t, "test-feature", betaFeatureSpec{settings: map[string]betaSettingSpec{
		"enabledExtra": {kind: BetaBoolSetting, defaultValue: true},
		"mode":         {kind: BetaChoiceSetting, defaultValue: "auto", choices: []string{"auto", "manual"}},
		"ratio":        {kind: BetaNumberSetting, defaultValue: 1.5},
		"count":        {kind: BetaIntSetting, defaultValue: 3},
		"title":        {kind: BetaStringSetting, defaultValue: "draft"},
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
		"mode":  {kind: BetaChoiceSetting, defaultValue: "auto", choices: []string{"auto", "manual"}},
		"count": {kind: BetaIntSetting, defaultValue: 3},
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
			"width": {kind: BetaNumberSetting},
		},
		graduate: func(c *Config, feature BetaFeature) {
			if !c.hadStableKey("gitAutoDiscover") {
				c.GitAutoDiscover = feature.Enabled
			}
			if width, ok := betaNumberValue(feature.Settings, "width"); ok && !c.hadStableKey("sidebarWidth") {
				c.SidebarWidth = float32(width)
			}
		},
	})
	p := writeConfig(t, `{
		"gitAutoDiscover": false,
		"betaFeatures": {
			"test-feature": {
				"enabled": true,
				"settings": {"width": 240, "obsolete": "drop me"}
			}
		}
	}`)

	c := Load(p)
	if c.GitAutoDiscover {
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
			if !c.hadStableKey("gitAutoDiscover") {
				c.GitAutoDiscover = feature.Enabled
			}
		},
	})
	p := writeConfig(t, `{
		"betaFeatures": {"test-feature": {"enabled": true}}
	}`)

	c := Load(p)
	if !c.GitAutoDiscover {
		t.Fatal("beta enabled state did not graduate")
	}
	if raw := readConfigJSON(t, p); raw["gitAutoDiscover"] != true {
		t.Fatalf("graduated enabled state missing from saved config: %+v", raw)
	}
}

func TestBetaMigrationRetiresFeatureAndKeepsFutureUnknownFeatures(t *testing.T) {
	withBetaFeature(t, "retired-feature", betaFeatureSpec{retired: true})
	p := writeConfig(t, `{
		"betaFeatures": {
			"retired-feature": {"enabled": true, "settings": {"obsolete": 1}},
			"future-feature": {"enabled": true, "futureFlag": {"version": 2}, "settings": {"unknown-shape": {"nested": true, "large": 9007199254740993}}},
			"future-scalar": ["new-shape"]
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
	feature := readConfigJSON(t, p)["betaFeatures"].(map[string]any)["future-feature"].(map[string]any)
	if flag, ok := feature["futureFlag"].(map[string]any); !ok || flag["version"] != float64(2) {
		t.Fatalf("unknown envelope fields were rewritten away: %+v", feature)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&saved); err != nil {
		t.Fatal(err)
	}
	beta := saved["betaFeatures"].(map[string]any)
	large := beta["future-feature"].(map[string]any)["settings"].(map[string]any)["unknown-shape"].(map[string]any)["large"]
	if large.(json.Number).String() != "9007199254740993" {
		t.Fatalf("unknown numeric setting lost precision: %#v", large)
	}
	scalar := beta["future-scalar"].([]any)
	if len(scalar) != 1 || scalar[0] != "new-shape" {
		t.Fatalf("unknown scalar feature was rewritten: %+v", scalar)
	}
	first := string(data)
	Load(p)
	second, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != first {
		t.Fatalf("preserved unknown settings were not idempotent:\nfirst:  %s\nsecond: %s", first, second)
	}
}

func TestBetaIntSettingPreservesLargeJSONNumber(t *testing.T) {
	withBetaFeature(t, "test-feature", betaFeatureSpec{settings: map[string]betaSettingSpec{
		"count": {kind: BetaIntSetting},
	}})
	p := writeConfig(t, `{
		"betaFeatures": {"test-feature": {"enabled": true, "settings": {"count": 9007199254740993}}}
	}`)

	c := Load(p)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&saved); err != nil {
		t.Fatal(err)
	}
	count := saved["betaFeatures"].(map[string]any)["test-feature"].(map[string]any)["settings"].(map[string]any)["count"]
	if count.(json.Number).String() != "9007199254740993" {
		t.Fatalf("large integer setting lost precision: %#v", count)
	}
	if !c.BetaEnabled("test-feature") {
		t.Fatal("valid enabled state was discarded with the invalid integer")
	}
}

func TestBetaMigrationDropsEmptySettingObjects(t *testing.T) {
	withBetaFeature(t, "test-feature", betaFeatureSpec{settings: map[string]betaSettingSpec{
		"count": {kind: BetaIntSetting},
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

func TestBetaMigrationIsIdempotentAndPreservesMalformedBetaData(t *testing.T) {
	// A betaFeatures value no beta shape can represent is user data the
	// in-memory configuration cannot hold: the configuration contract
	// (SPEC-007, REQ-CFG-070) preserves it verbatim for the build it may
	// belong to, and reports the schema failure through Issues. What the
	// migration still guarantees is that stable settings around it are
	// read, that nothing in memory pretends to be beta state it is not,
	// and that loading the result is idempotent.
	p := writeConfig(t, `{
		"gitAutoDiscover": true,
		"betaFeatures": "broken"
	}`)
	c := Load(p)
	if !c.GitAutoDiscover {
		t.Fatal("malformed beta data discarded stable settings")
	}
	if c.Beta != nil {
		t.Fatalf("malformed beta data survived: %+v", c.Beta)
	}
	reported := false
	for _, issue := range c.Issues() {
		if issue.Kind == IssueSchemaInvalid && issue.Key == "betaFeatures" {
			reported = true
		}
	}
	if !reported {
		t.Fatalf("malformed beta namespace was not reported: %+v", c.Issues())
	}
	first := readConfigJSON(t, p)
	if first["betaFeatures"] != "broken" {
		t.Fatalf("malformed beta data was not preserved verbatim: %+v", first)
	}
	again := Load(p)
	if !again.GitAutoDiscover || again.Beta != nil {
		t.Fatalf("second load changed migrated config: %+v", again)
	}
}
