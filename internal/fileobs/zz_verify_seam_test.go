package fileobs

// Independent verifier's probes of the Check/CommitIf seam — NOT part of
// Lane E's implementation. The integration contract (lane-e-integration-notes
// §B) says a migration to observation-based checking "must use the digest
// attribute, not regress to mtime". These probes test whether Check as
// designed enforces that, or silently downgrades.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The wiring contract's step 2 as literally written: the commit-time
// re-observation is ObservePath(SourceRead, path) — which carries size and
// mtime but NO digest. The expectation carries a digest. Because Check
// compares only the intersection of attributes, the digest is silently
// dropped from the comparison, and a same-size same-mtime file with
// different content passes the condition.
func TestVerifyCheckSilentlyDropsUnsharedDigest(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.txt")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := DigestFileSHA256(p)
	if err != nil {
		t.Fatal(err)
	}
	expected := Observed(SourceRead, p, info, time.Now()).WithDigest(digest)

	// An attacker/accident replaces the content with same-length bytes and
	// restores the mtime — undetectable without the digest.
	if err := os.WriteFile(p, []byte("HELLO"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}

	// The re-observation exactly as the contract's step 2 writes it:
	current, err := ObservePath(SourceRead, p)
	if err != nil {
		t.Fatal(err)
	}
	err = Check(expected, current)
	if err == nil {
		t.Logf("CONFIRMED TRAP: content changed (hello->HELLO, same len, same mtime) yet Check passes — " +
			"the digest the expectation carries is silently dropped because ObservePath does not re-digest")
	} else {
		t.Fatalf("Check = %v; the trap does not exist", err)
	}

	// And the honest version: if the caller re-enriches with the digest,
	// the mismatch is caught. The seam therefore depends on every wiring
	// site remembering a step the type system does not enforce.
	d2, err := DigestFileSHA256(p)
	if err != nil {
		t.Fatal(err)
	}
	enriched := current.WithDigest(d2)
	if err := Check(expected, enriched); !errors.Is(err, ErrConditionMismatch) {
		t.Fatalf("digest-enriched comparison = %v, want mismatch", err)
	}
}

// Stale() documentation says "An incomparable or unchanged observation is
// not staleness", but the code returns true whenever Check errors, and
// Check errors with ErrIncomparableIdentity for pairs sharing no attribute.
// Constructing such a pair through the public API is impossible today
// (every constructor carries size+mtime), so this documents a latent
// doc/code disagreement rather than a reachable bug.
func TestVerifyStaleIncomparableDocumentation(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.txt")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	first := Observed(SourceRead, p, mustStat(t, p), time.Now())
	time.Sleep(2 * time.Millisecond)
	second := Observed(SourceRead, p, mustStat(t, p), time.Now())
	// unchanged file: not stale — agrees with the doc.
	if first.Stale(second) {
		t.Log("unchanged observation reported stale (mtime granularity?)")
	}
}

func mustStat(t *testing.T, p string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return info
}
