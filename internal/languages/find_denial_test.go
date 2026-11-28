package languages

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"see/internal/config"
	"see/internal/operr"
	"see/internal/proc"
)

// writeExecutable creates a file the finders will stat; the tests never
// execute it, so no platform extension is needed.
func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// A configured server pointing at a workspace binary is denied by the
// trust boundary. The user installed the software and pointed at it —
// reporting "not installed" sends them to install what they have.
func TestFindConfiguredReportsWorkspaceDenialAsDenial(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "myserver")
	writeExecutable(t, exe)
	_, err := FindConfigured(root, config.LSPConfig{Name: "myserver", Command: []string{exe}})
	if !errors.Is(err, proc.ErrUntrustedExecutable) {
		t.Fatalf("workspace-resident server not a typed denial: %v", err)
	}
	if strings.Contains(err.Error(), "not installed") {
		t.Fatalf("denial misreported as missing software: %q", err.Error())
	}
	// A binary outside the workspace still resolves.
	outside := filepath.Join(t.TempDir(), "myserver")
	writeExecutable(t, outside)
	if _, err := FindConfigured(root, config.LSPConfig{Name: "myserver", Command: []string{outside}}); err != nil {
		t.Fatalf("outside server denied: %v", err)
	}
	// A missing binary still reads as missing, distinctly from a denial.
	_, err = FindConfigured(root, config.LSPConfig{Name: "myserver", Command: []string{"see-no-such-server-xyz"}})
	if err == nil || errors.Is(err, proc.ErrUntrustedExecutable) || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("missing server misclassified: %v", err)
	}
}

// The environment variable a user sets is their explicit choice: when it
// points into the workspace, every finder reports the denial, not a
// missing installation.
func TestExplicitEnvCandidatesReportDenial(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		env  string
		name string
		find func(string) (string, error)
	}{
		{"SEE_CLANGD", "clangd", FindClangd},
		{"SEE_CMAKE_LANGUAGE_SERVER", "cmake-language-server", FindCMakeServer},
		{"SEE_TYPESCRIPT_LANGUAGE_SERVER", "tsrs", FindTypeScript},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exe := filepath.Join(root, tc.name)
			writeExecutable(t, exe)
			t.Setenv(tc.env, exe)
			_, err := tc.find(root)
			if !errors.Is(err, proc.ErrUntrustedExecutable) {
				t.Fatalf("explicit workspace candidate not a typed denial: %v", err)
			}
			t.Setenv(tc.env, "")
		})
	}
}

// A gopls the user pointed at through $SEE_GOPLS is explicit; one found
// on PATH is not — a PATH copy inside the workspace is silently unusable,
// as before, because the user never asked for that one.
func TestFindGoplsEnvDenialVersusPathSkip(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "gopls")
	writeExecutable(t, exe)
	t.Setenv("SEE_GOPLS", exe)
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")
	t.Setenv("PATH", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if _, err := FindGopls(root); !errors.Is(err, proc.ErrUntrustedExecutable) {
		t.Fatalf("explicit $SEE_GOPLS workspace candidate not a typed denial: %v", err)
	}
	// Without the explicit variable, nothing usable exists and the
	// outcome is the ordinary missing-gopls error, which Recover retries.
	t.Setenv("SEE_GOPLS", "")
	if _, err := FindGopls(root); !errors.Is(err, ErrNoGopls) {
		t.Fatalf("implicit search outcome changed: %v", err)
	}
	// A trusted fallback wins over a denied explicit candidate: the
	// boundary forbids the workspace copy, not gopls itself.
	outside := filepath.Join(t.TempDir(), "gopls")
	writeExecutable(t, outside)
	t.Setenv("SEE_GOPLS", exe)
	got, err := FindGopls(root, outside)
	if err != nil || got != outside {
		t.Fatalf("trusted fallback not used: %q, %v", got, err)
	}
}

// Denials classify as permission-denied permanent failures, the kind the
// launch boundary uses, so surfaces that classify errors render them
// alike however far upstream they were produced.
func TestFinderDenialCarriesLaunchClassification(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "myserver")
	writeExecutable(t, exe)
	_, err := FindConfigured(root, config.LSPConfig{Name: "myserver", Command: []string{exe}})
	if !errors.Is(err, proc.ErrUntrustedExecutable) {
		t.Fatalf("denial lost its cause: %v", err)
	}
	var failure *operr.Error
	if !errors.As(err, &failure) {
		t.Fatalf("denial is not classified: %T", err)
	}
	if failure.Kind != operr.PermissionDenied || failure.Status != operr.Permanent {
		t.Fatalf("denial classification = %s (%s), want permission-denied (permanent)", failure.Kind, failure.Status)
	}
}
