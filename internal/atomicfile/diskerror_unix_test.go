//go:build unix

package atomicfile

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func limitedHelper() {
	// argv: -test.run=^$ <dir>
	dir := os.Args[len(os.Args)-1]
	// The kernel refuses writes beyond 8 KiB in this process, as a full
	// disk does: the write or the sync fails partway through the file.
	lim := &syscallRlimit{Cur: 8192, Max: 8192}
	if err := setrlimit(rlimitFSIZE, lim); err != nil {
		os.Exit(2)
	}
	target := filepath.Join(dir, "limited.txt")
	big := strings.Repeat("x", 1<<20)
	err := WriteFile(target, func(w io.Writer) error {
		_, err := io.WriteString(w, big)
		return err
	})
	if err != nil {
		os.Exit(3) // the failure we expect the parent to inspect
	}
	os.Exit(0)
}

func TestMidWriteDiskErrorKeepsOldBytes(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "limited.txt")
	if err := os.WriteFile(target, []byte("the old truth"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := helperCmd(t, "limited", dir)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// The helper may die of SIGXFSZ or exit 3 on the write error; both are
	// the kernel's disk-error outcome. It must not report success.
	err := cmd.Wait()
	if err == nil {
		t.Fatal("a write beyond the kernel's file size limit succeeded")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "the old truth" {
		t.Fatalf("kernel-limited write corrupted the target: len=%d err=%v", len(data), err)
	}
	// Whether staging remains depends on how the kernel ended the write:
	// an EFBIG error return lets WriteFile clean up after itself, while
	// death by SIGXFSZ is the crash case, whose orphan is documented and
	// harmless. Either way it must never share the target's name.
	entries, rerr := os.ReadDir(dir)
	if rerr != nil {
		t.Fatal(rerr)
	}
	for _, e := range entries {
		if e.Name() == filepath.Base(target) {
			continue
		}
		if !strings.HasPrefix(e.Name(), ".limited.txt.") || !strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("unexpected file beside the target: %s", e.Name())
		}
	}
}
