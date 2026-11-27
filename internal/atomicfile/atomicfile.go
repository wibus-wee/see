// Package atomicfile writes files through a staging file in the target's
// own directory: the new contents are complete and durable there before a
// rename publishes them in one step. A failure or crash before the rename
// leaves the target holding its old bytes; nothing partial ever appears
// under the target's name.
//
// Concurrent sessions for one target are last-writer-wins: each writes
// its own randomly named staging file, and the last rename published
// decides the contents. Writes never interleave into a mixed file.
// Callers that need conflict detection must check before writing, as
// editor saves do against the disk text they read; POSIX renames cannot
// carry conditions, so a concurrent external write in the window between
// such a check and the publish is silently replaced. Crashed sessions
// leave their staging files orphaned beside the target: dot-hidden, never
// mistaken for the target, never cleaned or reused by a later session,
// and safe to inspect or delete by hand.
package atomicfile

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrReadOnlyTarget rejects a write whose target exists without any write
// permission: truncating it in place would fail too, and silently
// replacing a file the owner marked read-only is worse.
var ErrReadOnlyTarget = errors.New("the file is read-only")

// Session is one logical overwrite of path. Write buffers into the staging
// file, Commit syncs, closes and publishes it, Abort discards it. A
// session ends at Commit or Abort: after either one, Write and Commit keep
// failing, and only one of them can succeed. Nothing of the session ever
// reaches the target without a successful Commit.
type Session struct {
	target   string // the publish destination after symlink resolution
	staging  string // the sibling file being written
	f        *os.File
	open     bool // writes still accepted
	staged   bool // staging still on disk to clean up
	poisoned bool // a write failed; nothing partial may be published
	finished bool // Commit or Abort ran
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
// session; the target keeps its old bytes. Publish failures of a
// same-directory rename are configuration failures (permissions, a
// directory in the way), so retrying this session could not succeed: the
// retry is a fresh session that writes everything again, and no staging
// is retained for it.
func (s *Session) Commit() error {
	if s.finished {
		return fmt.Errorf("atomicfile: commit on a finished session for %s", s.target)
	}
	s.finished = true
	s.open = false
	if s.poisoned {
		_ = s.f.Close()
		s.cleanup()
		return fmt.Errorf("atomicfile: commit after a failed write on %s", s.target)
	}
	err := s.f.Sync()
	if closeErr := s.f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(s.staging, s.target)
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
// gets, as BeginPerm describes.
func WriteFilePerm(path string, perm os.FileMode, write func(io.Writer) error) error {
	s, err := BeginPerm(path, perm)
	if err != nil {
		return err
	}
	if err := write(s); err != nil {
		_ = s.Abort()
		return err
	}
	return s.Commit()
}
