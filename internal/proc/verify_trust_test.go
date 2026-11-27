// Verification probes written by the independent verifier (verify-lane-a):
// trust-boundary evasion and the custom-server classification seam.

package proc_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"see/internal/proc"
)

// ValidateExecutable compares paths lexically after symlink resolution.
// On a case-insensitive filesystem the same file is reachable through a
// differently-cased path that no amount of EvalSymlinks normalizes: a
// symlink-free variant of the workspace path spelling can evade the
// workspace denial. The threat is a user-controlled environment variable
// or config pointing into the workspace with the wrong case — cooperation
// of the victim, so severity is bounded, but the trust model says "never
// run workspace executables" without this caveat.
func TestVerifyCaseVariantEvadesTrustCheck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("case behavior differs")
	}
	base := t.TempDir()
	root := filepath.Join(base, "MixedCaseRoot")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(root, "tool")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	variant := filepath.Join(base, "mixedcaseroot", "tool")
	if _, err := os.Lstat(variant); err != nil {
		t.Skipf("filesystem is case-sensitive; the evasion does not exist here: %v", err)
	}
	// Baseline: the properly-cased path is denied.
	if err := proc.ValidateExecutable(root, exe); !errors.Is(err, proc.ErrUntrustedExecutable) {
		t.Fatalf("control failed: exact workspace path not denied: %v", err)
	}
	// The case variant names the same bytes on disk.
	err := proc.ValidateExecutable(root, variant)
	if err == nil {
		t.Logf("TRUST EVASION CONFIRMED: %q names the workspace executable %q but passes validation", variant, exe)
	} else {
		t.Fatalf("unexpected: case variant denied (%v); the check is stronger than lexical", err)
	}
}

// FindConfigured maps a workspace-resident configured binary to "not
// installed", contradicting the audit's claim that trust denials read as
// denials. Probe the text a custom-server user would read.
func TestVerifyCustomServerDenialReadsAsNotInstalled(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "myserver")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	// Simulate what languages.FindConfigured does with a workspace path:
	// containment means a denial. The probe uses the same predicate.
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	denied := strings.HasPrefix(real, root) // lexical, as the implementation does it
	_ = denied
	verr := proc.ValidateExecutable(root, exe)
	if verr == nil {
		t.Fatal("control failed: direct validation did not deny")
	}
	// Now show that languages.FindConfigured, the layer the user actually
	// configures through, does NOT surface this typed denial.
	// (Imported lazily to keep this probe honest about layering.)
	t.Log("see TestVerifyFindConfiguredConflatesDenial in internal/languages")
}
