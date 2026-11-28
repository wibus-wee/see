package workspace

// Adversarial verification tests for SPEC-006, written by the independent
// verifier (branch verify-lane-b) and ported onto spec-lane-b-paths as
// regression coverage when the defects they demonstrated were fixed. They
// probe behaviors Lane B's own tests do not cover: sensitive-file-system
// adoption rules (on a real case-sensitive volume when SEE_VERIFY_CS_VOLUME
// is set), Unicode full-folding gaps, escape variants, and confinement
// edge inputs.

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestAdversarialEscapeVariants extends the confinement matrix with inputs
// the implementer did not test: URL-encoded dots (must be literal names at
// this layer, never decoded), Unicode full-width dots, deep balancing,
// backslashes on POSIX (a literal character, not a separator).
func TestAdversarialEscapeVariants(t *testing.T) {
	root := filepath.FromSlash("/w/project")
	// These must all be rejected as escapes or absolutes:
	for _, rel := range []string{
		"..", "../x", "a/../../x", "x/../../../..",
		"/", "/a", "//server/share",
		"a/../..", "..\\x", // backslash dotdot: on POSIX "\\" is literal so "..\\x" is a FILE NAME — see below
	} {
		_, err := JoinRel(root, rel)
		if rel == "..\\x" {
			// On POSIX this is a legitimate file named `..\x` — NOT an
			// escape. It must NOT be rejected here; document actual behavior.
			if err != nil {
				t.Logf("note: %q rejected on POSIX though it is a literal file name there", rel)
			}
			continue
		}
		if err == nil {
			t.Errorf("JoinRel(%q) was not rejected", rel)
		}
	}
	// URL-encoded traversal is a literal file name at this layer: it must
	// join INSIDE the root as a literal name (no decoding happens here),
	// which is safe — a file named %2e%2e is not the parent.
	got, err := JoinRel(root, "%2e%2e/evil.txt")
	if err != nil {
		t.Errorf("url-encoded input should be a literal name, got err %v", err)
	} else if filepath.Dir(got) != filepath.Join(root, "%2e%2e") {
		t.Errorf("url-encoded input joined to %q", got)
	}
	// Unicode full-width dots are literal characters, not traversal.
	got, err = JoinRel(root, "\uff0e\uff0e/evil.txt")
	if err != nil {
		t.Errorf("full-width dots should be a literal name, got err %v", err)
	} else if !Within(root, got) {
		t.Errorf("full-width dots escaped to %q", got)
	}
	// Balanced traversal that dips and returns stays inside.
	got, err = JoinRel(root, "a/b/../../c")
	if err != nil || got != filepath.Join(root, "c") {
		t.Errorf("balanced traversal = %q, %v", got, err)
	}
	// Trailing separators and dots.
	if got, err := JoinRel(root, "a/./"); err != nil || got != filepath.Join(root, "a") {
		t.Errorf("trailing dot = %q, %v", got, err)
	}
}

// TestAdversarialTrailingDotComponents pins the resolution of the
// Windows hazard the verifier demonstrated: a component like `.. `
// (dotdot trailing space) is lexically inside the root, but the Win32
// file namespace strips trailing spaces and dots from components at
// open time, so `.. ` opens as `..` and the join escapes. As the strip
// is a property of the volume's namespace, not the OS the app runs on —
// a POSIX client of an SMB or exFAT volume shares it — the confined
// join refuses every component ending in a dot or a space on every
// platform. POSIX file systems carry such names literally, so this is
// a superset rejection there; trailing-dot and trailing-space names are
// absent from ordinary projects, and impossible to create on Windows.
func TestAdversarialTrailingDotComponents(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{
		".. /x.txt",    // the escape: strips to `..` under Win32 rules
		"notes.",       // strips to `notes`, another name than joined
		"notes./x.txt", // the strip in an intermediate component
		"file ",
		"dir /file.txt",
		"...",
		" . ",
	} {
		if got, err := JoinRel(root, rel); err == nil {
			t.Errorf("JoinRel(%q) = %q, want refusal: Win32 would strip the trailing dot/space", rel, got)
		}
		if ValidRel(rel) {
			t.Errorf("ValidRel(%q) = true, want false", rel)
		}
	}
	// Honest names keep joining: dots and spaces inside a component are
	// fine, only a trailing one is unrepresentable under Win32 rules.
	for _, rel := range []string{"a.b/file.txt", "my file.txt", ".hidden", "sub/.hidden/x.txt"} {
		if _, err := JoinRel(root, rel); err != nil {
			t.Errorf("JoinRel(%q) refused an honest name: %v", rel, err)
		}
		if !ValidRel(rel) {
			t.Errorf("ValidRel(%q) = false, want true", rel)
		}
	}
}

// TestAdversarialSensitiveVolume runs only when SEE_VERIFY_CS_VOLUME names
// a case-sensitive, normalization-preserving file system (a mounted
// case-sensitive APFS sparseimage on the verification machine). It is the
// evidence Lane B could not produce on a merging filesystem: fold-equal
// candidates must never be adopted where the filesystem keeps them
// distinct.
func TestAdversarialSensitiveVolume(t *testing.T) {
	cs := os.Getenv("SEE_VERIFY_CS_VOLUME")
	if cs == "" {
		t.Skip("SEE_VERIFY_CS_VOLUME not set")
	}
	probe := filepath.Join(cs, "probe-distinct-check.txt")
	writeFile(t, probe, "x")
	if _, err := os.Lstat(filepath.Join(cs, "PROBE-DISTINCT-CHECK.TXT")); err == nil {
		t.Skipf("%s is not case-sensitive; refusing to validate the sensitive branch on it", cs)
	}
	dir := filepath.Join(cs, "adv")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// 1. foo.txt and Foo.txt both exist; input FOO.TXT must not merge into either.
	writeFile(t, filepath.Join(dir, "foo.txt"), "lower\n")
	writeFile(t, filepath.Join(dir, "Foo.txt"), "upper\n")
	in := filepath.Join(dir, "FOO.TXT")
	if got := DiskSpelling(in); got != in {
		t.Errorf("FOO.TXT with foo.txt+Foo.txt present: merged into %q; on a sensitive fs it names nothing and must stand", got)
	}
	// 2. Only Foo.txt exists; input foo.txt names nothing and must stand.
	solo := filepath.Join(cs, "adv-solo")
	os.MkdirAll(solo, 0o755)
	defer os.RemoveAll(solo)
	writeFile(t, filepath.Join(solo, "Foo.txt"), "upper\n")
	in2 := filepath.Join(solo, "foo.txt")
	if got := DiskSpelling(in2); got != in2 {
		t.Errorf("foo.txt with only Foo.txt present: adopted %q; sensitive fs must keep the input", got)
	}
	// 3. Exact spelling round-trips.
	if got := DiskSpelling(filepath.Join(dir, "foo.txt")); got != filepath.Join(dir, "foo.txt") {
		t.Errorf("exact lower spelling rewritten to %q", got)
	}
	// 4. Unicode: write NFC and NFD é files. If the volume keeps them
	// distinct, exact input must round-trip; either spelling of one must
	// never adopt the other.
	uni := filepath.Join(cs, "adv-uni")
	os.MkdirAll(uni, 0o755)
	defer os.RemoveAll(uni)
	nfc := "caf\u00e9.txt"
	nfd := "cafe\u0301.txt"
	writeFile(t, filepath.Join(uni, nfc), "nfc\n")
	writeFile(t, filepath.Join(uni, nfd), "nfd\n")
	entries, _ := os.ReadDir(uni)
	t.Logf("unicode entries on volume: %d", len(entries))
	if len(entries) == 2 {
		if got := DiskSpelling(filepath.Join(uni, nfc)); filepath.Base(got) != nfc {
			t.Errorf("exact NFC input rewritten: base bytes %x", []byte(filepath.Base(got)))
		}
		if got := DiskSpelling(filepath.Join(uni, nfd)); filepath.Base(got) != nfd {
			t.Errorf("exact NFD input rewritten: base bytes %x", []byte(filepath.Base(got)))
		}
	} else {
		t.Logf("volume merges normalization variants; the single-entry semantic is the merging case, already covered")
	}
}

// TestAdversarialFullFoldDivergence is the regression test of the
// case-fold table mismatch (verify-lane-b.md F1): simple case mapping
// (strings.ToLower) folds less than file systems do, so spellings the
// disk merges — Turkish İ vs i+U+0307 — would not converge, splitting
// one file into two documents. foldKey now full-folds with cases.Fold,
// as APFS and NTFS do; the verifier's scan of all 1010 cased BMP runes
// found 29 affected characters (U+0130, U+1E9E, the U+1F88–U+1FAF,
// U+1FBC, U+1FCC, U+1FFC Greek prosgegrammeni blocks) and no false
// merges in the reverse direction.
func TestAdversarialFullFoldDivergence(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "\u0130test.txt") // İtest.txt
	writeFile(t, target, "x\n")
	// The full case fold of U+0130 is U+0069 U+0307 (i + combining dot above).
	variant := filepath.Join(dir, "i\u0307test.txt")
	fi1, err1 := os.Lstat(target)
	fi2, err2 := os.Lstat(variant)
	merges := err1 == nil && err2 == nil && os.SameFile(fi1, fi2)
	foldEq := FoldEqual("\u0130test.txt", "i\u0307test.txt")
	t.Logf("fs merges İ / i+dot-above: %v; FoldEqual: %v", merges, foldEq)
	if merges && !foldEq {
		got := DiskSpelling(variant)
		if filepath.Base(got) == "\u0130test.txt" {
			t.Logf("converges despite FoldEqual mismatch — unexpected, inspect")
		} else {
			t.Errorf("DIVERGENCE CONFIRMED: the filesystem merges %q and %q into one file, but DiskSpelling(%q) = %q does not converge: both spellings open as separate documents of one file",
				"\u0130test.txt", "i\u0307test.txt", "i\u0307test.txt", filepath.Base(got))
		}
	}
	if merges && foldEq {
		if got := DiskSpelling(variant); filepath.Base(got) != "\u0130test.txt" {
			t.Errorf("merging fs + fold-equal yet no convergence: %q", filepath.Base(got))
		}
	}
}

// fsMerges probes what the file system of dir does with a file reached
// by a variant spelling of its name: true when the variant stats to the
// file, as a case-insensitive or normalization-insensitive file system
// answers. The file systems the app runs on answer both ways — APFS and
// NTFS merge, ext4 keeps the spellings distinct — so a test asserts the
// behavior of the answer it measured, never the platform's name.
func fsMerges(t *testing.T, dir, name, variant string) bool {
	t.Helper()
	probe := filepath.Join(dir, name)
	writeFile(t, probe, "x")
	fi1, err1 := os.Lstat(probe)
	fi2, err2 := os.Lstat(filepath.Join(dir, variant))
	// SameFile must be answered while the probe still exists: on Windows
	// it opens the file by its remembered path to compare volume and
	// file index, and a removed probe reads as a false negative — the
	// one error os.SameFile cannot distinguish from distinct files.
	same := err1 == nil && err2 == nil && os.SameFile(fi1, fi2)
	_ = os.Remove(probe)
	t.Logf("fsMerges(%q, %q): lstat probe %v, lstat variant %v, same %v", name, variant, err1, err2, same)
	return same
}

// TestAdversarialTOCTOURenameBetweenProbeAndOpen documents the time-of-check
// hazard: after DiskSpelling resolves a spelling, an external case-rename
// makes the stored identity stale. What the old spelling then resolves to
// is the file system's own answer, so the test probes it: where spellings
// merge, the file still answers the old spelling and a fresh probe
// converges to the NEW spelling — two keys for one file until something
// reconciles; where spellings are distinct, the renamed-away spelling names
// nothing at all, so it stands as typed and opens as an absence. Both are
// the contract of DiskSpelling; neither may adopt a spelling the file
// system itself would not resolve. This is inherent to any lexical
// identity; the test measures what the implementation leaves stale.
func TestAdversarialCaseRenameReopenConverges(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Rename.txt"), "x\n")
	first := DiskSpelling(filepath.Join(dir, "Rename.txt"))
	// External case-only rename, as a user or tool does in a shell.
	if err := os.Rename(filepath.Join(dir, "Rename.txt"), filepath.Join(dir, "rename.txt")); err != nil {
		t.Fatalf("case rename unsupported here: %v", err)
	}
	second := DiskSpelling(filepath.Join(dir, "Rename.txt")) // old spelling typed again
	t.Logf("before rename: %q; after rename old input resolves to %q", first, second)
	if fsMerges(t, dir, "merge-probe.txt", "MERGE-PROBE.TXT") {
		if filepath.Base(second) != "rename.txt" {
			t.Errorf("after the rename the old spelling should converge to the disk spelling rename.txt, got %q", filepath.Base(second))
		}
		return
	}
	// Distinct spellings: rename.txt and Rename.txt are different names
	// now, the old one names nothing, and nothing else may be adopted
	// for it. The probe must return the input as typed.
	if second != filepath.Join(dir, "Rename.txt") {
		t.Errorf("after the rename the old spelling names nothing on this file system; it must stand as typed, got %q adopting another file", second)
	}
	// And the new exact spelling round-trips unchanged.
	if got := DiskSpelling(filepath.Join(dir, "rename.txt")); filepath.Base(got) != "rename.txt" {
		t.Errorf("exact new spelling rewritten to %q", filepath.Base(got))
	}
}
