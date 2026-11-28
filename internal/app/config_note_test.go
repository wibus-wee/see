package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"see/internal/config"
)

// The startup surface of a degraded configuration: the note the welcome
// screen shows names what was reset, and the original file survived with
// its backup beside it.
func TestConfigLoadNoteSurfacesCorruption(t *testing.T) {
	broken := `{"appearance": "dark", corrupted`
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load(p)
	note := configLoadNote(cfg)
	if note == "" {
		t.Fatal("a corrupted configuration produced no startup note")
	}
	if !strings.Contains(note, string(config.IssueParseFailed)) {
		t.Fatalf("the note does not say what happened: %q", note)
	}
	if clean := configLoadNote(config.Load(filepath.Join(t.TempDir(), "absent.json"))); clean != "" {
		t.Fatalf("a clean configuration produced a note: %q", clean)
	}
	issues := cfg.Issues()
	if len(issues) != 1 || issues[0].Backup == "" {
		t.Fatalf("issues: %+v", issues)
	}
	backup, err := os.ReadFile(issues[0].Backup)
	if err != nil || string(backup) != broken {
		t.Fatalf("backup = %q err=%v", backup, err)
	}
	original, err := os.ReadFile(p)
	if err != nil || string(original) != broken {
		t.Fatalf("original = %q err=%v", original, err)
	}
}
