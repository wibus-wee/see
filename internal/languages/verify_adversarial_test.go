// Verification probes written by the independent verifier (verify-lane-a):
// the custom-server trust-denial seam. The audit claims trust denials read
// as denials in settings and find surfaces; this test checks what a user
// who configured a workspace-resident server actually reads.

package languages

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"see/internal/config"
)

// FindConfigured denies a workspace-resident binary by reporting "not
// installed" — the exact conflation REQ-EXT-020 forbids at the launch
// boundary and the audit claimed fixed in the settings/find surfaces.
// The custom-server path was not one of those surfaces.
func TestVerifyFindConfiguredConflatesDenial(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "myserver")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	_, err := FindConfigured(root, config.LSPConfig{Name: "myserver", Command: []string{exe}})
	if err == nil {
		t.Fatal("workspace binary accepted for a configured server")
	}
	text := err.Error()
	if strings.Contains(text, "not installed") && !strings.Contains(text, "denied") && !strings.Contains(text, "trust") {
		t.Logf("CLASSIFICATION CONFIRMED WRONG: workspace-resident server binary reads as missing: %q", text)
	} else {
		t.Fatalf("unexpected: denial surfaced honestly: %q", text)
	}
}
