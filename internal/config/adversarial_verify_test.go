// Adversarial verification tests written by the independent verifier of
// lane C (branch verify-lane-c). They probe behaviors the implementer's
// own suite does not cover. They do not modify the implementation.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func verifyFileKeys(t *testing.T, p string) map[string]json.RawMessage {
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

// TestVerifyZeroValueResetResurrectsInvalidValue is the central
// adversarial finding of the write-back contract. The documented rule
// says "a struct field wins a collision with a preserved key: once the
// key validates again, the typed value is the configuration." But every
// struct field is tagged omitempty, so a user repair to the DEFAULT
// value — zero, false, "" — emits nothing for the key, the preserved
// invalid value wins the "collision" by default, and the user's fix is
// silently replaced by the value they just repaired.
func TestVerifyZeroValueResetResurrectsInvalidValue(t *testing.T) {
	// appearance is the clearest case: "" is a valid, meaningful value
	// (follow the system appearance), so setting it is a real repair.
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"appearance": "purple"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := Load(p)
	if len(c.Issues()) != 1 || c.Issues()[0].Kind != IssueValidationFailed {
		t.Fatalf("setup: %+v", c.Issues())
	}

	// The user repairs the field to the default: system appearance.
	c.Appearance = ""
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	raw := verifyFileKeys(t, p)
	if got := string(raw["appearance"]); got != `"purple"` {
		t.Fatalf("control assumption wrong: file has appearance=%s", got)
	}
	reloaded := Load(p)
	if len(reloaded.Issues()) != 1 || reloaded.Issues()[0].Key != "appearance" {
		t.Fatalf("the repair did not stick: the invalid value was resurrected, issues %+v", reloaded.Issues())
	}
	t.Log("CONFIRMED: repairing an invalid enum to its zero value is silently overwritten by the preserved invalid value on the very next save")

	// Same shape for a numeric key: editorFontSize 8 (invalid, < 9)
	// repaired to the default 0 (= 13 effective).
	p2 := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p2, []byte(`{"editorFontSize": 8}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c2 := Load(p2)
	c2.FontSize = 0 // the user resets the font size to default
	if err := c2.Save(); err != nil {
		t.Fatal(err)
	}
	raw2 := verifyFileKeys(t, p2)
	if got := string(raw2["editorFontSize"]); got != "8" {
		t.Fatalf("control assumption wrong: file has editorFontSize=%s", got)
	}
	t.Log("CONFIRMED: editorFontSize reset to default is likewise overwritten by the preserved invalid 8")
}

// TestVerifyRepairToNonDefaultDoesStick documents the asymmetry: the
// invalid value is only dropped when the repair is non-zero. This is why
// the bug is silent data loss rather than a constant annoyance.
func TestVerifyRepairToNonDefaultDoesStick(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"appearance": "purple"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := Load(p)
	c.Appearance = "dark"
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if issues := Load(p).Issues(); len(issues) != 0 {
		t.Fatalf("a non-default repair should stick: %+v", issues)
	}
}

// TestVerifyFailedBackupThenSaveOverwritesOriginal demonstrates the
// REQ-CFG-040 hole: "only after user confirmation or automatic backup
// completes may the original be overwritten." When the backup write
// fails, nothing records that the original is unprotected — the next
// Save destroys the user's broken file with no backup in existence.
func TestVerifyFailedBackupThenSaveOverwritesOriginal(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root writes through permission bits")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	broken := `{"editorFontSize": 16, corrupted`
	if err := os.WriteFile(p, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	// The backup write fails because the configuration directory cannot
	// be written right now — a full disk, a transient permission problem.
	// (mkdir -p of the backup's parent has nothing to create: it is the
	// config directory itself.)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	c := Load(p)
	issues := c.Issues()
	if len(issues) != 1 || issues[0].Kind != IssueParseFailed {
		t.Fatalf("setup: %+v", issues)
	}
	if issues[0].Backup != "" {
		t.Fatalf("setup: backup unexpectedly succeeded at %s", issues[0].Backup)
	}
	t.Logf("backup failed as arranged; issue carries Backup=%q", issues[0].Backup)

	// The directory recovers before the user next changes a setting.
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	c.Appearance = "dark" // any setting change; in the app this is one click
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	final, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(final), "corrupted") {
		t.Fatal("control assumption wrong: the original survived")
	}
	// The original user configuration is gone, and no backup exists
	// anywhere: the one copy was never written.
	matches, _ := filepath.Glob(p + ".broken-*")
	if len(matches) != 0 {
		t.Fatalf("a backup exists after all: %v", matches)
	}
	t.Log("CONFIRMED: after a failed backup, the next Save overwrites the user's unrecoverable original — REQ-CFG-040's overwrite gate has no enforcement")
}

// TestVerifyNullValuesAreSilentlyDropped probes the schema_invalid
// boundary: JSON null unmarshals into any Go type without error and
// leaves it zero. The key is neither preserved (no error) nor written
// back (omitempty drops the zero): the file's text for that key is
// silently erased on the first save — exactly what REQ-CFG-070 forbids
// for values the build cannot use.
func TestVerifyNullValuesAreSilentlyDropped(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"editorFontSize": null, "appearance": null, "editorSoftWrap": null}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := Load(p)
	if len(c.Issues()) != 0 {
		t.Fatalf("null values raised issues: %+v (they are silently accepted instead)", c.Issues())
	}
	c.Appearance = "dark" // touch something so Save has meaning
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	raw := verifyFileKeys(t, p)
	for _, key := range []string{"editorFontSize", "editorSoftWrap"} {
		if _, present := raw[key]; present {
			t.Fatalf("%s unexpectedly written back as %s", key, raw[key])
		}
	}
	t.Log("CONFIRMED: explicit null values are silently erased from the file on write-back — neither preserved nor validated")
}

// TestVerifyVersionOfWrongTypeIsNormalizedOnWriteBack documents what
// happens to a version key of the wrong shape: it is preserved in
// extras, but MarshalJSON always writes the int "version" first, so the
// preserved original loses the collision and is rewritten — the same
// omitempty-adjacent hole, here for the reserved key.
func TestVerifyVersionOfWrongTypeIsNormalizedOnWriteBack(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"version": "1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := Load(p)
	if len(c.Issues()) != 1 || c.Issues()[0].Kind != IssueSchemaInvalid || c.Issues()[0].Key != "version" {
		t.Fatalf("issues: %+v", c.Issues())
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	raw := verifyFileKeys(t, p)
	if string(raw["version"]) != "1" {
		t.Fatalf("version rewritten to %s", raw["version"])
	}
	t.Log("CONFIRMED: a wrongly-typed version (string \"1\") is silently normalized to int 1 on write-back despite being preserved in extras")
}

// TestVerifyBackupAccumulation documents that every corrupted load adds
// one more backup file and nothing ever cleans them up.
func TestVerifyBackupAccumulation(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	broken := `{"broken"`
	if err := os.WriteFile(p, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		c := Load(p)
		if len(c.Issues()) != 1 || c.Issues()[0].Backup == "" {
			t.Fatalf("issues: %+v", c.Issues())
		}
	}
	matches, err := filepath.Glob(p + ".broken-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("three corrupted launches left %d backup files; nothing ever removes them", len(matches))
	if len(matches) < 3 {
		t.Fatalf("expected at least 3 backups, got %v", matches)
	}
}
