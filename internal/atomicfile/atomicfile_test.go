package atomicfile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func testdata(t *testing.T, n int) []byte {
	t.Helper()
	var sb strings.Builder
	for len(sb.String()) < n {
		sb.WriteString("the quick brown fox jumps over the lazy dog\n")
	}
	return []byte(sb.String()[:n])
}

func TestWriteFilePublishesExactBytesAndKeepsMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	want := testdata(t, 100_000)
	if err := WriteFile(path, func(w io.Writer) error {
		// small pieces prove the stream does not need to be one buffer
		rest := want
		for len(rest) > 0 {
			n := min(len(rest), 997)
			if _, err := w.Write(rest[:n]); err != nil {
				return err
			}
			rest = rest[n:]
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(want) {
		t.Fatalf("target bytes differ: len(data)=%d err=%v", len(data), err)
	}
	if runtime.GOOS == "windows" {
		// Windows has no POSIX permission bits; os.Chmod there only toggles
		// the read-only attribute. The invariant that survives the rewrite
		// is that the rewritten file is not read-only, since the pre-write
		// file was writable.
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o222 == 0 {
			t.Fatalf("rewritten file became read-only: %v", info.Mode().Perm())
		}
	} else if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("mode not preserved: %v err=%v", info.Mode().Perm(), err)
	}
	if stagingLeftovers(t, dir) {
		t.Fatal("a successful write left staging files in the directory")
	}
}

func stagingLeftovers(t *testing.T, dir string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") && strings.HasPrefix(e.Name(), ".") {
			return true
		}
	}
	return false
}

func TestWriteFileFailureKeepsOldBytesAndNoStaging(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("injected write failure")
	err := WriteFile(path, func(w io.Writer) error {
		if _, err := w.Write([]byte("half the new content")); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "original" {
		t.Fatalf("failed write touched the target: %q err=%v", data, err)
	}
	if stagingLeftovers(t, dir) {
		t.Fatal("failed write left staging behind")
	}
}

func TestCommitFailureDoesNotReportSuccess(t *testing.T) {
	// The rename over the target fails everywhere when the target is a
	// directory by the time publish runs. The commit must report failure
	// rather than success, and the staging file must not be left behind.
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Begin(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write([]byte("data")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.Commit(); err == nil {
		t.Fatal("commit over a directory reported success")
	}
	if _, err := s.Write([]byte("more")); err == nil {
		t.Fatal("the failed session still accepts writes")
	}
	if err := s.Commit(); err == nil {
		t.Fatal("the failed session still accepts commits")
	}
	if stagingLeftovers(t, dir) {
		t.Fatal("failed publish left staging behind")
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Fatal("failed publish disturbed the target directory")
	}
}

func TestPoisonedSessionNeverPublishes(t *testing.T) {
	// A write that fails mid-session must not publish what little was
	// accepted, even if the caller forgets to abort and commits anyway.
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Begin(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write([]byte("partial")); err != nil {
		t.Fatal(err)
	}
	if err := s.f.Close(); err != nil { // simulate the backing write failing underneath
		t.Fatal(err)
	}
	if _, err := s.Write([]byte("more")); err == nil {
		t.Fatal("writes kept going after the backing file failed")
	}
	if err := s.Commit(); err == nil {
		t.Fatal("a poisoned session published")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "old" {
		t.Fatalf("a poisoned session changed the target: %q err=%v", data, err)
	}
	if stagingLeftovers(t, dir) {
		t.Fatal("the poisoned session left staging behind")
	}
}

func TestAbortExposesNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Begin(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write([]byte("never committed")); err != nil {
		t.Fatal(err)
	}
	if err := s.Abort(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "old" {
		t.Fatalf("abort changed the target: %q err=%v", data, err)
	}
	if stagingLeftovers(t, dir) {
		t.Fatal("abort left staging behind")
	}
}

func TestReadOnlyTargetRejectedBeforeStaging(t *testing.T) {
	if runningAsRoot() {
		t.Skip("root ignores permission bits")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "locked.txt")
	if err := os.WriteFile(path, []byte("locked"), 0o444); err != nil {
		t.Fatal(err)
	}
	err := WriteFile(path, func(w io.Writer) error {
		_, err := io.WriteString(w, "replacement")
		return err
	})
	if !errors.Is(err, ErrReadOnlyTarget) {
		t.Fatalf("err = %v, want ErrReadOnlyTarget", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "locked" {
		t.Fatalf("read-only target changed: %q err=%v", data, err)
	}
	if stagingLeftovers(t, dir) {
		t.Fatal("rejected write left staging behind")
	}
}

func TestSymlinkTargetIsWrittenThrough(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real.txt")
	link := filepath.Join(dir, "link.txt")
	if err := os.WriteFile(real, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(link, func(w io.Writer) error {
		_, err := io.WriteString(w, "through the symlink")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(real)
	if err != nil || string(data) != "through the symlink" {
		t.Fatalf("symlink target: %q err=%v", data, err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink itself was replaced")
	}
}

// writeFileRetrying republishes the whole file when a publish fails.
// On Windows a rename over the target loses to any concurrent open handle
// on it — Go does not pass FILE_SHARE_DELETE — so a transient access
// denial is environmental reality there, not a violation of the contract
// under test (writes never interleave, readers never see torn bytes). The
// package documents regenerating the bytes and retrying the whole write
// as the remedy, which is what this does. The retry is bounded so a
// permanent failure cannot hide inside it. On POSIX the first attempt
// succeeds and the loop never iterates.
func writeFileRetrying(t *testing.T, path, text string) error {
	t.Helper()
	var err error
	for attempt := range 200 {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Millisecond)
		}
		rest := text
		err = WriteFile(path, func(w io.Writer) error {
			for len(rest) > 0 {
				n := min(len(rest), 1500)
				if _, err := w.Write([]byte(rest[:n])); err != nil {
					return err
				}
				rest = rest[n:]
			}
			return nil
		})
		if err == nil {
			return nil
		}
	}
	return fmt.Errorf("write still failing after 200 whole-write retries: %w", err)
}

func TestConcurrentWritesNeverInterleave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	a := strings.Repeat("a", 50_000) + "\n"
	b := strings.Repeat("b", 50_000) + "\n"
	done := make(chan error, 2)
	write := func(text string) {
		var err error
		for range 20 {
			if err = writeFileRetrying(t, path, text); err != nil {
				break
			}
		}
		done <- err
	}
	go write(a)
	go write(b)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != a && string(data) != b {
		t.Fatal("concurrent writes interleaved: target holds neither complete version")
	}
	if stagingLeftovers(t, dir) {
		t.Fatal("concurrent writes left staging behind")
	}
}

// TestConcurrentReadersNeverSeeTornContent strengthens the interleaving
// test by reading mid-stream: readers poll continuously while writers
// race, and every read must be one complete version, never a mix. This
// is the user-visible contract at any instant, not only after the dust
// settles. (Regression coverage from the independent verification of the
// write lifecycle.)
func TestConcurrentReadersNeverSeeTornContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	versions := []string{
		strings.Repeat("a", 40_000) + "\n",
		strings.Repeat("b", 40_000) + "\n",
		strings.Repeat("c", 40_000) + "\n",
		strings.Repeat("d", 40_000) + "\n",
	}
	if err := os.WriteFile(path, []byte(versions[0]), 0o644); err != nil {
		t.Fatal(err)
	}
	valid := map[string]bool{}
	for _, v := range versions {
		valid[v] = true
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	failures := make(chan string, 64)
	for _, v := range versions {
		wg.Add(1)
		go func(text string) {
			defer wg.Done()
			for range 15 {
				select {
				case <-stop:
					return
				default:
				}
				if err := writeFileRetrying(t, path, text); err != nil {
					failures <- "writer error: " + err.Error()
					return
				}
			}
		}(v)
	}

	reads := 0
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-stop:
				return
			default:
			}
			data, err := os.ReadFile(path)
			if err != nil {
				// Windows publishes with MoveFileEx, not a rename of one
				// directory entry: at the publish boundary an open can lose
				// to the move in flight and be refused, or meet the moment
				// the target is unlinked. Neither is torn content — the
				// contract under test — and both pass with the boundary,
				// so they cost a read rather than fail the test.
				if runtime.GOOS == "windows" && (errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.Errno(32))) {
					// Errno 32 is ERROR_SHARING_VIOLATION; it maps to no
					// portable error, and comparing the Errno itself
					// compiles on every platform, where it matches nothing.
					continue
				}
				failures <- "reader error: " + err.Error()
				return
			}
			reads++
			if !valid[string(data)] {
				failures <- "torn read: target held none of the complete versions"
				return
			}
		}
	}()

	wg.Wait()
	close(stop)
	<-readerDone
	close(failures)
	for f := range failures {
		t.Fatal(f)
	}
	if reads < 10 {
		t.Fatalf("readers barely ran (reads=%d): overlap not demonstrated", reads)
	}
}

// stagingName returns the session's staging path for crash-positioning.
func stagingName(s *Session) string { return s.staging }

// TestMain runs the helper processes the crash and disk-error tests
// fork: they die or hit real kernel write limits inside this binary.
func TestMain(m *testing.M) {
	switch os.Getenv("SEE_ATOMICFILE_HELPER") {
	case "crash":
		crashHelper()
	case "limited":
		limitedHelper()
	}
	os.Exit(m.Run())
}

func helperCmd(t *testing.T, mode string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], append([]string{"-test.run=^$"}, args...)...)
	cmd.Env = append(os.Environ(), "SEE_ATOMICFILE_HELPER="+mode)
	return cmd
}

func crashHelper() {
	// argv: -test.run=^$ <target> <ready-file>
	target, ready := os.Args[len(os.Args)-2], os.Args[len(os.Args)-1]
	s, err := Begin(target)
	if err != nil {
		os.Exit(2)
	}
	if _, err := s.Write([]byte("uncommitted write that must never be seen")); err != nil {
		os.Exit(2)
	}
	if err := os.WriteFile(ready, []byte(stagingName(s)), 0o600); err != nil {
		os.Exit(2)
	}
	// Wait to be killed mid-session: no Commit, no Abort. The park must
	// keep a timer pending — a bare select{} has no cases and no timers,
	// so the runtime's deadlock detector would kill the helper itself
	// with "all goroutines are asleep" if the parent is slow to notice
	// the ready file, masquerading as a test-binary crash.
	for {
		time.Sleep(time.Hour)
	}
}

func TestCrashLeavesOldBytesAndStagingImpersonatesNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("the old truth"), 0o644); err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(dir, "ready")
	cmd := helperCmd(t, "crash", path, ready)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			t.Fatal("helper never staged its write")
		}
		time.Sleep(10 * time.Millisecond)
	}
	staging, err := os.ReadFile(ready)
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "the old truth" {
		t.Fatalf("crash corrupted the target: %q err=%v", data, err)
	}
	// The staging file may remain — it is the crashed session's orphaned
	// scratch, owned by nobody now — but it must never be mistaken for the
	// target: its dot-hidden random name is not the target's name, and the
	// target still holds the old bytes.
	if _, err := os.Stat(string(staging)); err != nil {
		t.Fatalf("staging file missing at %s: %v", staging, err)
	}
	if filepath.Base(string(staging)) == filepath.Base(path) {
		t.Fatal("staging file shares the target's name")
	}
	if data2, err := os.ReadFile(path); err != nil || string(data2) == "uncommitted write that must never be seen" {
		t.Fatalf("uncommitted bytes reached the target: %q err=%v", data2, err)
	}
	// A fresh write after the crash succeeds and cleans up after itself.
	if err := WriteFile(path, func(w io.Writer) error {
		_, err := io.WriteString(w, "recovered write")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != "recovered write" {
		t.Fatalf("post-crash write: %q err=%v", data, err)
	}
}
