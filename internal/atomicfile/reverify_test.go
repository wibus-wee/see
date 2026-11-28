package atomicfile

// Independent re-verification tests (branch reverify-lane-c), written by
// the re-verifier without consulting the fixer. They probe seams the
// fixer's own suite does not: the cond-runs-under-lock contract, dangling
// symlinks, and metadata behavior of the link-based publish. No
// implementation code is touched.

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// CommitIf documents "cond runs under the session lock, so it must be
// quick and must not call the session." This probes whether the constraint
// is enforced or documentation-only: a cond that writes through its own
// session. Either outcome is reportable; a deadlock means one misbehaving
// caller freezes the session forever.
func TestReverifyCommitIfCondCallingSession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Begin(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write([]byte("new")); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- s.CommitIf(func() error {
			// Forbidden by the documentation: calling the session from cond.
			_, err := s.Write([]byte("x"))
			return err
		})
	}()
	select {
	case err := <-done:
		t.Logf("no deadlock: cond re-entered the session, commit returned %v", err)
	case <-time.After(3 * time.Second):
		t.Log("CONFIRMED: cond calling the session self-deadlocks — the documented constraint is documentation-only, and the stuck goroutine holds the session lock forever")
	}
}

// A cond that is merely slow, not re-entrant: does it block other session
// operations? The documentation says cond must be quick; this pins what
// "under the session lock" means in practice — a racing Abort cannot end
// the session while cond runs.
func TestReverifyCommitIfSlowCondBlocksAbort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	s, err := Begin(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write([]byte("new")); err != nil {
		t.Fatal(err)
	}
	condRunning := make(chan struct{})
	condRelease := make(chan struct{})
	commitDone := make(chan error, 1)
	go func() {
		commitDone <- s.CommitIf(func() error {
			close(condRunning)
			<-condRelease
			return nil
		})
	}()
	<-condRunning
	abortDone := make(chan struct{})
	go func() {
		_ = s.Abort()
		close(abortDone)
	}()
	select {
	case <-abortDone:
		t.Log("abort completed while cond was still running — it does not wait for the condition")
	case <-time.After(500 * time.Millisecond):
		t.Log("CONFIRMED: a racing Abort blocks until cond returns — slow conditions freeze every session operation")
	}
	close(condRelease)
	if err := <-commitDone; err != nil {
		t.Fatal(err)
	}
	<-abortDone
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "new" {
		t.Fatalf("the commit did not publish after cond passed: %q err=%v", data, err)
	}
}

// The dangling-symlink divergence: plain Commit replaces a dangling
// symlink (documented), but create-if-absent refuses it, because the name
// is occupied. Both defensible — this pins that the difference is real and
// each half matches its documentation.
func TestReverifyDanglingSymlinkCommitVsCommitIfAbsent(t *testing.T) {
	dir := t.TempDir()

	// Plain Commit over a dangling symlink: the symlink is replaced.
	link1 := filepath.Join(dir, "link1.txt")
	if err := os.Symlink(filepath.Join(dir, "missing.txt"), link1); err != nil {
		t.Fatal(err)
	}
	s, err := Begin(link1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write([]byte("new")); err != nil {
		t.Fatal(err)
	}
	if err := s.Commit(); err != nil {
		t.Fatalf("plain commit over a dangling symlink failed: %v", err)
	}
	data, err := os.ReadFile(link1)
	if err != nil || string(data) != "new" {
		t.Fatalf("plain commit did not replace the dangling symlink: %q err=%v", data, err)
	}

	// CommitIfAbsent over a dangling symlink: refused, name occupied.
	link2 := filepath.Join(dir, "link2.txt")
	if err := os.Symlink(filepath.Join(dir, "missing.txt"), link2); err != nil {
		t.Fatal(err)
	}
	s2, err := Begin(link2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Write([]byte("would overwrite")); err != nil {
		t.Fatal(err)
	}
	err = s2.CommitIfAbsent()
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("err = %v, want fs.ErrExist: create-if-absent over a dangling symlink", err)
	}
	// The symlink itself must be untouched.
	target, err := os.Readlink(link2)
	if err != nil {
		t.Fatalf("the dangling symlink is gone: %v", err)
	}
	if target != filepath.Join(dir, "missing.txt") {
		t.Fatalf("the symlink was retargeted: %s", target)
	}
}

// CommitIfAbsent publishes by hard link: the staged bytes become the
// target with no copy. Verify the new file carries the write and that the
// link-then-unlink dance leaves no extra directory entries beyond the
// documented shapes.
func TestReverifyCommitIfAbsentPublishesExactBytesAndMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "created.txt")
	s, err := BeginPerm(path, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("exactly these bytes\x00with a NUL")
	if _, err := s.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := s.CommitIfAbsent(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(payload) {
		t.Fatalf("published bytes differ: %q err=%v", data, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %#o, want 0600 from BeginPerm", info.Mode().Perm())
	}
	if stagingLeftovers(t, dir) {
		t.Fatal("staging left behind")
	}
}

// setid stripping, pinned empirically: a setuid target loses the bit on
// replacement, as the documentation now states.
func TestReverifySetuidBitStrippedOnReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exec")
	if err := os.WriteFile(path, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o4755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, func(w io.Writer) error {
		_, err := w.Write([]byte("new"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSetuid != 0 {
		t.Fatalf("setuid survived replacement: %#o — contradicts the documented stripping", info.Mode())
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("permission bits not preserved: %#o", info.Mode().Perm())
	}
}

// Session-ending ordering, pinned: the first Commit or Abort ends the
// session for everything later, whichever order they arrive in.
func TestReverifyFirstEndingWins(t *testing.T) {
	dir := t.TempDir()

	// Commit then Abort: the commit publishes, the abort is a no-op.
	path1 := filepath.Join(dir, "committed.txt")
	s, err := Begin(path1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write([]byte("winner")); err != nil {
		t.Fatal(err)
	}
	if err := s.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := s.Abort(); err != nil {
		t.Fatalf("abort after commit errored: %v", err)
	}
	data, err := os.ReadFile(path1)
	if err != nil || string(data) != "winner" {
		t.Fatalf("abort after commit undid the publish: %q err=%v", data, err)
	}

	// Abort then Commit: the abort discards, the commit fails.
	path2 := filepath.Join(dir, "aborted.txt")
	s2, err := Begin(path2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Write([]byte("loser")); err != nil {
		t.Fatal(err)
	}
	if err := s2.Abort(); err != nil {
		t.Fatal(err)
	}
	if err := s2.Commit(); err == nil {
		t.Fatal("commit after abort succeeded — the session did not end")
	}
	if _, err := os.Stat(path2); !os.IsNotExist(err) {
		t.Fatalf("the aborted publish reached the target: %v", err)
	}
}
