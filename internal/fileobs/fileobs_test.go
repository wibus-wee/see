package fileobs

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func stat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestObservationsAreImmutableSnapshots(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.txt")
	writeFile(t, p, "hello")
	base := Observed(SourceRead, p, stat(t, p), time.Now())
	digest, err := DigestFileSHA256(p)
	if err != nil {
		t.Fatal(err)
	}
	enriched := base.WithDigest(digest)
	if _, ok := base.Digest(); ok {
		t.Fatal("enriching a copy mutated the original snapshot")
	}
	if got, ok := enriched.Digest(); !ok || got != digest {
		t.Fatal("enriched copy lost the digest")
	}
	// Two enrichments of one base stay independent of each other.
	other := base.WithETag("v1")
	if _, ok := other.Digest(); ok {
		t.Fatal("one enrichment bled into another")
	}
	if tag, ok := other.ETag(); !ok || tag != "v1" {
		t.Fatal("enrichment did not carry its own attribute")
	}
}

func TestMissingAndEmptyAreDifferentObservations(t *testing.T) {
	root := t.TempDir()
	empty := filepath.Join(root, "empty.txt")
	writeFile(t, empty, "")
	obs := Observed(SourceRead, empty, stat(t, empty), time.Now())
	if !obs.Exists() {
		t.Fatal("an empty file is not a missing file")
	}
	if size, ok := obs.Size(); !ok || size != 0 {
		t.Fatalf("empty file size = %d, %v; want present zero", size, ok)
	}
	missing := Missing(SourceRead, filepath.Join(root, "gone.txt"), time.Now())
	if missing.Exists() {
		t.Fatal("a missing file exists")
	}
	if size, ok := missing.Size(); ok || size != 0 {
		t.Fatal("absence invented a size") // absence carries no attributes
	}
	if err := Check(obs, missing); !errors.Is(err, ErrConditionMismatch) {
		t.Fatalf("empty vs missing = %v, want a mismatch", err)
	}
	both := Missing(SourceRead, missing.Path(), time.Now())
	if err := Check(missing, both); err != nil {
		t.Fatalf("two absences should agree: %v", err)
	}
}

func TestUnknownVersionStateIsNotFabricatedFalse(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.txt")
	writeFile(t, p, "x")
	obs := Observed(SourceGit, p, stat(t, p), time.Now())
	if obs.Current() != TriUnknown || obs.Deleted() != TriUnknown {
		t.Fatal("unknown version state read as a definite answer")
	}
	marker := obs.WithVersion("v7", TriFalse, TriTrue)
	if marker.Current() != TriFalse || marker.Deleted() != TriTrue {
		t.Fatal("version state was not preserved")
	}
	if obs.Current() != TriUnknown {
		t.Fatal("enriching mutated the original")
	}
}

func TestCheckNeverMixesIdentityAttributes(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.txt")
	writeFile(t, p, "hello")
	info := stat(t, p)
	at := time.Now()
	base := Observed(SourceRead, p, info, at)
	digest, err := DigestFileSHA256(p)
	if err != nil {
		t.Fatal(err)
	}
	expected := base.WithDigest(digest)

	// Unchanged file: the condition holds.
	same := Observed(SourceRead, p, stat(t, p), time.Now()).WithDigest(digest)
	if err := Check(expected, same); err != nil {
		t.Fatalf("unchanged file broke the condition: %v", err)
	}

	// Content change with the same length: only the digest catches it.
	writeFile(t, p, "HELLO")
	changed := Observed(SourceRead, p, stat(t, p), time.Now())
	d2, err := DigestFileSHA256(p)
	if err != nil {
		t.Fatal(err)
	}
	changed = changed.WithDigest(d2)
	if err := Check(expected, changed); !errors.Is(err, ErrConditionMismatch) {
		t.Fatalf("changed content passed the condition: %v", err)
	}

	// Tokens of different sources never compare.
	git := Observed(SourceGit, p, stat(t, p), time.Now()).WithDigest(digest)
	if err := Check(expected, git); !errors.Is(err, ErrIncomparableIdentity) {
		t.Fatalf("cross-source comparison = %v, want incomparable", err)
	}
	if errors.Is(err, ErrConditionMismatch) {
		t.Fatal("incomparable tokens masqueraded as a mismatch")
	}

	// Nothing shared: two observations whose attribute sets do not
	// intersect have nothing to compare, which is incomparable rather
	// than a vacuous match. A digest-only source against a size-only
	// one is exactly the mixing REQ-META-040 forbids.
	writeFile(t, p, "hello")
	sized := Observed(SourceRead, p, stat(t, p), time.Now())
	digestOnly := Missing(SourceRead, p, time.Now())
	digestOnly.exists = true
	digestOnly = digestOnly.WithDigest(digest)
	if err := Check(sized, digestOnly); !errors.Is(err, ErrIncomparableIdentity) {
		t.Fatalf("non-intersecting comparison = %v, want incomparable", err)
	}
}

// The regression the wiring contract nearly shipped: the commit-time
// re-observation is digest-less (plain ObservePath), the expectation
// carries the digest, and the content changed in a way only the digest
// catches — same length, mtime restored. Check must refuse to evaluate
// the weakened condition loudly, never pass it silently: a silent pass
// here is a save that overwrites a concurrent writer's bytes while
// reporting the condition held.
func TestCheckRefusesToDropExpectationAttributes(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.txt")
	writeFile(t, p, "hello")
	info := stat(t, p)
	digest, err := DigestFileSHA256(p)
	if err != nil {
		t.Fatal(err)
	}
	expected := Observed(SourceRead, p, info, time.Now()).WithDigest(digest)

	// Same-length replacement with the mtime put back: invisible to
	// size+mtime, visible only to the digest.
	writeFile(t, p, "HELLO")
	if err := os.Chtimes(p, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}

	current, err := ObservePath(SourceRead, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := current.Digest(); ok {
		t.Fatal("the test needs the re-observation digest-less, as plain ObservePath is")
	}
	err = Check(expected, current)
	if !errors.Is(err, ErrIncomparableIdentity) {
		t.Fatalf("digest-less re-observation against digest expectation = %v, want incomparable", err)
	}
	if errors.Is(err, ErrConditionMismatch) {
		t.Fatal("an unevaluated condition must not masquerade as a mismatch either")
	}

	// The honest wiring — the re-observation digests, as ObserveContent
	// does — evaluates at full strength and catches the change.
	strong, err := ObserveContent(SourceRead, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(expected, strong); !errors.Is(err, ErrConditionMismatch) {
		t.Fatalf("digest-carrying comparison = %v, want mismatch", err)
	}
}

// Asymmetry is the contract: the expectation fixes the strength, and a
// current observation that measures more than the expectation asks is
// fully comparable — the extras are unused information, not a weakened
// condition.
func TestCheckIgnoresAttributesBeyondTheExpectation(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.txt")
	writeFile(t, p, "hello")
	weak := Observed(SourceRead, p, stat(t, p), time.Now())
	strong, err := ObserveContent(SourceRead, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(weak, strong); err != nil {
		t.Fatalf("weak expectation against a richer observation = %v, want nil", err)
	}
	if err := Check(strong, weak); !errors.Is(err, ErrIncomparableIdentity) {
		t.Fatalf("the same pair reversed = %v, want incomparable", err)
	}
}

func TestObserveContentCarriesTheDigest(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.txt")
	writeFile(t, p, "hello")
	obs, err := ObserveContent(SourceRead, p)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := DigestFileSHA256(p)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := obs.Digest()
	if !ok || got != digest {
		t.Fatal("ObserveContent lost the digest")
	}
	if size, ok := obs.Size(); !ok || size != 5 {
		t.Fatalf("ObserveContent size = %d, %v", size, ok)
	}
	// Absence is still an observation, not an error, and carries nothing.
	missing, err := ObserveContent(SourceRead, filepath.Join(root, "gone.txt"))
	if err != nil || missing.Exists() {
		t.Fatalf("absent file = %v, %v", missing.Exists(), err)
	}
	// A broken path — a child of a file — is a failure, not an observation.
	if _, err := ObserveContent(SourceRead, filepath.Join(p, "child")); err == nil {
		t.Fatal("a broken path produced an observation")
	}
	// Both ends digest-carrying: unchanged content holds the condition.
	again, err := ObserveContent(SourceRead, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(obs, again); err != nil {
		t.Fatalf("unchanged content broke a digest-strength condition: %v", err)
	}
}

func TestStaleRequiresLaterObservationOfSameSubject(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.txt")
	writeFile(t, p, "one")
	first := Observed(SourceRead, p, stat(t, p), time.Now())
	time.Sleep(2 * time.Millisecond)
	writeFile(t, p, "two")
	second := Observed(SourceRead, p, stat(t, p), time.Now())
	if !first.Stale(second) {
		t.Fatal("an older observation was not stale once content moved on")
	}
	if second.Stale(first) {
		t.Fatal("the newer observation cannot be stale against the older")
	}
	otherPath := Observed(SourceRead, filepath.Join(root, "b.txt"), stat(t, p), time.Now())
	if first.Stale(otherPath) {
		t.Fatal("another path's observation stales this one")
	}
}

func TestObservePathSeparatesAbsenceFromFailure(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.txt")
	writeFile(t, p, "hi")
	obs, err := ObservePath(SourceRead, p)
	if err != nil || !obs.Exists() {
		t.Fatalf("existing file = %v, %v", obs.Exists(), err)
	}
	missing, err := ObservePath(SourceRead, filepath.Join(root, "gone.txt"))
	if err != nil || missing.Exists() {
		t.Fatalf("absent file = %v, %v; absence is an observation, not an error", missing.Exists(), err)
	}
	// A path under a file is a filesystem failure: nothing is observed.
	if _, err := ObservePath(SourceRead, filepath.Join(p, "child")); err == nil {
		t.Fatal("a broken path produced an observation")
	}
}

func TestVerifyDigestCategories(t *testing.T) {
	content := []byte("the staged bytes")
	digest, err := DigestSHA256(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyContentDigest(content, digest); err != nil {
		t.Fatalf("matching content rejected: %v", err)
	}
	var wrong [32]byte
	err = VerifyContentDigest(content, wrong)
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("mismatch = %v, want ErrDigestMismatch", err)
	}

	// A read failure keeps its own category instead of reading as corruption.
	broken := &failReader{err: errors.ErrUnsupported}
	if err := VerifyDigest(broken, nil, digest); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("read failure = %v, want the read error itself", err)
	}

	// Bytes pass through the sink even when they turn out wrong; the
	// mismatch is still reported, which is why publishing requires
	// staging first.
	var sink bytes.Buffer
	if err := VerifyDigest(bytes.NewReader(content), &sink, wrong); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("streamed mismatch = %v", err)
	}
	if !bytes.Equal(sink.Bytes(), content) {
		t.Fatal("the sink did not receive the streamed bytes")
	}
}

type failReader struct{ err error }

func (r *failReader) Read([]byte) (int, error) { return 0, r.err }

var _ io.Reader = (*failReader)(nil)

func TestParseHexDigestFailsClosed(t *testing.T) {
	digest, err := DigestSHA256(bytes.NewReader([]byte("x")))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseHexDigest(HexDigest(digest))
	if err != nil || parsed != digest {
		t.Fatalf("round trip = %v, %v", parsed, err)
	}
	for _, bad := range []string{"", "abcd", HexDigest(digest) + "00", "zz" + HexDigest(digest)[2:]} {
		if _, err := ParseHexDigest(bad); err == nil {
			t.Fatalf("%q parsed as a digest", bad)
		}
	}
}
