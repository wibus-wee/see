package config

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFailedBackupBlocksOverwriteUntilBackupSucceeds is the regression
// test for the overwrite-gate violation found in independent
// verification: when the backup of a corrupted configuration fails,
// nothing used to stop the next Save from atomically replacing the only
// copy of the user's original file. The contract allows overwriting it
// only after the backup completes, so Save must refuse while the
// original is unprotected — and must heal, completing backup and
// overwrite, when the transient failure is over.
func TestFailedBackupBlocksOverwriteUntilBackupSucceeds(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	broken := `{"editorFontSize": 16, corrupted`
	if err := os.WriteFile(p, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	// The backup write fails as it does when the configuration directory
	// cannot be written right now — a full disk, a transient permission
	// problem. No file system shape expresses that state on every
	// platform (POSIX mode bits do not bind root, Windows has none that
	// deny creating a file in a directory), so the failure is injected
	// at the backup write itself.
	realWrite := writeBackupFile
	t.Cleanup(func() { writeBackupFile = realWrite })
	writeBackupFile = func(string, os.FileMode, func(io.Writer) error) error {
		return errors.New("injected: the backup directory is unwritable")
	}
	c := Load(p)
	issues := c.Issues()
	if len(issues) != 1 || issues[0].Kind != IssueParseFailed {
		t.Fatalf("setup: %+v", issues)
	}
	if issues[0].Backup != "" {
		t.Fatalf("setup: backup unexpectedly succeeded at %s", issues[0].Backup)
	}

	// While the directory stays unwritable the overwrite is refused:
	// the original — the only copy of what the user had — survives.
	c.Appearance = "dark" // any setting change; in the app this is one click
	if err := c.Save(); err == nil {
		t.Fatal("the overwrite went ahead with no backup anywhere")
	}
	original, err := os.ReadFile(p)
	if err != nil || string(original) != broken {
		t.Fatalf("the refused save touched the only copy: %q err=%v", original, err)
	}

	// The transient failure passes, as a freed disk or fixed permission
	// does. The next save completes the backup first, then overwrites:
	// at no moment was the original unprotected.
	writeBackupFile = realWrite
	if err := c.Save(); err != nil {
		t.Fatalf("the save did not heal after the transient failure passed: %v", err)
	}
	matches, _ := filepath.Glob(p + ".broken-*")
	if len(matches) != 1 {
		t.Fatalf("expected exactly one backup after the healing save, got %v", matches)
	}
	backed, err := os.ReadFile(matches[0])
	if err != nil || string(backed) != broken {
		t.Fatalf("the backup does not hold the original bytes: %q err=%v", backed, err)
	}
	final, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(final), "corrupted") || !strings.Contains(string(final), `"dark"`) {
		t.Fatalf("the overwrite did not publish the new settings: %s", final)
	}
	// And the issue now knows where the backup landed, for the user the
	// startup note sends looking.
	if c.Issues()[0].Backup != matches[0] {
		t.Fatalf("the issue does not carry the completed backup: %+v", c.Issues()[0])
	}

	// Once protected, saving stays ordinary: no second backup appears.
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if again, _ := filepath.Glob(p + ".broken-*"); len(again) != 1 {
		t.Fatalf("a healthy save wrote another backup: %v", again)
	}
}

// TestSuccessfulBackupNeedsNoGate: when the backup completes at load
// time, the original is protected from the start and Save proceeds
// without any backup dance.
func TestSuccessfulBackupNeedsNoGate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	broken := `{"broken"`
	if err := os.WriteFile(p, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	c := Load(p)
	if len(c.Issues()) != 1 || c.Issues()[0].Backup == "" {
		t.Fatalf("setup: %+v", c.Issues())
	}
	c.Appearance = "dark"
	if err := c.Save(); err != nil {
		t.Fatalf("a protected original should not gate saving: %v", err)
	}
	matches, _ := filepath.Glob(p + ".broken-*")
	if len(matches) != 1 {
		t.Fatalf("the load-time backup should be the only one: %v", matches)
	}
}
