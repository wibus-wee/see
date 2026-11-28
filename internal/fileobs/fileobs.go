// Package fileobs describes the state of a file as one operation observed
// it: an immutable snapshot bound to its source and observation time, never
// a permanent property of the path. It exists so that saves, the watcher,
// git status, local history, and downloads can speak about "the file I
// read" in one identity model instead of mixing path, mtime, size, and
// content hash as interchangeable tokens.
//
// The model follows SPEC-009 section B:
//
//   - An Observation is immutable. Construction and enrichment copy; no
//     method mutates a snapshot another caller may hold (REQ-META-010).
//   - Every attribute is either present or absent, and a present-but-empty
//     value stays distinct from absence (REQ-META-060). Unknown version
//     state is TriUnknown, never a fabricated false (REQ-META-020).
//   - Identity comparison is defined only between observations of the same
//     Source, because two sources do not necessarily produce tokens of one
//     contract (REQ-META-040).
//
// The package is a contract owner only: wiring it into the watcher, git
// status, or the save path belongs to the subsystems that own those, as
// docs/specs/conformance/lane-e-integration-notes.md describes.
package fileobs

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Source identifies the operation or subsystem that produced an
// observation. Tokens of different sources are never compared, because a
// source fixes which attributes its observations carry and what they mean.
type Source string

const (
	// SourceRead is the read an editor buffer was loaded from.
	SourceRead Source = "read"
	// SourceSave is the write that published a buffer.
	SourceSave Source = "save"
	// SourceWatch is a filesystem event the watcher observed.
	SourceWatch Source = "watch"
	// SourceGit is git's view of the working tree or the index.
	SourceGit Source = "git"
	// SourceHistory is a local history snapshot or restore.
	SourceHistory Source = "history"
	// SourceScan is a quick open or tree scan entry.
	SourceScan Source = "scan"
)

// Tri is a three-state answer: unknown is a state of its own, never a
// fabricated false.
type Tri uint8

const (
	TriUnknown Tri = iota
	TriTrue
	TriFalse
)

// Presence bits for the comparable attributes of an Observation.
const (
	hasSize uint16 = 1 << iota
	hasMtime
	hasDigest
	hasETag
	hasVersion
)

// Observation is one immutable snapshot of a file's state. Fields are
// private so that presence can never drift from value, and so that a
// snapshot in one caller's hands cannot change under another's.
type Observation struct {
	path    string
	exists  bool
	source  Source
	at      time.Time
	attrs   uint16
	size    int64
	mtime   time.Time
	digest  [32]byte
	etag    string
	version string
	current Tri
	deleted Tri
}

// Observed is the snapshot of a file that exists, as source saw it at at.
// Size and modification time come from info; enrich the result with
// WithDigest or the other enrichment methods as the operation measured
// more.
func Observed(source Source, path string, info os.FileInfo, at time.Time) Observation {
	return Observation{
		path:   path,
		exists: true,
		source: source,
		at:     at.UTC(),
		attrs:  hasSize | hasMtime,
		size:   info.Size(),
		mtime:  info.ModTime().UTC(),
	}
}

// Missing is the snapshot of a file that was absent when source looked at
// at. Absence is a real observation: a create-if-absent condition compares
// it like any other state.
func Missing(source Source, path string, at time.Time) Observation {
	return Observation{path: path, source: source, at: at.UTC()}
}

// errNotDirectory reports that path cannot name anything because it
// walks through a file: a child of a non-directory. POSIX answers such a
// lookup with ENOTDIR, plainly an error. Windows answers it with
// ERROR_PATH_NOT_FOUND — the same code a merely missing parent produces —
// and Go folds both into os.ErrNotExist, so the raw lookup error cannot
// separate a corrupted layout from absence there. The distinction
// matters: absence is an observation a create-if-absent condition may
// act on, while a layout violation is a failure that says nothing about
// any file, so the ancestors are walked when the lookup reports absence.
func errNotDirectory(path string) error {
	dir := filepath.Dir(path)
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		info, err := os.Stat(dir)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("%w: %s is not a directory", errBrokenPath, dir)
			}
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			// The ancestor cannot be interrogated — permission, a failed
			// volume. Nothing here proves the layout broken, and the
			// original error is the better description.
			return nil
		}
		dir = parent
	}
}

// errBrokenPath marks a path that walks through a non-directory, a
// corruption of the layout rather than the absence of a file.
var errBrokenPath = errors.New("path walks through a file")

// ObservePath stats path and snapshots it as source sees it now. A file
// that does not exist is an absence observation, not an error; other
// filesystem failures are errors, because they say nothing about the file.
//
// The observation carries size and modification time but never the
// content digest: it is a weak observation, and a condition that must
// distinguish same-size same-mtime content needs the digest — use
// ObserveContent for those, which is what the save path's wiring contract
// requires at both ends (lane-e-integration-notes.md §B).
func ObservePath(source Source, path string) (Observation, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		if broken := errNotDirectory(path); broken != nil {
			return Observation{}, broken
		}
		return Missing(source, path, time.Now()), nil
	}
	if err != nil {
		return Observation{}, err
	}
	return Observed(source, path, info, time.Now()), nil
}

// ObserveContent is ObservePath with the full-content digest measured as
// part of the observation: the observation of an existing file always
// carries the digest, so a content-strength condition built on it can
// never degrade silently. One open file serves both the stat and the
// digest, so both attributes describe the same opened file object even
// when the path is replaced mid-read — a concurrent writer can still
// produce a mismatch, but never a Frankenstein observation that matches
// content it never was. ObserveContent observes files: a directory opens
// but cannot be read, so it is an error, where ObservePath stats it into
// a size+mtime observation happily.
func ObserveContent(source Source, path string) (Observation, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		if broken := errNotDirectory(path); broken != nil {
			return Observation{}, broken
		}
		return Missing(source, path, time.Now()), nil
	}
	if err != nil {
		return Observation{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Observation{}, err
	}
	digest, err := DigestSHA256(f)
	if err != nil {
		return Observation{}, err
	}
	return Observed(source, path, info, time.Now()).WithDigest(digest), nil
}

// ExpectContent is the expectation half of a content condition for bytes
// the caller already holds, where observing the file again cannot produce
// a more faithful snapshot: the editor saves against the bytes it read,
// and a re-observation at that point would race the file instead of
// removing doubt. The observation is digest-only by design — no size, no
// modification time — so the condition it builds is exactly "the content
// is unchanged": a rewrite of identical bytes passes, which a size+mtime
// expectation would reject, and no weakened comparison can be silently
// constructed from it. The current end of the condition must come from
// ObserveContent for Check to evaluate at all — a weak re-observation is
// incomparable, never a silent pass.
func ExpectContent(source Source, path string, content []byte) Observation {
	digest, _ := DigestSHA256(bytes.NewReader(content)) // bytes.Reader never fails
	return Observation{
		path:   path,
		exists: true,
		source: source,
		at:     time.Now().UTC(),
		attrs:  hasDigest,
		digest: digest,
	}
}

// WithDigest returns a copy of o that also carries the full-content
// SHA-256 digest. The original is unchanged.
func (o Observation) WithDigest(digest [32]byte) Observation {
	o.attrs |= hasDigest
	o.digest = digest
	return o
}

// WithETag returns a copy of o that also carries a backend-supplied ETag,
// kept verbatim so it stays comparable with what produced it. An empty
// ETag is a present-but-empty value, distinct from no ETag at all.
func (o Observation) WithETag(etag string) Observation {
	o.attrs |= hasETag
	o.etag = etag
	return o
}

// WithVersion returns a copy of o that also carries a backend version id,
// kept verbatim. current and deleted say whether the version is the
// current one and whether it is a delete marker; pass TriUnknown when the
// backend did not say, rather than guessing false.
func (o Observation) WithVersion(version string, current, deleted Tri) Observation {
	o.attrs |= hasVersion
	o.version = version
	o.current = current
	o.deleted = deleted
	return o
}

// Path is the file the observation is about. It names the subject; it is
// not part of the identity comparison, because the same path can host many
// generations of content.
func (o Observation) Path() string { return o.path }

// Exists reports whether the file was there at the observation. A file
// that exists with no content is an empty file, not a missing one.
func (o Observation) Exists() bool { return o.exists }

// Source is the operation or subsystem that took the snapshot.
func (o Observation) Source() Source { return o.source }

// At is when the observation was taken, in UTC.
func (o Observation) At() time.Time { return o.at }

// Size is the file's length in bytes, and whether it is known.
func (o Observation) Size() (int64, bool) { return o.size, o.attrs&hasSize != 0 }

// ModTime is the file's modification time, and whether it is known.
func (o Observation) ModTime() (time.Time, bool) { return o.mtime, o.attrs&hasMtime != 0 }

// Digest is the full-content SHA-256 digest, and whether it is known.
func (o Observation) Digest() ([32]byte, bool) { return o.digest, o.attrs&hasDigest != 0 }

// ETag is the backend-supplied ETag verbatim, and whether one was
// supplied. A present empty string means the backend sent an empty ETag.
func (o Observation) ETag() (string, bool) { return o.etag, o.attrs&hasETag != 0 }

// Version is the backend version id verbatim, and whether one is known.
func (o Observation) Version() (string, bool) { return o.version, o.attrs&hasVersion != 0 }

// Current says whether the observed version is the current one, or
// TriUnknown when the backend did not say.
func (o Observation) Current() Tri { return o.current }

// Deleted says whether the observation is of a delete marker, or
// TriUnknown when the backend did not say.
func (o Observation) Deleted() Tri { return o.deleted }

// ErrConditionMismatch means a conditional operation's expectation no
// longer holds: the file changed, appeared, or vanished since the
// expectation was observed. It is its own category, distinct from
// not-found or permission errors, so callers can offer a merge or force
// path instead of reporting an absence that is not one (REQ-COND-030).
var ErrConditionMismatch = errors.New("the file no longer matches the observed state")

// ErrIncomparableIdentity means two identity tokens cannot be compared as
// the caller asked: they come from different sources, they share no
// attribute at all, or the current observation lacks an attribute the
// expectation carries. Treating such a pair as matching, or as
// mismatching, would both invent information, so a condition that cannot
// be evaluated must surface as an error rather than as a silent pass or
// fail (REQ-META-040).
var ErrIncomparableIdentity = errors.New("identity tokens cannot be compared")

// Check compares the expectation an operation was conditioned on with the
// file's state now. nil means the condition holds; ErrConditionMismatch
// means it does not; ErrIncomparableIdentity means it cannot be evaluated.
//
// The comparison is strict about provenance and never mixes attributes:
//
//   - different sources are incomparable, because each source fixes its
//     own token contract;
//   - existence must agree — a file that appeared or vanished is a
//     mismatch before any attribute is looked at;
//   - every attribute the expectation carries must also be carried by the
//     current observation. The expectation fixes the condition's strength:
//     if the re-observation skips an attribute — a commit-time
//     ObservePath that never digested, against an expectation that
//     carries the digest — comparing what remains would evaluate a weaker
//     condition than the caller conditioned on, and the weakening would
//     be silent in exactly the same-content-different-bytes cases the
//     stronger attribute exists to catch. A condition that cannot be
//     evaluated at full strength is incomparable, so the mistake fails
//     loudly at commit instead of passing as a match;
//   - attributes the current observation carries beyond the expectation
//     are ignored: the expectation chose the condition's strength, and a
//     richer measurement does not weaken it;
//   - every attribute both observations carry must be equal, byte for
//     byte for digests and verbatim for etags and versions;
//   - with no shared attribute there is nothing to compare, which is
//     incomparable rather than a match.
//
// Nothing in Check can prove the expectation itself is as strong as the
// operation needs: a condition built on a digest-less expectation is a
// size+mtime comparison by construction, evaluated correctly and silently.
// Wiring that needs content strength must build the expectation from a
// digest-carrying observation — see ObserveContent — and
// lane-e-integration-notes.md §B prescribes exactly that for the save
// path.
func Check(expected, current Observation) error {
	if expected.source != current.source {
		return fmt.Errorf("%w: %q tokens cannot be compared with %q tokens",
			ErrIncomparableIdentity, expected.source, current.source)
	}
	if expected.exists != current.exists {
		return ErrConditionMismatch
	}
	if !expected.exists {
		return nil // both agree the file was absent
	}
	if missing := expected.attrs &^ current.attrs; missing != 0 {
		return fmt.Errorf("%w: the expectation carries %s, which the current observation does not measure",
			ErrIncomparableIdentity, attrNames(missing))
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

// attrNames names the presence bits for error messages.
func attrNames(attrs uint16) string {
	names := []struct {
		bit  uint16
		name string
	}{
		{hasSize, "size"},
		{hasMtime, "mtime"},
		{hasDigest, "digest"},
		{hasETag, "etag"},
		{hasVersion, "version"},
	}
	out := ""
	for _, n := range names {
		if attrs&n.bit != 0 {
			if out != "" {
				out += ","
			}
			out += n.name
		}
	}
	return out
}

// Stale reports whether other proves o outdated: the same source observed
// the same path later, and the state no longer matches. An unchanged
// observation is not staleness, but an incomparable one counts as stale:
// a consumer that cannot evaluate whether its observation still holds
// must refresh rather than trust it — staleness is the fail-safe reading,
// and "incomparable is not staleness" would let a poisoned comparison
// keep a cache entry alive forever.
func (o Observation) Stale(later Observation) bool {
	return o.path == later.path && o.source == later.source &&
		later.at.After(o.at) && Check(o, later) != nil
}

// String describes the digest in hex for logs and tests.
func HexDigest(digest [32]byte) string { return hex.EncodeToString(digest[:]) }

// ParseHexDigest is the inverse of HexDigest. A string of the wrong shape
// is an error, never a truncated or padded digest, so a corrupt manifest
// fails closed.
func ParseHexDigest(s string) ([32]byte, error) {
	var out [32]byte
	if len(s) != hex.EncodedLen(len(out)) {
		return out, fmt.Errorf("a SHA-256 digest is %d hex characters, not %d", hex.EncodedLen(len(out)), len(s))
	}
	if _, err := hex.Decode(out[:], []byte(s)); err != nil {
		return out, err
	}
	return out, nil
}
