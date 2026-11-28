package config

import (
	"encoding/json"
	"math"
	"reflect"
	"slices"
	"sort"
)

// BetaFeature is one feature's unfinished configuration. Everything a beta
// feature remembers stays in its own namespace, so graduating or retiring it
// never leaves settings mixed into the stable configuration schema.
type BetaFeature struct {
	Enabled  bool           `json:"enabled,omitempty"`
	Settings map[string]any `json:"settings,omitempty"`
}

type betaSettingKind int

const (
	betaBoolSetting betaSettingKind = iota
	betaStringSetting
	betaNumberSetting
	betaIntSetting
	betaChoiceSetting
)

type betaSettingSpec struct {
	kind         betaSettingKind
	defaultValue any
	choices      []string
}

type betaFeatureSpec struct {
	settings map[string]betaSettingSpec
	// graduate moves a shipped feature's beta state into stable settings. Keep
	// it here after the feature code itself moves out of beta; deleting the
	// registry entry would make old beta data look like a newer unknown feature.
	graduate func(*Config, BetaFeature)
	// retired drops a feature that never shipped or whose state has no stable
	// destination.
	retired bool
}

// betaFeatures is the lifecycle registry. New beta features add their settings
// schema here; callers gate hidden UI with BetaEnabled and read parameters with
// the typed Beta accessors. A feature whose beta state has shipped must keep a
// graduate or retired entry forever, so old configuration is migrated or
// discarded instead of accumulating.
var betaFeatures = map[string]betaFeatureSpec{}

func init() {
	for id, spec := range betaFeatures {
		if id == "" {
			panic("beta feature with empty id")
		}
		if spec.retired && spec.graduate != nil {
			panic("beta feature " + id + " is both graduated and retired")
		}
		for key, setting := range spec.settings {
			if key == "" {
				panic("beta feature " + id + " has a setting with an empty key")
			}
			if setting.kind == betaChoiceSetting && len(setting.choices) == 0 {
				panic("beta setting " + id + "." + key + " has no choices")
			}
			if setting.defaultValue != nil {
				if _, ok := setting.normalize(setting.defaultValue); !ok {
					panic("beta setting " + id + "." + key + " has an invalid default")
				}
			}
		}
	}
}

func (spec betaFeatureSpec) active() bool {
	return !spec.retired && spec.graduate == nil
}

func activeBetaFeature(feature string) (betaFeatureSpec, bool) {
	spec, ok := betaFeatures[feature]
	return spec, ok && spec.active()
}

func (spec betaSettingSpec) normalize(value any) (any, bool) {
	switch spec.kind {
	case betaBoolSetting:
		v, ok := value.(bool)
		return v, ok
	case betaStringSetting:
		v, ok := value.(string)
		return v, ok
	case betaNumberSetting:
		v, ok := betaFloat(value)
		return v, ok && finite(v)
	case betaIntSetting:
		v, ok := betaFloat(value)
		if !ok || !finite(v) || math.Trunc(v) != v || v < math.MinInt || v > math.MaxInt {
			return nil, false
		}
		return int(v), true
	case betaChoiceSetting:
		v, ok := value.(string)
		return v, ok && slices.Contains(spec.choices, v)
	}
	return nil, false
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func betaFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	}
	return 0, false
}

func (spec betaSettingSpec) defaultOrZero() any {
	if spec.defaultValue == nil {
		switch spec.kind {
		case betaBoolSetting:
			return false
		case betaStringSetting, betaChoiceSetting:
			return ""
		case betaNumberSetting:
			return float64(0)
		case betaIntSetting:
			return 0
		}
	}
	if value, ok := spec.normalize(spec.defaultValue); ok {
		return value
	}
	return nil
}

func (c *Config) betaValue(feature, key string) (any, bool) {
	spec, ok := activeBetaFeature(feature)
	if !ok {
		return nil, false
	}
	setting, ok := spec.settings[key]
	if !ok {
		return nil, false
	}
	if entry, ok := c.Beta[feature]; ok {
		if value, ok := entry.Settings[key]; ok {
			if normalized, ok := setting.normalize(value); ok {
				return normalized, true
			}
		}
	}
	return setting.defaultOrZero(), true
}

// BetaEnabled reports whether a registered beta feature is visible and active.
func (c *Config) BetaEnabled(feature string) bool {
	if _, ok := activeBetaFeature(feature); !ok {
		return false
	}
	return c.Beta[feature].Enabled
}

// SetBetaEnabled changes a registered beta feature's visibility. It reports
// whether the feature is still active and accepted the change.
func (c *Config) SetBetaEnabled(feature string, enabled bool) bool {
	if _, ok := activeBetaFeature(feature); !ok {
		return false
	}
	entry := c.Beta[feature]
	if entry.Enabled == enabled {
		return true
	}
	entry.Enabled = enabled
	if enabled || len(entry.Settings) > 0 {
		if c.Beta == nil {
			c.Beta = map[string]BetaFeature{}
		}
		c.Beta[feature] = entry
	} else {
		delete(c.Beta, feature)
	}
	if len(c.Beta) == 0 {
		c.Beta = nil
	}
	return true
}

// SetBetaSetting validates and stores one parameter in a beta feature's
// namespace. It reports whether the feature and setting are registered and the
// value has the registered type.
func (c *Config) SetBetaSetting(feature, key string, value any) bool {
	spec, ok := activeBetaFeature(feature)
	if !ok {
		return false
	}
	setting, ok := spec.settings[key]
	if !ok {
		return false
	}
	normalized, ok := setting.normalize(value)
	if !ok {
		return false
	}
	entry := c.Beta[feature]
	if entry.Settings == nil {
		entry.Settings = map[string]any{}
	}
	entry.Settings[key] = normalized
	if c.Beta == nil {
		c.Beta = map[string]BetaFeature{}
	}
	c.Beta[feature] = entry
	return true
}

func (c *Config) BetaBool(feature, key string) bool {
	value, _ := c.betaValue(feature, key)
	v, _ := value.(bool)
	return v
}

func (c *Config) BetaString(feature, key string) string {
	value, _ := c.betaValue(feature, key)
	v, _ := value.(string)
	return v
}

func (c *Config) BetaNumber(feature, key string) float64 {
	value, _ := c.betaValue(feature, key)
	v, _ := value.(float64)
	return v
}

func (c *Config) BetaInt(feature, key string) int {
	value, _ := c.betaValue(feature, key)
	v, _ := value.(int)
	return v
}

func betaBoolValue(settings map[string]any, key string) (bool, bool) {
	v, ok := settings[key].(bool)
	return v, ok
}

func betaNumberValue(settings map[string]any, key string) (float64, bool) {
	return betaFloat(settings[key])
}

func (c *Config) hadStableKey(key string) bool {
	return c.stableKeys[key]
}

// parseBetaFeatures decodes the beta namespace without letting malformed beta
// data poison the stable configuration around it. The boolean reports input
// that should be rewritten in the clean shape.
func parseBetaFeatures(data json.RawMessage) (map[string]BetaFeature, bool) {
	if string(data) == "null" {
		return nil, true
	}
	var rawFeatures map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawFeatures); err != nil {
		return nil, true
	}
	features := map[string]BetaFeature{}
	changed := len(rawFeatures) == 0
	for id, raw := range rawFeatures {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil || object == nil {
			changed = true
			continue
		}
		var feature BetaFeature
		for key, value := range object {
			switch key {
			case "enabled":
				var enabled bool
				if err := json.Unmarshal(value, &enabled); err == nil {
					feature.Enabled = enabled
				} else {
					changed = true
				}
			case "settings":
				var settings map[string]any
				if err := json.Unmarshal(value, &settings); err == nil && settings != nil {
					feature.Settings = settings
				} else {
					changed = true
				}
			default:
				changed = true
			}
		}
		features[id] = feature
	}
	if len(features) == 0 {
		return nil, true
	}
	return features, changed
}

func normalizeBetaSettings(feature BetaFeature, spec betaFeatureSpec) (BetaFeature, bool) {
	changed := feature.Settings != nil && len(feature.Settings) == 0
	settings := map[string]any{}
	for key, value := range feature.Settings {
		setting, registered := spec.settings[key]
		if !registered {
			changed = true
			continue
		}
		normalized, valid := setting.normalize(value)
		if !valid {
			changed = true
			continue
		}
		if !reflect.DeepEqual(normalized, value) {
			changed = true
		}
		settings[key] = normalized
	}
	if len(settings) == 0 {
		settings = nil
	}
	feature.Settings = settings
	return feature, changed
}

// migrateBeta converges beta configuration on its current lifecycle state:
// active entries are schema-pruned, graduated entries move into stable settings
// and disappear, retired entries disappear, and unknown entries are left alone
// because they may belong to a newer version of See.
func (c *Config) migrateBeta() bool {
	changed := false
	ids := make([]string, 0, len(c.Beta))
	for id := range c.Beta {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		feature := c.Beta[id]
		spec, registered := betaFeatures[id]
		if !registered {
			if !feature.Enabled && len(feature.Settings) == 0 {
				delete(c.Beta, id)
				changed = true
			}
			continue
		}
		if spec.retired {
			delete(c.Beta, id)
			changed = true
			continue
		}
		normalized, cleaned := normalizeBetaSettings(feature, spec)
		if spec.graduate != nil {
			spec.graduate(c, normalized)
			delete(c.Beta, id)
			changed = true
			continue
		}
		if !normalized.Enabled && len(normalized.Settings) == 0 {
			delete(c.Beta, id)
			changed = true
			continue
		}
		if cleaned {
			c.Beta[id] = normalized
			changed = true
		}
	}
	if len(c.Beta) == 0 && c.Beta != nil {
		c.Beta = nil
		changed = true
	}
	return changed
}
