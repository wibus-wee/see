package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"slices"
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
	if err := Run([]string{os.DevNull}, &out); err == nil || !strings.Contains(err.Error(), "expected a file or folder") {
		t.Fatalf("device %s: %v", os.DevNull, err)
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
	for _, name := range []string{"see", filepath.Join(".local", "bin", "see")} {
		if _, ok := Invocation(name, nil); !ok {
			t.Errorf("Invocation(%q) = false", name)
		}
	}
	for _, name := range []string{"See", "/Applications/See.app/Contents/MacOS/See", "other", "See.exe", "see.exe", "SEE.EXE", filepath.Join("Program Files", "See", "See.exe")} {
		if _, ok := Invocation(name, nil); ok {
			t.Errorf("Invocation(%q) = true", name)
		}
	}
}

func TestInvocationDistinguishesDesktopLaunchFromLinuxShellCommand(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux desktop entry")
	}
	dir := t.TempDir()
	desktopExecutable := filepath.Join(dir, "see")
	if err := os.WriteFile(desktopExecutable, nil, 0755); err != nil {
		t.Fatal(err)
	}
	if _, ok := Invocation(desktopExecutable, nil); ok {
		t.Fatal("desktop launch was treated as a CLI invocation")
	}
	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{"explicit path", []string{"/project"}, []string{"/project"}},
		{"RunArg without path", []string{RunArg}, nil},
		{"RunArg with path", []string{RunArg, "/project"}, []string{"/project"}},
	} {
		got, ok := Invocation(desktopExecutable, tc.args)
		if !ok || !slices.Equal(got, tc.want) {
			t.Errorf("Invocation(%q, %q) = %q, %v; want %q, true", desktopExecutable, tc.args, got, ok, tc.want)
		}
	}
	cliExecutable := filepath.Join(dir, "see-cli")
	if err := os.WriteFile(cliExecutable, nil, 0755); err != nil {
		t.Fatal(err)
	}
	cliLink := filepath.Join(dir, "see")
	if err := os.Remove(cliLink); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cliExecutable, cliLink); err != nil {
		t.Fatal(err)
	}
	if _, ok := Invocation(cliLink, nil); !ok {
		t.Fatal("the installed CLI symlink was not treated as a CLI invocation")
	}
}

func TestInvocationWindowsLauncher(t *testing.T) {
	for _, args := range [][]string{nil, {"path with spaces"}} {
		got, ok := Invocation("See.exe", append([]string{RunArg}, args...))
		if !ok || !slices.Equal(got, args) {
			t.Errorf("CLI invocation = %q, %v; want %q, true", got, ok, args)
		}
	}
	for _, name := range []string{"See.exe", "see"} {
		if _, ok := Invocation(name, []string{OpenArg, "project"}); ok {
			t.Errorf("Invocation(%q) matched an app handoff", name)
		}
	}
}

func TestOpenPath(t *testing.T) {
	wd := t.TempDir()
	absolute := filepath.Join(wd, "absolute")
	for _, tc := range []struct {
		args []string
		want string
		ok   bool
	}{
		{[]string{OpenArg, absolute}, absolute, true},
		{[]string{"rel"}, filepath.Join(wd, "rel"), true},
		{[]string{"-psn_0_1"}, "", false},
		{nil, "", false},
	} {
		got, ok := OpenPath(tc.args, wd)
		if got != tc.want || ok != tc.ok {
			t.Errorf("OpenPath(%q) = %q, %v; want %q, %v", tc.args, got, ok, tc.want, tc.ok)
		}
	}
}

func TestParseFlags(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		want  Request
		fails bool
	}{
		{[]string{"--diff", "a", "b"}, Request{Diff: [2]string{"a", "b"}}, false},
		{[]string{"--diff", "a"}, Request{}, true},
		{[]string{"--diff", "a", "b", "c"}, Request{}, true},
		{[]string{"-"}, Request{Stdin: true}, false},
		{[]string{"--stdin"}, Request{Stdin: true}, false},
		{[]string{"--stdin", "file"}, Request{}, true},
		{[]string{"--diff", "a", "b", "-"}, Request{}, true},
		{[]string{"--new-window", "p"}, Request{Path: "p", NewWindow: true}, false},
		{[]string{"--new-window"}, Request{NewWindow: true}, false},
		{[]string{"--diff", "a", "b", "--new-window"}, Request{Diff: [2]string{"a", "b"}, NewWindow: true}, false},
		{[]string{"--bogus"}, Request{}, true},
		{[]string{"--", "p"}, Request{Path: "p"}, false},
	} {
		got, err := parse(tc.args)
		if tc.fails != (err != nil) || (!tc.fails && got != tc.want) {
			t.Errorf("parse(%q) = %+v, %v; want %+v, fails=%v", tc.args, got, err, tc.want, tc.fails)
		}
	}
}

func TestOpenRequestReadsTheHandoff(t *testing.T) {
	wd := t.TempDir()
	rel := func(parts ...string) string { return filepath.Join(append([]string{wd}, parts...)...) }
	for _, tc := range []struct {
		args []string
		want Request
		ok   bool
	}{
		{[]string{OpenArg, rel("p")}, Request{Path: rel("p")}, true},
		{[]string{OpenArg, "--new-window", rel("p")}, Request{Path: rel("p"), NewWindow: true}, true},
		{[]string{OpenStdinArg, rel("stdin")}, Request{Path: rel("stdin"), Stdin: true}, true},
		{[]string{OpenDiffArg, rel("a"), rel("b")}, Request{Diff: [2]string{rel("a"), rel("b")}}, true},
		{[]string{OpenDiffArg, "--new-window", rel("a"), rel("b")}, Request{Diff: [2]string{rel("a"), rel("b")}, NewWindow: true}, true},
		{[]string{OpenDiffArg, rel("a")}, Request{}, false},
		{[]string{OpenDiffArg, rel("a"), rel("b"), rel("c")}, Request{}, false},
		{[]string{OpenArg}, Request{}, false},
		{nil, Request{}, false},
	} {
		got, ok := OpenRequest(tc.args, wd)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("OpenRequest(%q) = %+v, %v; want %+v, %v", tc.args, got, ok, tc.want, tc.ok)
		}
	}
}

func TestOpenRequestResolvesRelativePaths(t *testing.T) {
	wd := t.TempDir()
	got, ok := OpenRequest([]string{OpenDiffArg, "a", "b"}, wd)
	if !ok || got.Diff[0] != filepath.Join(wd, "a") || got.Diff[1] != filepath.Join(wd, "b") {
		t.Fatalf("OpenRequest = %+v, %v", got, ok)
	}
}

func TestWriteStdinKeepsThePipe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "in")
	if err := writeStdin(path, strings.NewReader("piped\n")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "piped\n" {
		t.Fatalf("stdin file = %q, %v", got, err)
	}
}
