package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunExplainsItsUsage(t *testing.T) {
	var out bytes.Buffer
	if err := Run([]string{"--help"}, &out); err != nil {
		t.Fatalf("help: %v", err)
	}
	if !strings.Contains(out.String(), "see [file-or-folder]") {
		t.Fatalf("help = %q", out.String())
	}
}

func TestRunRejectsWhatItCannotOpen(t *testing.T) {
	var out bytes.Buffer
	if err := Run([]string{"one", "two"}, &out); err == nil {
		t.Fatal("accepted two paths")
	}
	missing := filepath.Join(t.TempDir(), "missing")
	err := Run([]string{missing}, &out)
	if err == nil || !strings.Contains(err.Error(), "cannot open") {
		t.Fatalf("missing path: %v", err)
	}
	// A device is neither a file nor a folder, and is refused before any
	// process starts.
	if err := Run([]string{"/dev/null"}, &out); err == nil || !strings.Contains(err.Error(), "expected a file or folder") {
		t.Fatalf("/dev/null: %v", err)
	}
}

func TestOpenableResolves(t *testing.T) {
	dir := t.TempDir()
	got, err := Openable(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := filepath.Abs(dir); got != want {
		t.Fatalf("Openable(%q) = %q, want %q", dir, got, want)
	}
}

func TestInvocationRecognizesTheCommand(t *testing.T) {
	for _, name := range []string{"see", "/usr/local/bin/see", "see.exe", "SEE.EXE"} {
		if !Invocation(name) {
			t.Errorf("Invocation(%q) = false", name)
		}
	}
	for _, name := range []string{"See", "/Applications/See.app/Contents/MacOS/See", "other"} {
		if Invocation(name) {
			t.Errorf("Invocation(%q) = true", name)
		}
	}
}
