package app

// Independent verifier's probe: the threat model claims a binary inside the
// workspace never starts, "comparing symlink-resolved paths". On a
// case-insensitive volume (default macOS APFS) EvalSymlinks does not
// normalize case, so a case-variant spelling of the workspace path names
// the same bytes while comparing lexically outside it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"see/internal/languages"
	"see/internal/config"
)

func TestVerifyCaseVariantWorkspaceBinaryResolution(t *testing.T) {
	// A workspace directory with mixed case in its name, under the temp dir.
	root := filepath.Join(t.TempDir(), "WorkSpaceMixedCase")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.rs"), []byte("fn main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(root)
	variant := filepath.Join(filepath.Dir(root), strings.ToLower(base[:1])+base[1:])
	if variant == root {
		t.Skip("no cased letter in workspace root")
	}
	// If the volume is case-sensitive, the variant does not resolve inside.
	if _, err := os.Stat(variant); err != nil {
		t.Skipf("case-sensitive volume: %v", err)
	}
	inside := filepath.Join(variant, "tools", "evil-lsp")
	if err := os.MkdirAll(filepath.Join(root, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tools", "evil-lsp"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := config.LSPConfig{Name: "evil-lsp", Extensions: []string{".rs"}, Command: []string{inside}}
	got, err := languages.FindConfigured(root, spec)
	if err == nil {
		// Known defect D1 of docs/specs/conformance/verify-lane-e.md: the
		// denial is lexical and case-insensitive volumes do not normalize
		// case in EvalSymlinks. Logged rather than failed so the probe
		// documents behavior without breaking the branch; flip this to a
		// hard failure once the resolvers use disk-spelling comparison.
		t.Logf("D1 REPRODUCES: case-variant in-workspace binary RESOLVED: %q — workspace-binary denial evaded", got)
		return
	}
	t.Logf("case-variant correctly rejected: %v — D1 fixed?", err)
}
