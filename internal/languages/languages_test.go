package languages

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestTypeScriptLanguage(t *testing.T) {
	cases := map[string]string{
		"a.ts":   "typescript",
		"a.tsx":  "typescriptreact",
		"a.d.ts": "typescript", // the last extension decides
		"a.js":   "javascript",
		"a.mjs":  "javascript",
		"a.cjs":  "javascript",
		"a.jsx":  "javascriptreact",
		"a.go":   "",
		"noext":  "",
	}
	for name, want := range cases {
		if got := TypeScriptLanguage(name); got != want {
			t.Errorf("TypeScriptLanguage(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestFindGoplsPrefersTheEnvironment(t *testing.T) {
	root := t.TempDir()
	bin := t.TempDir() // outside the workspace, which never lends its own gopls
	fake := filepath.Join(bin, "gopls")
	if runtime.GOOS == "windows" {
		fake += ".exe"
	}
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEE_GOPLS", fake)
	got, err := FindGopls(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != fake {
		t.Fatalf("FindGopls = %q, want %q", got, fake)
	}
}

func TestFindGoplsSaysWhenThereIsNone(t *testing.T) {
	// A workspace never lends its own gopls, and with nothing on the
	// caller's PATH the search fails with what to install.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "gopls"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEE_GOPLS", "")
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if got, err := FindGopls(root); err == nil {
		t.Fatalf("FindGopls found %q in the workspace", got)
	}
}

func TestManagedTypeScriptPaths(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("APPDATA", dir)
	managed := ManagedTypeScriptDir()
	if managed == "" {
		t.Fatal("no managed folder")
	}
	if want := filepath.Join(dir, "See", "language-servers", "tsrs"); managed != want {
		t.Fatalf("ManagedTypeScriptDir = %q, want %q", managed, want)
	}
	if entry := ManagedTypeScriptEntry(); entry != filepath.Join(managed, "node_modules", "tsrs", "bin", "tsrs") {
		t.Fatalf("ManagedTypeScriptEntry = %q", entry)
	}
	if ManagedTypeScriptExists() || ManagedTypeScriptInstalled() {
		t.Fatal("an installation that is not there is reported")
	}
	if err := os.MkdirAll(managed, 0o700); err != nil {
		t.Fatal(err)
	}
	if !ManagedTypeScriptExists() {
		t.Fatal("the folder exists but is not reported")
	}
	if ManagedTypeScriptInstalled() {
		t.Fatal("a folder without the server is reported as installed")
	}
}
