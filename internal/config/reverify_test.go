package config

// Independent re-verification tests (branch reverify-lane-c). These probe
// scenarios the fixer's own suite does not cover, written by the
// re-verifier without consulting the fixer. No implementation code is
// touched.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The F-1 semantics adjudication scenario: the user hand-writes an invalid
// enum, opens the app, changes nothing, and the first unrelated save
// happens (recents, session capture). The contract says the file converges
// to the in-memory truth — purple is dropped without any repair gesture.
func TestReverifyUnrelatedSaveConvergesInvalidEnumWithoutUserTouchingIt(t *testing.T) {
	p := writeConfig(t, `{"appearance": "purple", "editorFontSize": 16}`)
	c := Load(p)
	if len(c.Issues()) != 1 || c.Issues()[0].Kind != IssueValidationFailed {
		t.Fatalf("setup: %+v", c.Issues())
	}
	// No repair gesture: the user never touches appearance. An unrelated
	// change forces a save, the way opening a workspace does in the app.
	c.AddRecent("/tmp/some-workspace")
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	raw := readFileKeys(t, p)
	if v, present := raw["appearance"]; present {
		t.Fatalf("purple survived an unrelated save: %s", v)
	}
	if string(raw["editorFontSize"]) != "16" {
		t.Fatalf("the valid neighbor changed: %s", raw["editorFontSize"])
	}
	// The file healed: next launch reports nothing, and the user's
	// in-effect setting (system appearance) is what the file now says.
	if issues := Load(p).Issues(); len(issues) != 0 {
		t.Fatalf("the file did not heal: %+v", issues)
	}
}

// The accepted cost, verified as real: a value valid only in a newer
// build's widened enum is lost on the first save of the older build, even
// when the user never touches the setting. The matrix claims this cost is
// documented; this test pins that the behavior is exactly the documented
// one — the in-memory default replaces it, nothing else is damaged.
func TestReverifyEnumWideningDowngradeLosesNewerValue(t *testing.T) {
	p := writeConfig(t, `{"version": 99, "appearance": "solarized-auto", "newInV99": {"x": 1}}`)
	c := Load(p)
	// version_newer AND validation_failed both expected.
	var sawNewer, sawValidation bool
	for _, i := range c.Issues() {
		sawNewer = sawNewer || i.Kind == IssueVersionNewer
		sawValidation = sawValidation || i.Kind == IssueValidationFailed
	}
	if !sawNewer || !sawValidation {
		t.Fatalf("setup: %+v", c.Issues())
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	raw := readFileKeys(t, p)
	if _, present := raw["appearance"]; present {
		t.Fatalf("the newer enum value survived: %s", raw["appearance"])
	}
	// The unknown v99 key and the version declaration DO survive — the
	// asymmetry the contract documents.
	if string(raw["version"]) != "99" {
		t.Fatalf("version declaration downgraded: %s", raw["version"])
	}
	var nested map[string]int
	if err := json.Unmarshal(raw["newInV99"], &nested); err != nil || nested["x"] != 1 {
		t.Fatalf("the unknown newer key was lost: %s err=%v", raw["newInV99"], err)
	}
}

// The asymmetry the expressibility rule does NOT close: a type-mismatched
// invalid value is preserved forever, so the file cannot heal through the
// app for schema-invalid keys — the warning repeats on every launch while
// memory already runs the default. Pinned as documented behavior; the
// re-verification assesses whether the matrix discloses it.
func TestReverifyTypeMismatchNeverHeals(t *testing.T) {
	p := writeConfig(t, `{"editorFontSize": "sixteen"}`)
	c := Load(p)
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	again := Load(p)
	if len(again.Issues()) != 1 || again.Issues()[0].Kind != IssueSchemaInvalid {
		t.Fatalf("type-mismatched values stay preserved, so the warning repeats: %+v", again.Issues())
	}
	if again.FontSize != 0 {
		t.Fatalf("memory runs the default while the file says otherwise: %v", again.FontSize)
	}
}

// hiddenNames is the one struct field without omitempty: it always emits
// into the marshaled map, so a preserved type-mismatched original loses
// the collision and is silently dropped — contradicting the contract that
// type-mismatched values are preserved verbatim. This test documents what
// the implementation actually does; if it fails, the contract holds and
// this scenario is fine.
func TestReverifyHiddenNamesTypeMismatchCollision(t *testing.T) {
	p := writeConfig(t, `{"hiddenNames": 42}`)
	c := Load(p)
	if len(c.Issues()) != 1 || c.Issues()[0].Kind != IssueSchemaInvalid {
		t.Fatalf("setup: %+v", c.Issues())
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	raw := readFileKeys(t, p)
	if string(raw["hiddenNames"]) == "42" {
		t.Log("type-mismatched hiddenNames preserved — contract holds for the non-omitempty field")
		return
	}
	t.Logf("CONFIRMED GAP: type-mismatched hiddenNames was overwritten with %s — the non-omitempty field always wins the collision, so the preservation contract does not cover it", raw["hiddenNames"])
}

// F-2 gate, variant A: the user hand-repairs the corrupted file between
// Load and Save. The gate must still back up the bytes Load saw — the only
// copy it knows — and the overwrite then replaces the user's manual repair
// (the documented no-reload snapshot limitation, not a gate defect).
func TestReverifyGateBacksUpLoadBytesEvenWhenFileChangedSince(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root writes through permission bits")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	broken := `{"editorFontSize": 16, corrupted`
	if err := os.WriteFile(p, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	c := Load(p)
	if len(c.Issues()) != 1 || c.Issues()[0].Backup != "" {
		t.Fatalf("setup: %+v", c.Issues())
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// The user hand-repairs the file while the app runs.
	manualRepair := `{"appearance": "light"}`
	if err := os.WriteFile(p, []byte(manualRepair), 0o600); err != nil {
		t.Fatal(err)
	}
	c.Appearance = "dark"
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(p + ".broken-*")
	if len(matches) != 1 {
		t.Fatalf("expected exactly one backup, got %v", matches)
	}
	backed, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	// What the backup must hold is the corrupted bytes Load saw — the only
	// copy the gate is responsible for — not the manual repair.
	if string(backed) != broken {
		t.Fatalf("backup holds %q, want the corrupted bytes Load saw %q", backed, broken)
	}
	// And the manual repair is overwritten by the snapshot (documented
	// snapshot semantics of REQ-CFG-090): the save must not have failed
	// silently, and the file now holds the app's truth.
	final, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(final), `"dark"`) {
		t.Fatalf("the snapshot did not publish: %s", final)
	}
}

// F-2 gate, variant B: the backup succeeds at Save time but the main
// write then fails (directory goes unwritable between the two). The
// original must survive, the backup exists, and a later save heals. This
// is the no-unprotected-moment invariant under mid-save failure.
func TestReverifyGateSurvivesMainWriteFailureAfterBackup(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root writes through permission bits")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	broken := `{"x": broken`
	if err := os.WriteFile(p, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	c := Load(p)
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Race-free simulation of "backup lands, then the filesystem turns
	// hostile": cannot intercept mid-Save, so instead verify the two
	// halves independently. Half one: backup+overwrite heal together when
	// the filesystem stays friendly (covered by the fixer's test). Half
	// two, here: a Save whose main write fails leaves the original bytes
	// untouched, because the write is atomic — tested by making the main
	// write impossible while the backup is already done.
	if err := c.Save(); err != nil {
		t.Fatalf("the healing save failed: %v", err)
	}
	matches, _ := filepath.Glob(p + ".broken-*")
	if len(matches) != 1 {
		t.Fatalf("backup missing after heal: %v", matches)
	}
	// Now corrupt the environment for the NEXT save: the file on disk is
	// healthy JSON now, so this is the plain atomic-write guarantee.
	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	c.Appearance = "light"
	saveErr := c.Save()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if saveErr == nil {
		t.Fatal("save into an unwritable directory succeeded")
	}
	after, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("a failed save changed the file:\nbefore: %s\nafter:  %s", before, after)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("staging left behind: %s", e.Name())
		}
	}
}

// The empty-file edge: an empty config is a parse failure whose bytes are
// empty. When the backup fails at load, unprotected holds a zero-length
// slice, len(unprotected) == 0 reads as "nothing to protect", and the gate
// never engages: the save overwrites with no backup anywhere. Lossless in
// practice (an empty file carries no settings) — but the gate's own
// invariant, "never overwrite the only copy", is keyed on byte length,
// which this test pins down exactly.
func TestReverifyEmptyFileParseFailureGate(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root writes through permission bits")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	c := Load(p)
	if len(c.Issues()) != 1 || c.Issues()[0].Kind != IssueParseFailed {
		t.Fatalf("setup: %+v", c.Issues())
	}
	if c.Issues()[0].Backup != "" {
		t.Fatalf("setup: backup unexpectedly succeeded")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	c.Appearance = "dark"
	if err := c.Save(); err != nil {
		t.Fatalf("save refused or failed for the empty original: %v", err)
	}
	matches, _ := filepath.Glob(p + ".broken-*")
	if len(matches) == 0 {
		t.Log("CONFIRMED EDGE: the empty original was overwritten with no backup anywhere — the gate keys on len(bytes), and empty bytes read as nothing-to-protect. Lossless in practice.")
	} else {
		t.Logf("the empty original was backed up at %v", matches)
	}
	final, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(final), `"dark"`) {
		t.Fatalf("save did not publish: %s", final)
	}
}

// A whitespace-only file IS protected: its bytes are non-empty. Pins that
// the gate keys on byte length, not on information content.
func TestReverifyWhitespaceFileIsProtected(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root writes through permission bits")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	c := Load(p)
	if len(c.Issues()) != 1 || c.Issues()[0].Kind != IssueParseFailed {
		t.Fatalf("setup: %+v", c.Issues())
	}
	c.Appearance = "dark"
	saveErr := c.Save()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if saveErr == nil {
		t.Fatal("the whitespace original was overwritten without a backup")
	}
	data, err := os.ReadFile(p)
	if err != nil || string(data) != "  \n" {
		t.Fatalf("the refused save touched the original: %q err=%v", data, err)
	}
}

// Variant of the dispatcher's "permissions change mid-save" probe: while a
// healing Save runs (backup write + overwrite), a concurrent goroutine
// flips the directory's writability back and forth. Outcomes vary — the
// point is the invariants: the file is never corrupted or emptied, and
// when the save succeeds the original bytes are backed up somewhere.
// Statistical, not deterministic: the window is small, so failures of the
// invariants would surface rarely but never falsely pass a broken gate.
func TestReverifyGateUnderRacingPermissionFlips(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root writes through permission bits")
	}
	for range 30 {
		dir := t.TempDir()
		p := filepath.Join(dir, "config.json")
		broken := `{"x": corrupted`
		if err := os.WriteFile(p, []byte(broken), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		c := Load(p)
		if len(c.Issues()) != 1 || c.Issues()[0].Backup != "" {
			t.Fatalf("setup: %+v", c.Issues())
		}
		stop := make(chan struct{})
		go func() {
			writable := true
			for {
				select {
				case <-stop:
					return
				default:
					if writable {
						_ = os.Chmod(dir, 0o500)
					} else {
						_ = os.Chmod(dir, 0o700)
					}
					writable = !writable
				}
			}
		}()
		c.Appearance = "dark"
		saveErr := c.Save()
		close(stop)
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		final, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if len(final) == 0 {
			t.Fatal("the file was left empty")
		}
		if saveErr == nil {
			// A successful overwrite must never have been unprotected: the
			// original bytes are in a backup.
			if string(final) == broken {
				t.Fatal("the save reported success but nothing changed")
			}
			matches, _ := filepath.Glob(p + ".broken-*")
			found := false
			for _, m := range matches {
				b, _ := os.ReadFile(m)
				if string(b) == broken {
					found = true
				}
			}
			if !found {
				t.Fatalf("the overwrite succeeded with no backup of the original anywhere: %v", matches)
			}
		} else {
			// A refused or failed save must have left the original untouched
			// — unless an earlier iteration's chmod flip let the backup
			// through but the main write failed; either way the original
			// bytes survive in the file itself or in a backup.
			if string(final) != broken {
				matches, _ := filepath.Glob(p + ".broken-*")
				found := false
				for _, m := range matches {
					b, _ := os.ReadFile(m)
					if string(b) == broken {
						found = true
					}
				}
				if !found {
					t.Fatalf("save failed AND the original bytes are gone: file=%q backups=%v", final, matches)
				}
			}
		}
	}
}

// F-6 refutation check, independently observed: an existing 0640 config
// keeps 0640 through Save, and a 0600 config keeps 0600. The original
// verifier claimed overwrite normalizes to 0600.
func TestReverifyConfigModePreservedOnOverwrite(t *testing.T) {
	for _, mode := range []os.FileMode{0o640, 0o644, 0o600} {
		dir := t.TempDir()
		p := filepath.Join(dir, "config.json")
		if err := os.WriteFile(p, []byte(`{"appearance": "dark"}`), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil { // umask may have narrowed creation
			t.Fatal(err)
		}
		c := Load(p)
		c.Appearance = "light"
		if err := c.Save(); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Fatalf("mode %#o became %#o after overwrite", mode, info.Mode().Perm())
		}
	}
	// And a NEW config file gets 0600 as documented.
	p := filepath.Join(t.TempDir(), "config.json")
	c := Load(p)
	c.Appearance = "dark"
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("a new config file has mode %#o, want 0600", info.Mode().Perm())
	}
}
