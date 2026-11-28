// Package atomicfile writes files through a staging file in the target's
// own directory: the new contents are complete and durable there before a
// rename publishes them in one step. A failure or crash before the rename
// leaves the target holding its old bytes; nothing partial ever appears
// under the target's name.
//
// Concurrent sessions for one target are last-writer-wins: each writes
// its own randomly named staging file, and the last rename published
// decides the contents. Writes never interleave into a mixed file.
// Callers that need conflict detection publish through CommitIf, which
// evaluates its condition immediately before the rename, or — when the
// condition is absence — CommitIfAbsent, which the kernel makes truly
// atomic. A condition checked before writing instead, as a caller reading
// the file and comparing later would do, leaves the whole staging write
// as the window in which a concurrent external write is silently
// replaced; POSIX renames cannot carry conditions, so even CommitIf
// leaves the rename-boundary sliver. Crashed sessions leave their staging
// files orphaned beside the target: dot-hidden, never mistaken for the
// target, never cleaned or reused by a later session, and safe to inspect
// or delete by hand.
//
// One session may be shared across goroutines: Write, Commit and Abort
// serialize, and the first Commit or Abort to arrive ends the session for
// the rest. What they may not do is order themselves — which write lands
// before the commit is the caller's synchronization problem, as with
// os.File. Replacement keeps the replaced file's permission bits but not
// metadata the rename cannot carry: hard links break, extended attributes
// and ACLs do not survive, and setuid, setgid and sticky bits are
// stripped, because only the permission bits are copied.
package atomicfile

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// ErrReadOnlyTarget rejects a write whose target exists without any write
// permission: truncating it in place would fail too, and silently
// replacing a file the owner marked read-only is worse.
var ErrReadOnlyTarget = errors.New("the file is read-only")

// Session is one logical overwrite of path. Write buffers into the staging
// file, Commit syncs, closes and publishes it, Abort discards it. A
// session ends at Commit or Abort: after either one, Write and Commit keep
// failing, and only one of them can succeed. Nothing of the session ever
// reaches the target without a successful Commit. The methods may be
// called from different goroutines; they serialize, and a Write racing a
// Commit either lands entirely before it or fails after it.
type Session struct {
	target   string // the publish destination after symlink resolution
	staging  string // the sibling file being written
	f        *os.File
	mu       sync.Mutex // serializes Write, Commit and Abort for sharable sessions
	open     bool       // writes still accepted
	staged   bool       // staging still on disk to clean up
	poisoned bool       // a write failed; nothing partial may be published
	finished bool       // Commit or Abort ran
}

// Begin starts overwriting path. The file is written anew: a symlink is
// followed when it resolves, and the permissions of the file it replaces
// are kept. A path whose symlink does not resolve is replaced with a
// regular file rather than followed into a nonexistent target.
func Begin(path string) (*Session, error) {
	return begin(path, 0)
}

// BeginPerm is Begin with the permissions a newly created target gets.
// A file already at path keeps its own permissions instead.
func BeginPerm(path string, perm os.FileMode) (*Session, error) {
	return begin(path, perm)
}

func begin(path string, perm os.FileMode) (*Session, error) {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	if info, err := os.Stat(path); err == nil {
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s is not a regular file", path)
		}
		if info.Mode().Perm()&0o222 == 0 {
			return nil, fmt.Errorf("%w: %s", ErrReadOnlyTarget, path)
		}
	}
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	var tmp *os.File
	for range 10 {
		var id [8]byte
		if _, err := rand.Read(id[:]); err != nil {
			return nil, err
		}
		name := filepath.Join(dir, fmt.Sprintf(".%s.%x.tmp", base, id))
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		tmp = f
		break
	}
	if tmp == nil {
		return nil, fmt.Errorf("could not create a staging file next to %s", path)
	}
	s := &Session{target: path, staging: tmp.Name(), f: tmp, open: true, staged: true}
	if info, err := os.Stat(path); err == nil {
		if err := tmp.Chmod(info.Mode().Perm()); err != nil {
			s.Abort()
			return nil, err
		}
	} else if perm != 0 {
		if err := tmp.Chmod(perm); err != nil {
			s.Abort()
			return nil, err
		}
	}
	return s, nil
}

// Write appends to the staging file. As io.Writer it may accept a prefix
// of p when it fails; io.Copy and WriteTo loop, which is how callers reach
// it. After Commit or Abort, or after the first failure, every Write
// fails: the session is poisoned rather than silently truncated.
func (s *Session) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.open {
		return 0, fmt.Errorf("atomicfile: write on a finished or failed session for %s", s.target)
	}
	n, err := s.f.Write(p)
	if err != nil {
		s.open = false
		s.poisoned = true
	}
	return n, err
}

// Commit finishes the write: sync, close, then rename over the target.
// Only a nil error means the new bytes are the target's. A session whose
// write failed is poisoned: its Commit fails instead of publishing partial
// contents. Any failure reports it, removes the staging file and ends the
// session; the target keeps its old bytes.
//
// A failed publish never retains the staging file for an in-place retry.
// The premise is not that rename failures are permanent — they are not:
// NFS and FUSE renames fail with ESTALE from client cache races, signals
// interrupt them with EINTR, soft mounts return EIO while a server is
// down, and a full directory can refuse one more entry until space frees;
// retrying the identical rename is the documented remedy for several of
// these. Cleanup is the right trade-off for two other reasons. First,
// after EINTR or a network filesystem error the rename may already have
// happened server-side, so the publish state is uncertain, and a retry of
// it can overwrite what the first attempt published: safe republishing is
// only possible when the failure is known to predate the rename, which the
// error alone cannot tell. Second, every caller regenerates the staged
// bytes at no cost — they are editor buffers and serialized settings — so
// retention would buy nothing even when a retry was safe. What a retry
// needs is a fresh session that writes everything again. Callers whose
// bytes cannot be regenerated must stream them again from their source
// rather than rely on staging surviving a failed publish.
func (s *Session) Commit() error {
	return s.commit(nil, false)
}

// CommitIf is Commit that first requires cond: it runs immediately before
// the rename, after the staging file is durable and closed, and its error
// aborts the publish — the target keeps its old bytes and the staging file
// is removed, exactly as a failed Commit. The condition is not part of
// the atomic operation: POSIX rename carries no condition, so an external
// write landing between cond and the rename is still overwritten. What
// CommitIf shrinks is the window, from the entire staging write — which
// grows with the file — to the rename boundary; callers must treat the
// semantics as best-effort conflict detection, never as atomic compare-
// and-publish. cond runs after the session is finished for every other
// operation — the lock no longer needs to cover it because nothing a
// racing Write, Abort or Commit could still change — so a cond that calls
// back into the session does not deadlock: it observes the finished
// session and fails like any late caller.
func (s *Session) CommitIf(cond func() error) error {
	return s.commit(cond, false)
}

// CommitIfAbsent publishes like Commit but atomically refuses when the
// target exists: the publish is a hard link of the staging file onto the
// target's name, and the kernel fails it when anything already occupies
// that name. The refusal error satisfies errors.Is(err, fs.ErrExist).
// Unlike CommitIf this is a true atomic create-if-absent: no external
// write can slip between check and publish, because the kernel performs
// both in one step. It is the one conditional publish POSIX offers for
// regular files. The target tested is the session's resolved one, as
// Begin describes: an existing symlink is followed before the test.
func (s *Session) CommitIfAbsent() error {
	return s.commit(nil, true)
}

func (s *Session) commit(cond func() error, ifAbsent bool) error {
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		return fmt.Errorf("atomicfile: commit on a finished session for %s", s.target)
	}
	s.finished = true
	s.open = false
	if s.poisoned {
		_ = s.f.Close()
		s.cleanup()
		s.mu.Unlock()
		return fmt.Errorf("atomicfile: commit after a failed write on %s", s.target)
	}
	// Once finished is set, every other operation on the session fails or
	// no-ops under the lock, so nothing below needs its protection —
	// holding it through cond would only buy a permanent self-deadlock
	// the first time a condition calls back into the session.
	s.mu.Unlock()
	err := s.f.Sync()
	if closeErr := s.f.Close(); err == nil {
		err = closeErr
	}
	if err == nil && cond != nil {
		// As late as the condition can run: the staging file is durable
		// and closed, and nothing but the publish remains.
		err = cond()
	}
	if err == nil {
		if ifAbsent {
			err = os.Link(s.staging, s.target)
			if err == nil {
				// Published: the staging name is now a second link to the
				// target's contents, not scratch, so removing it is cleanup
				// rather than destruction. Its failure leaves a dot-hidden
				// twin — the documented orphan shape, never the target's name.
				s.staged = false
				_ = os.Remove(s.staging)
				return nil
			}
		} else {
			err = os.Rename(s.staging, s.target)
		}
	}
	if err != nil {
		s.cleanup()
		return err
	}
	s.staged = false
	return nil
}

// Abort discards the staging file. Nothing of this session reaches the
// target, now or later; the file is never republished by another session,
// because the random staging name belongs to this one. Abort never fails a
// write that already reported its error: cleanup failures do not replace
// the result the caller was given.
func (s *Session) Abort() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return nil
	}
	s.finished = true
	s.open = false
	_ = s.f.Close()
	s.cleanup()
	return nil
}

func (s *Session) cleanup() {
	if s.staged {
		s.staged = false
		_ = os.Remove(s.staging)
	}
}

// Target is what a successful Commit publishes to, after symlinks resolve.
func (s *Session) Target() string { return s.target }

// WriteFile publishes data over path in one Commit, streaming it through
// write. The write is all-or-nothing: either the target holds exactly the
// bytes write produced, or it is unchanged (or still absent) and the error
// says why. Bytes passed to write are never partially committed: write may
// stream them in pieces, but the publish boundary is the whole file.
func WriteFile(path string, write func(io.Writer) error) error {
	return WriteFilePerm(path, 0, write)
}

// WriteFilePerm is WriteFile with the permissions a newly created target
// gets, as BeginPerm describes. A panic in write still cleans up: the
// deferred Abort closes the staging handle and removes the file before
// the panic propagates, rather than leaking the descriptor until exit.
func WriteFilePerm(path string, perm os.FileMode, write func(io.Writer) error) error {
	s, err := BeginPerm(path, perm)
	if err != nil {
		return err
	}
	defer func() { _ = s.Abort() }()
	if err := write(s); err != nil {
		return err
	}
	return s.Commit()
}
