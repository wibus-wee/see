package proc

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"see/internal/workspace"
)

// A secret longer than the digest window must still never appear in it:
// redaction runs before the trim, so the window's size is no longer the
// bound of what can be redacted. PEM bundles and certificate chains in
// the environment exceed the 4 KiB digest.
func TestTailRedactsOversizeSecret(t *testing.T) {
	secret := strings.Repeat("k", 5000) // longer than the 4 KiB digest
	env := []string{"PRIVATE_KEY=" + secret}
	const max = 4 << 10
	for _, chunks := range []struct {
		name  string
		write func(tail *Tail, data string)
	}{
		{"one write", func(tail *Tail, data string) { tail.Write([]byte(data)) }},
		{"byte at a time", func(tail *Tail, data string) {
			for i := 0; i < len(data); i++ {
				tail.Write([]byte{data[i]})
			}
		}},
		{"copy-sized chunks", func(tail *Tail, data string) {
			for len(data) > 0 {
				n := min(len(data), 512)
				tail.Write([]byte(data[:n]))
				data = data[n:]
			}
		}},
	} {
		t.Run(chunks.name, func(t *testing.T) {
			tail := NewTail(max, env)
			chunks.write(tail, "fatal: key rejected\n"+secret+"\n")
			out := tail.String()
			if strings.Contains(out, secret[:64]) {
				t.Fatalf("oversize secret leaked into the digest: %.120q", out)
			}
			if !strings.Contains(out, "<redacted>") {
				t.Fatalf("redaction marker missing: %q", out)
			}
			if !strings.Contains(out, "fatal: key rejected") {
				t.Fatalf("harmless diagnostic lost: %q", out)
			}
		})
	}
}

// A secret split across writes at an arbitrary byte must be redacted as
// surely as one written whole: stderr copies in whatever chunks the pipe
// delivers.
func TestTailRedactsSecretSplitAcrossWrites(t *testing.T) {
	secret := "0123456789abcdefSECRET-material-here"
	env := []string{"API_TOKEN=" + secret}
	for split := 1; split < len(secret); split++ {
		tail := NewTail(4<<10, env)
		data := "err: " + secret + " again"
		if _, err := tail.Write([]byte(data[:len("err: ")+split])); err != nil {
			t.Fatal(err)
		}
		if _, err := tail.Write([]byte(data[len("err: ")+split:])); err != nil {
			t.Fatal(err)
		}
		if out := tail.String(); strings.Contains(out, secret) {
			t.Fatalf("secret split at byte %d leaked: %q", split, out)
		}
	}
}

// A secret straddling the 4 KiB cut cannot match inside the window after
// trimming; streaming redaction removes it before the cut exists. The
// whole-lines policy dropping the partial first line is a second layer,
// not the defense.
func TestTailRedactsSecretStraddlingTheCut(t *testing.T) {
	secret := "0123456789abcdefSECRET-material-here"
	env := []string{"API_TOKEN=" + secret}
	const max = 4 << 10
	tail := NewTail(max, env)

	cut := len(secret) / 2
	pre := strings.Repeat("p", max-len(secret)-2) + "\n"
	stream := pre + secret + "\n" + strings.Repeat("f", cut+8)
	if len(stream) <= max {
		t.Fatalf("stream %d does not exceed the bound %d", len(stream), max)
	}
	if _, err := tail.Write([]byte(stream)); err != nil {
		t.Fatal(err)
	}
	out := tail.String()
	if strings.Contains(out, secret) {
		t.Fatalf("whole secret present unredacted: %q", out)
	}
	for i := 1; i < len(secret); i++ {
		if frag := secret[i:]; len(frag) >= 8 && strings.Contains(out, frag) {
			t.Fatalf("a %d-byte fragment %q of the secret is in the digest", len(frag), frag)
		}
	}
}

// Secrets near the end of the stream are not lost to the holdback: the
// final read flushes it, redacted like everything else.
func TestTailFlushesHeldBackSecretAtEnd(t *testing.T) {
	secret := "ending-secret-0123456789"
	env := []string{"TOKEN=" + secret}
	tail := NewTail(4<<10, env)
	if _, err := tail.Write([]byte("crash: " + secret)); err != nil {
		t.Fatal(err)
	}
	out := tail.String()
	if strings.Contains(out, secret) {
		t.Fatalf("trailing secret leaked: %q", out)
	}
	if !strings.Contains(out, "crash: <redacted>") {
		t.Fatalf("redacted context missing: %q", out)
	}
}

// Non-secret output shorter than the holdback still appears in full once
// read; the holdback delays, it does not swallow.
func TestTailWithoutSecretsKeepsEverything(t *testing.T) {
	tail := NewTail(4<<10, []string{"HOME=/home/user"})
	msg := "a line of plain output"
	if _, err := tail.Write([]byte(msg)); err != nil {
		t.Fatal(err)
	}
	if got := tail.String(); got != msg {
		t.Fatalf("plain output mangled: %q", got)
	}
}

func TestDiskSpellingCorrectsCaseVariants(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("case behavior differs")
	}
	base := t.TempDir()
	dir := filepath.Join(base, "MixedCaseRoot")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	variant := filepath.Join(base, "mixedcaseroot", "SUB")
	if _, err := os.Lstat(variant); err != nil {
		t.Skipf("filesystem is case-sensitive; nothing to correct: %v", err)
	}
	// DiskSpelling is lexical — it never resolves symlinks — so the
	// expectation is spelled from the same root the inputs are.
	want := filepath.Join(base, "MixedCaseRoot", "sub")
	if got := workspace.DiskSpelling(filepath.Join(dir, "sub")); got != want {
		t.Fatalf("control failed: exact spelling changed: %q", got)
	}
	if got := workspace.DiskSpelling(variant); got != want {
		t.Fatalf("case variant not corrected: %q, want %q", got, want)
	}
	// What does not exist keeps its spelling: nothing deeper can be probed.
	missing := filepath.Join(base, "mixedcaseroot", "sub", "NoSuchThing")
	if got := workspace.DiskSpelling(missing); got != filepath.Join(want, "NoSuchThing") {
		t.Fatalf("missing component not preserved verbatim under a corrected parent: %q", got)
	}
}

func TestDiskSpellingCorrectsUnicodeNormalization(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("normalization-insensitive volume needed")
	}
	base := t.TempDir()
	// macOS volumes compare names normalization-insensitively: the NFC
	// spelling of é and its NFD decomposition name one file.
	nfc := "café"
	nfd := "café"
	if nfc == nfd {
		t.Fatal("fixture broken: spellings identical")
	}
	dir := filepath.Join(base, nfc)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(base, nfd)); err != nil {
		t.Skipf("volume is normalization-sensitive: %v", err)
	}
	if got := workspace.DiskSpelling(filepath.Join(base, nfd)); got != dir {
		t.Fatalf("normalization variant not corrected: %q, want %q", got, dir)
	}
}

// The case variant of a workspace executable names the same bytes on a
// case-insensitive file system; the trust check must deny it as it denies
// the exact spelling. filepath.EvalSymlinks does not normalize case, so
// this is the regression the disk spelling exists for.
func TestValidateExecutableDeniesCaseVariant(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("case behavior differs")
	}
	base := t.TempDir()
	root := filepath.Join(base, "MixedCaseRoot")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(root, "tool")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	variant := filepath.Join(base, "mixedcaseroot", "tool")
	if _, err := os.Lstat(variant); err != nil {
		t.Skipf("filesystem is case-sensitive; the evasion does not exist here: %v", err)
	}
	if err := ValidateExecutable(root, exe); !errors.Is(err, ErrUntrustedExecutable) {
		t.Fatalf("control failed: exact workspace path not denied: %v", err)
	}
	if err := ValidateExecutable(root, variant); !errors.Is(err, ErrUntrustedExecutable) {
		t.Fatalf("case variant of a workspace executable evaded the trust check: %v", err)
	}
	// A case-variant root must not invert the result either: the same
	// bytes are the workspace however spelled.
	if err := ValidateExecutable(filepath.Join(base, "mixedcaseroot"), variant); !errors.Is(err, ErrUntrustedExecutable) {
		t.Fatalf("case-variant root and executable evaded the trust check: %v", err)
	}
	// An executable outside the workspace still passes.
	outside := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(outside, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ValidateExecutable(root, outside); err != nil {
		t.Fatalf("outside executable denied: %v", err)
	}
}
