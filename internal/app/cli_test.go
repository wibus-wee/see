package app

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIPathOpensFileOrFolder(t *testing.T) {
	root := writeTree(t, map[string]string{"main.go": "package main\n"})
	h := newHarness(t, "")
	h.a.openCLIPath(filepath.Join(root, "main.go"))
	if h.a.root != root || h.a.activePath() != filepath.Join(root, "main.go") {
		t.Fatalf("file: root=%q active=%q", h.a.root, h.a.activePath())
	}
	h.a.closeWorkspace()
	h.a.openCLIPath(root)
	if h.a.root != root {
		t.Fatalf("folder: root=%q", h.a.root)
	}
}

func TestCLIReportsInvalidArguments(t *testing.T) {
	var out bytes.Buffer
	if err := runCLI([]string{"--help"}, &out); err != nil || !strings.Contains(out.String(), "see [file-or-folder]") {
		t.Fatalf("help: %q, %v", out.String(), err)
	}
	if err := runCLI([]string{"one", "two"}, &out); err == nil {
		t.Fatal("accepted multiple paths")
	}
	if err := runCLI([]string{filepath.Join(t.TempDir(), "missing")}, &out); err == nil {
		t.Fatal("accepted missing path")
	}
}
