package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSyntaxCorruptionKeepsOriginalBacksUpAndDegrades(t *testing.T) {
	broken := `{"editorFontSize": 16, "appearance": "d` // cut off mid-file, as a crash left it
	p := writeConfig(t, broken)
	c := Load(p)
	if len(c.Issues()) != 1 || c.Issues()[0].Kind != IssueParseFailed {
		t.Fatalf("issues: %+v", c.Issues())
	}
	issue := c.Issues()[0]
	if issue.Backup == "" {
		t.Fatal("no backup was made of the broken file")
	}
	backed, err := os.ReadFile(issue.Backup)
	if err != nil || string(backed) != broken {
		t.Fatalf("backup = %q err=%v, want the original bytes", backed, err)
	}
	original, err := os.ReadFile(p)
	if err != nil || string(original) != broken {
		t.Fatalf("the original file was modified: %q err=%v", original, err)
	}
	// Degraded mode is observable: every key is at its default.
	if c.FontSize != 0 || c.Appearance != "" || c.EditorFontSize() != 13 {
		t.Fatalf("degraded load: %+v", c)
	}
}

func TestSchemaInvalidFieldDegradesOnlyThatKey(t *testing.T) {
	p := writeConfig(t, `{"editorFontSize": "sixteen", "appearance": "dark"}`)
	c := Load(p)
	if c.FontSize != 0 {
		t.Fatalf("the mistyped key loaded: %v", c.FontSize)
	}
	if c.Appearance != "dark" {
		t.Fatalf("a valid neighbor key was lost: %q", c.Appearance)
	}
	if len(c.Issues()) != 1 || c.Issues()[0].Kind != IssueSchemaInvalid || c.Issues()[0].Key != "editorFontSize" {
		t.Fatalf("issues: %+v", c.Issues())
	}
	// Write-back preserves the original text of the broken key: nothing
	// the file said is silently discarded.
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	raw := readFileKeys(t, p)
	if string(raw["editorFontSize"]) != `"sixteen"` {
		t.Fatalf("the mistyped value was lost on write-back: %s", raw["editorFontSize"])
	}
	if string(raw["appearance"]) != `"dark"` {
		t.Fatalf("the valid neighbor changed: %s", raw["appearance"])
	}
}

// A validation-failed key is representable in memory, so write-back
// converges the file to the in-memory truth rather than preserving the
// invalid original. The truth here is the degraded default: the key is
// omitted, and the file heals — the next load reports nothing. Anything
// weaker fails the zero-value repair: the user fixing the field to its
// default emits no key, so a preserved invalid original would win the
// write-back back and the file could never heal at all.
func TestValidationFailureConvergesToInMemoryTruthOnWriteBack(t *testing.T) {
	p := writeConfig(t, `{"appearance": "purple"}`)
	c := Load(p)
	if c.Appearance != "" {
		t.Fatalf("the invalid enum loaded: %q", c.Appearance)
	}
	if len(c.Issues()) != 1 || c.Issues()[0].Kind != IssueValidationFailed || c.Issues()[0].Key != "appearance" {
		t.Fatalf("issues: %+v", c.Issues())
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	raw := readFileKeys(t, p)
	if _, present := raw["appearance"]; present {
		t.Fatalf("the invalid value survived write-back: %s — the in-memory default should replace it", raw["appearance"])
	}
	if issues := Load(p).Issues(); len(issues) != 0 {
		t.Fatalf("the file did not heal: %+v", issues)
	}
}

// TestZeroValueResetSticksOnWriteBack is the regression test for the
// write-back contract violation found in independent verification: every
// field is omitempty, so repairing an invalid value to the default — a
// zero value — used to emit nothing, letting the preserved invalid value
// win the collision. The file could never be repaired to a default
// through the app, and the warning repeated on every launch while the
// settings already showed the intended state.
func TestZeroValueResetSticksOnWriteBack(t *testing.T) {
	// "appearance" is the clearest case: "" is valid and meaningful
	// (follow the system appearance), so resetting to it is a real repair.
	p := writeConfig(t, `{"appearance": "purple"}`)
	c := Load(p)
	if len(c.Issues()) != 1 || c.Issues()[0].Kind != IssueValidationFailed {
		t.Fatalf("setup: %+v", c.Issues())
	}
	c.Appearance = "" // the user repairs to the default: system appearance
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	raw := readFileKeys(t, p)
	if got, present := raw["appearance"]; present {
		t.Fatalf("the repair did not stick: the invalid %s was written back over it", got)
	}
	if issues := Load(p).Issues(); len(issues) != 0 {
		t.Fatalf("the warning did not clear: %+v", issues)
	}

	// Same shape for a numeric key: editorFontSize 8 (invalid, below 9)
	// repaired to the default 0, which is 13 in effect.
	p2 := writeConfig(t, `{"editorFontSize": 8}`)
	c2 := Load(p2)
	if len(c2.Issues()) != 1 {
		t.Fatalf("setup: %+v", c2.Issues())
	}
	c2.FontSize = 0 // the user resets the font size to default
	if err := c2.Save(); err != nil {
		t.Fatal(err)
	}
	raw2 := readFileKeys(t, p2)
	if got, present := raw2["editorFontSize"]; present {
		t.Fatalf("the numeric repair did not stick: %s was written back over it", got)
	}
	if issues := Load(p2).Issues(); len(issues) != 0 {
		t.Fatalf("the numeric warning did not clear: %+v", issues)
	}
}

// A repair through a valid non-default value sticks the same way — and
// did even before the contract correction, because the emitted field won
// the collision. This pins that the correction did not regress it.
func TestRepairToNonDefaultSticksOnWriteBack(t *testing.T) {
	p := writeConfig(t, `{"appearance": "purple"}`)
	c := Load(p)
	c.Appearance = "dark"
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	raw := readFileKeys(t, p)
	if string(raw["appearance"]) != `"dark"` {
		t.Fatalf("the non-default repair did not stick: %s", raw["appearance"])
	}
	if issues := Load(p).Issues(); len(issues) != 0 {
		t.Fatalf("a non-default repair should stick: %+v", issues)
	}
}

// Values that do not fit their key's type are not representable in
// memory: they stay preserved verbatim, unlike validation failures. The
// asymmetry is the contract — memory may only overrule what memory can
// represent.
func TestSchemaInvalidValueStaysPreservedOnWriteBack(t *testing.T) {
	p := writeConfig(t, `{"editorFontSize": "sixteen"}`)
	c := Load(p)
	if len(c.Issues()) != 1 || c.Issues()[0].Kind != IssueSchemaInvalid {
		t.Fatalf("setup: %+v", c.Issues())
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	raw := readFileKeys(t, p)
	if string(raw["editorFontSize"]) != `"sixteen"` {
		t.Fatalf("the type-mismatched value was not preserved: %s", raw["editorFontSize"])
	}
}

// An explicit null unmarshals into any Go type without error, so it reads
// as the zero value — unset, meaning the default — and write-back
// normalizes it to the omission the zero value gets. Deliberate, not
// silent: null carries no information omission does not, and the
// degradation it degrades to is the same.
func TestNullReadsAsDefaultAndNormalizesOnWriteBack(t *testing.T) {
	p := writeConfig(t, `{"editorFontSize": null, "appearance": null}`)
	c := Load(p)
	if len(c.Issues()) != 0 {
		t.Fatalf("null values raised issues: %+v — null reads as unset", c.Issues())
	}
	if c.FontSize != 0 || c.Appearance != "" {
		t.Fatalf("null did not read as the default: %+v", c)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	raw := readFileKeys(t, p)
	for _, key := range []string{"editorFontSize", "appearance"} {
		if _, present := raw[key]; present {
			t.Fatalf("null was not normalized away: %s = %s", key, raw[key])
		}
	}
}

// A version declaration this build cannot read as a number is not
// representable in memory, so write-back preserves it rather than
// rewriting the file's schema claim into this build's integer.
func TestWronglyTypedVersionStaysPreservedOnWriteBack(t *testing.T) {
	p := writeConfig(t, `{"version": "1"}`)
	c := Load(p)
	if len(c.Issues()) != 1 || c.Issues()[0].Kind != IssueSchemaInvalid || c.Issues()[0].Key != "version" {
		t.Fatalf("issues: %+v", c.Issues())
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	raw := readFileKeys(t, p)
	if string(raw["version"]) != `"1"` {
		t.Fatalf("the wrongly-typed version was normalized to %s — the file's own declaration must survive", raw["version"])
	}
}

func readFileKeys(t *testing.T, p string) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("saved configuration is not valid JSON: %v\n%s", err, data)
	}
	return raw
}

func TestUnknownKeysSurviveRoundTrip(t *testing.T) {
	// Keys a newer build or a plugin may have written: the load does not
	// know them, must not complain, and must write them back untouched.
	p := writeConfig(t, `{"appearance": "dark", "futureFeature": {"nested": [1, 2]}, "version": 1}`)
	c := Load(p)
	if len(c.Issues()) != 0 {
		t.Fatalf("unknown keys raised issues: %+v", c.Issues())
	}
	if c.Appearance != "dark" {
		t.Fatalf("known key: %q", c.Appearance)
	}
	c.Appearance = "light"
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	raw := readFileKeys(t, p)
	// The unknown value survives semantically verbatim; indentation of
	// its tokens may follow the file's formatting.
	var got, want any
	if err := json.Unmarshal(raw["futureFeature"], &got); err != nil {
		t.Fatalf("unknown key is no longer valid JSON: %v", err)
	}
	if err := json.Unmarshal(json.RawMessage(`{"nested": [1, 2]}`), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unknown key rewritten: %s", raw["futureFeature"])
	}
	if string(raw["appearance"]) != `"light"` {
		t.Fatalf("known key not updated: %s", raw["appearance"])
	}
}

func TestNewerVersionDegradesWithIssue(t *testing.T) {
	p := writeConfig(t, `{"version": 99, "appearance": "dark", "newInV99": true}`)
	c := Load(p)
	if len(c.Issues()) != 1 || c.Issues()[0].Kind != IssueVersionNewer {
		t.Fatalf("issues: %+v", c.Issues())
	}
	// The load is degraded, not refused: keys this build knows still load.
	if c.Appearance != "dark" {
		t.Fatalf("known keys refused: %q", c.Appearance)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	raw := readFileKeys(t, p)
	if string(raw["newInV99"]) != `true` {
		t.Fatalf("the newer schema's keys were lost: %s", raw["newInV99"])
	}
	if string(raw["version"]) != "99" {
		t.Fatalf("write-back downgraded the version declaration to %s", raw["version"])
	}
}

func TestSavedConfigurationDeclaresVersion(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	c := Load(p)
	c.Appearance = "dark"
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	raw := readFileKeys(t, p)
	var version int
	if err := json.Unmarshal(raw["version"], &version); err != nil || version != CurrentVersion {
		t.Fatalf("version key = %s err=%v, want %d", raw["version"], err, CurrentVersion)
	}
	// And the saved file loads back cleanly: Save writes valid JSON.
	if issues := Load(p).Issues(); len(issues) != 0 {
		t.Fatalf("own output did not load cleanly: %+v", issues)
	}
}

func TestMissingFileIsNotAnIssue(t *testing.T) {
	c := Load(filepath.Join(t.TempDir(), "config.json"))
	if len(c.Issues()) != 0 {
		t.Fatalf("the first run reported issues: %+v", c.Issues())
	}
}

func TestUnreadableFileReportsReadFailure(t *testing.T) {
	// A directory where the file should be: reading it as a file fails
	// on every platform and for every user. POSIX mode bits cannot
	// express the same denial portably — root reads through them — and
	// Windows has no owner-equivalent denial through chmod at all.
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.Mkdir(p, 0o700); err != nil {
		t.Fatal(err)
	}
	c := Load(p)
	if len(c.Issues()) != 1 || c.Issues()[0].Kind != IssueReadFailed {
		t.Fatalf("issues: %+v", c.Issues())
	}
	if c.Appearance != "" {
		t.Fatal("unreadable configuration still loaded")
	}
}

func TestSaveLeavesNoStagingBehind(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	c := Load(p)
	c.Appearance = "dark"
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("staging left behind: %s", e.Name())
		}
	}
}
