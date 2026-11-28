package fileobs

// Independent re-verification probes for the fail-closed Check respin
// (commit 5bb38f4). Written by the re-verifier, not the implementer: the
// attack is reproduced from the verification report's description, the
// pre-fix comparison is reconstructed from git history to confirm the
// trap existed and is now closed, and the concurrency probe discriminates
// the same-file-object constructor from the stat-then-open shape it
// claims not to be.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// checkIntersectionOnly is the pre-fix Check verbatim, reconstructed
// from internal/fileobs/fileobs.go at e593858. It exists to prove the
// attack passed it silently — otherwise "the fix closes the trap" is
// asserted against a trap that may never have existed.
func checkIntersectionOnly(expected, current Observation) error {
	if expected.source != current.source {
		return fmt.Errorf("%w: %q tokens cannot be compared with %q tokens",
			ErrIncomparableIdentity, expected.source, current.source)
	}
	if expected.exists != current.exists {
		return ErrConditionMismatch
	}
	if !expected.exists {
		return nil
	}
	shared := expected.attrs & current.attrs
	if shared == 0 {
		return fmt.Errorf("%w: no shared attribute", ErrIncomparableIdentity)
	}
	if shared&hasSize != 0 && expected.size != current.size {
		return ErrConditionMismatch
	}
	if shared&hasMtime != 0 && !expected.mtime.Equal(current.mtime) {
		return ErrConditionMismatch
	}
	if shared&hasDigest != 0 && expected.digest != current.digest {
		return ErrConditionMismatch
	}
	if shared&hasETag != 0 && expected.etag != current.etag {
		return ErrConditionMismatch
	}
	if shared&hasVersion != 0 && expected.version != current.version {
		return ErrConditionMismatch
	}
	return nil
}

func writeProbeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The verifier's attack, reproduced independently: the attacker replaces
// the file with different bytes of the same length and restores the
// mtime. Against a digest-carrying expectation and a digest-less commit
// re-observation, the pre-fix intersection Check passed silently — nil,
// the condition "holds", the save proceeds over the attacker's bytes.
// The respin must instead fail loudly as incomparable, and the honestly
// wired comparison (ObserveContent at commit) must catch the substitution
// as a mismatch.
func TestReverifyAttackSameSizeMtimeRestored(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "target.txt")

	writeProbeFile(t, p, "hello")
	expectation, err := ObserveContent(SourceRead, p)
	if err != nil {
		t.Fatal(err)
	}
	mt, ok := expectation.ModTime()
	if !ok {
		t.Fatal("the expectation must carry mtime")
	}

	// The attack: same length, different bytes, mtime put back exactly.
	writeProbeFile(t, p, "HELLO")
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}

	// Sanity: the attack really is invisible to size+mtime.
	weakNow, err := ObservePath(SourceRead, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(expectation, weakNow); !errors.Is(err, ErrIncomparableIdentity) {
		t.Fatalf("digest expectation vs weak re-observation = %v, want incomparable (fail-closed)", err)
	}
	if got := checkIntersectionOnly(expectation, weakNow); got != nil {
		t.Fatalf("control broken: the pre-fix intersection Check must pass the attack silently, got %v", got)
	}

	// The honest wiring: both ends digest-carrying. The substitution must
	// surface as a mismatch, not pass and not be incomparable.
	strongNow, err := ObserveContent(SourceRead, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(expectation, strongNow); !errors.Is(err, ErrConditionMismatch) {
		t.Fatalf("digest-vs-digest comparison of substituted content = %v, want mismatch", err)
	}
	if _, ok := strongNow.Digest(); !ok {
		t.Fatal("ObserveContent lost the digest")
	}
}

// The asymmetry must not overcorrect: the expectation fixes the
// condition's strength, and a deliberately weak condition is legitimate.
// A weak (size+mtime) expectation compared against a richer or equally
// weak observation must evaluate as stated — including passing silently
// where size+mtime cannot see. That is the documented contract, and the
// reason strictness cannot be symmetric; the probe exists so a future
// "symmetric strictness" patch fails here instead of shipping.
func TestReverifyWeakExpectationIsLegitimate(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "target.txt")

	writeProbeFile(t, p, "hello")
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	weakExpectation := Observed(SourceRead, p, info, time.Now())
	if _, ok := weakExpectation.Digest(); ok {
		t.Fatal("test setup: the weak expectation must not carry a digest")
	}

	// Unchanged file, weak expectation, weak re-observation: holds.
	weakNow, err := ObservePath(SourceRead, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(weakExpectation, weakNow); err != nil {
		t.Fatalf("weak expectation, unchanged file = %v, want nil", err)
	}

	// Unchanged file, weak expectation, strong re-observation: the extra
	// digest in the current observation is ignored, never an error.
	strongNow, err := ObserveContent(SourceRead, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(weakExpectation, strongNow); err != nil {
		t.Fatalf("weak expectation vs richer observation = %v, want nil (extras ignored)", err)
	}

	// The same attack as before now passes — silently, correctly, and by
	// construction: a digest-less expectation IS a size+mtime condition.
	// This is the documented limit of what Check can enforce, and the
	// reason the wiring contract must demand digest at both ends rather
	// than rely on Check strictness alone.
	mt := info.ModTime()
	writeProbeFile(t, p, "HELLO")
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}
	attackedNow, err := ObservePath(SourceRead, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(weakExpectation, attackedNow); err != nil {
		t.Fatalf("weak condition must evaluate as written (attack invisible to it), got %v", err)
	}

	// Mtime genuinely changed: even the weak condition catches it.
	if err := os.Chtimes(p, mt.Add(time.Hour), mt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	laterNow, err := ObservePath(SourceRead, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(weakExpectation, laterNow); !errors.Is(err, ErrConditionMismatch) {
		t.Fatalf("weak condition vs changed mtime = %v, want mismatch", err)
	}
}

// Absence must not interact badly with fail-closed strictness: missing
// observations carry no attributes, and both-absent agreement must stay
// nil. A present expectation vs an absent current is a mismatch, never
// incomparable.
func TestReverifyAbsenceStaysEvaluable(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "gone.txt")
	at := time.Now()

	miss1 := Missing(SourceRead, p, at)
	miss2 := Missing(SourceRead, p, at.Add(time.Second))
	if err := Check(miss1, miss2); err != nil {
		t.Fatalf("both-absent = %v, want nil", err)
	}

	writeProbeFile(t, p, "x")
	present, err := ObserveContent(SourceRead, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(miss1, present); !errors.Is(err, ErrConditionMismatch) {
		t.Fatalf("absent expectation vs present file = %v, want mismatch", err)
	}
	if err := Check(present, miss2); !errors.Is(err, ErrConditionMismatch) {
		t.Fatalf("present expectation vs absent file = %v, want mismatch", err)
	}
	// Cross-source stays incomparable regardless of the respin.
	if err := Check(present, Missing(SourceGit, p, at)); !errors.Is(err, ErrIncomparableIdentity) {
		t.Fatalf("cross-source = %v, want incomparable", err)
	}
}

// The constructor's core promise: stat and digest describe one opened
// file object. The probe swaps the file underneath continuous
// observation — via atomic rename, so every generation is complete and
// the path itself is never absent — and asserts that every observation
// is internally consistent: if size and mtime name generation G, the
// digest must be generation G's digest. A stat-then-open observation —
// the Frankenstein shape the constructor claims not to be — observes the
// stat of generation G and the bytes of generation G+1 whenever the swap
// lands between the two syscalls, and the same assertion catches it.
// The control below the main loop measures that the discriminating
// power is real, not theoretical.
func TestReverifyObserveContentNeverFrankenstein(t *testing.T) {
	if testing.Short() {
		t.Skip("concurrency probe")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "victim.bin")

	// Two generations, distinct content and distinct fixed mtimes. They
	// are small on purpose: the race this probe hunts lives between the
	// stat and the open, not in the read — an open file is bound to its
	// inode — so what matters is swap frequency, and small generations
	// swap by hardlink+rename with no copying at all.
	genA := []byte("generation a content")
	genB := []byte("generation b payload!")
	mtA := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	mtB := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	storeA := filepath.Join(dir, "store-a.bin")
	storeB := filepath.Join(dir, "store-b.bin")
	for _, gen := range []struct {
		store string
		data  []byte
		mt    time.Time
	}{{storeA, genA, mtA}, {storeB, genB, mtB}} {
		if err := os.WriteFile(gen.store, gen.data, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(gen.store, gen.mt, gen.mt); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(dir, "next.bin")
	if err := os.Link(storeA, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(link, p); err != nil {
		t.Fatal(err)
	}

	digestOf := map[[32]byte]string{}
	dA, err := DigestFileSHA256(storeA)
	if err != nil {
		t.Fatal(err)
	}
	dB, err := DigestFileSHA256(storeB)
	if err != nil {
		t.Fatal(err)
	}
	digestOf[dA], digestOf[dB] = "A", "B"

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	swaps := 0
	go func() {
		defer wg.Done()
		// Continuously swap the two generations in place at the victim
		// path: hardlink the other generation's store to a scratch name,
		// then atomically rename it over the victim. Two syscalls per
		// swap, no copying — the victim path never ceases to exist, and
		// every generation at it is always complete.
		aIsVictim := true // victim currently holds generation A
		for {
			select {
			case <-stop:
				return
			default:
			}
			store := storeB
			if !aIsVictim {
				store = storeA
			}
			if err := os.Link(store, link); err != nil {
				continue
			}
			if err := os.Rename(link, p); err != nil {
				_ = os.Remove(link)
				continue
			}
			aIsVictim = !aIsVictim
			swaps++
		}
	}()
	defer func() { close(stop); wg.Wait() }()

	inconsistent := 0
	frankenInconsistent := 0
	observations := 0
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		obs, err := ObserveContent(SourceRead, p)
		if err != nil {
			continue // transiently unreadable; not a consistency failure
		}
		observations++
		size, _ := obs.Size()
		mt, _ := obs.ModTime()
		digest, hasDigest := obs.Digest()
		if !hasDigest {
			t.Fatal("an observation of an existing file lost the digest")
		}
		gen, known := digestOf[digest]
		consistent := known &&
			((gen == "A" && size == int64(len(genA)) && mt.Equal(mtA)) ||
				(gen == "B" && size == int64(len(genB)) && mt.Equal(mtB)))
		if !consistent {
			inconsistent++
		}

		// Control: the Frankenstein observation — stat the path, then
		// open it fresh. Inconsistency here is expected and is what makes
		// the main assertion meaningful.
		if info, statErr := os.Stat(p); statErr == nil {
			f, openErr := os.Open(p)
			if openErr == nil {
				fd, _ := DigestSHA256(f)
				f.Close()
				gen, known := digestOf[fd]
				fconsistent := known &&
					((gen == "A" && info.Size() == int64(len(genA)) && info.ModTime().Equal(mtA)) ||
						(gen == "B" && info.Size() == int64(len(genB)) && info.ModTime().Equal(mtB)))
				if !fconsistent {
					frankenInconsistent++
				}
			}
		}
	}
	if observations == 0 {
		t.Fatal("no observations completed")
	}
	if inconsistent != 0 {
		t.Fatalf("%d/%d ObserveContent observations were Frankenstein: stat and digest described different generations",
			inconsistent, observations)
	}
	if frankenInconsistent == 0 {
		t.Logf("warning: the stat-then-open control never caught a swap mid-window; "+
			"the discrimination argument rests on the structural same-fd reading, not this control")
	}
	t.Logf("observations=%d franken-control-inconsistent=%d", observations, frankenInconsistent)
}
