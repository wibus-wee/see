package proc

// Independent re-verification probes for the case-variant trust fix
// (verify-lane-a F2), following the original attack scenario literally:
// a workspace at one path, an executable inside it, reached through a
// differently-cased spelling of the same bytes.

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func caseInsensitiveOrSkip(t *testing.T, variant string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("windows path semantics differ; covered by the platform's own CI")
	}
	if _, err := os.Lstat(variant); err != nil {
		t.Skipf("volume is case-sensitive; the evasion does not exist here: %v", err)
	}
}

// The F2 scenario as reported: workspace /tmp/ws, tool /tmp/ws/tool,
// attack through /tmp/WS/tool and /Tmp/ws/tool. Uses a fresh tree under
// /tmp so the scenario matches the report's shape; skips if the volume
// does not fold case.
func TestReverifyCaseVariantDenialInTmpScenario(t *testing.T) {
	base, err := os.MkdirTemp("/tmp", "reverify-ws-*")
	if err != nil {
		t.Fatalf("cannot build the /tmp scenario: %v", err)
	}
	defer os.RemoveAll(base)
	ws := filepath.Join(base, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(ws, "tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	upperWS := filepath.Join(base, "WS", "tool")
	mixed := filepath.Join(filepath.Dir(base), strings.ToUpper(filepath.Base(base)), "ws", "tool")
	caseInsensitiveOrSkip(t, upperWS)

	// Controls: exact spelling denied, outside executable allowed.
	if err := ValidateExecutable(ws, tool); !errors.Is(err, ErrUntrustedExecutable) {
		t.Fatalf("control failed: exact workspace path not denied: %v", err)
	}
	if err := ValidateExecutable(ws, upperWS); !errors.Is(err, ErrUntrustedExecutable) {
		t.Fatalf("/WS/tool case variant evaded the trust check: %v", err)
	}
	if err := ValidateExecutable(ws, mixed); !errors.Is(err, ErrUntrustedExecutable) {
		t.Fatalf("uppercased-parent variant evaded the trust check: %v", err)
	}

	outside := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(outside, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ValidateExecutable(ws, outside); err != nil {
		t.Fatalf("outside executable must pass: %v", err)
	}
}

// Variant attacks beyond plain case: deep components cased at multiple
// levels at once, and a variant of the root itself paired with a variant
// of the executable.
func TestReverifyDeepCaseVariants(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "WorkSpace")
	deep := filepath.Join(root, "SubDir", "Bin")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(deep, "srv")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	variant := filepath.Join(base, "workspace", "subdir", "bin", "SRV")
	caseInsensitiveOrSkip(t, variant)

	if err := ValidateExecutable(root, variant); !errors.Is(err, ErrUntrustedExecutable) {
		t.Fatalf("deep multi-level case variant evaded: %v", err)
	}
	if err := ValidateExecutable(filepath.Join(base, "WORKSPACE"), exe); !errors.Is(err, ErrUntrustedExecutable) {
		t.Fatalf("case-variant root with exact executable evaded: %v", err)
	}
}

// The fix must not deny what is genuinely outside: on a case-insensitive
// volume a sibling directory whose name shares only a prefix is not
// inside the workspace, however spelled.
func TestReverifySiblingPrefixNotDenied(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "ws")
	sibling := filepath.Join(base, "ws2")
	for _, d := range []string{root, sibling} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	exe := filepath.Join(sibling, "tool")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ValidateExecutable(root, exe); err != nil {
		t.Fatalf("sibling executable wrongly denied: %v", err)
	}
	// And its case variant, the mirror image of the attack: on a folding
	// volume WS2/tool is still outside ws.
	variant := filepath.Join(base, "WS2", "tool")
	if _, err := os.Lstat(variant); err == nil {
		if err := ValidateExecutable(root, variant); err != nil {
			t.Fatalf("case-variant sibling executable wrongly denied: %v", err)
		}
	}
}

// Unicode normalization variants name the same file on macOS volumes:
// the NFC workspace reached through an NFD spelling must deny equally.
func TestReverifyUnicodeNormalizationVariantDenied(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("normalization-insensitive volume needed")
	}
	base := t.TempDir()
	nfc := "w\u00f6rkspace"  // ö as one precomposed codepoint
	nfd := "wo\u0308rkspace" // o + combining diaeresis, the NFD spelling
	if nfc == nfd {
		t.Fatal("fixture broken: spellings identical")
	}
	root := filepath.Join(base, nfc)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(root, "tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	variant := filepath.Join(base, nfd, "tool")
	if _, err := os.Lstat(variant); err != nil {
		t.Skipf("volume is normalization-sensitive: %v", err)
	}
	if err := ValidateExecutable(root, variant); !errors.Is(err, ErrUntrustedExecutable) {
		t.Fatalf("unicode normalization variant evaded the trust check: %v", err)
	}
}
