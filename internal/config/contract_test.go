package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

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

func TestValidationFailurePreservesOriginalOnWriteBack(t *testing.T) {
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
	if string(raw["appearance"]) != `"purple"` {
		t.Fatalf("the invalid value was silently rewritten: %s", raw["appearance"])
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
	if os.Getuid() == 0 {
		t.Skip("root reads anything")
	}
	p := writeConfig(t, `{"appearance": "dark"}`)
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o600) })
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
