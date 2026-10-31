// Package cli is See's command line: the arguments it takes, and starting
// the app for a file or a folder as a process of its own, so that the
// shell that asked is not held open.
package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// OpenArg is the argument the app process is started with to open a path,
// which tells it apart from a command line invocation.
const OpenArg = "--see-cli-open"

// Run opens what the arguments name, or explains how to. It starts the
// app and returns; see the usage errors when the arguments name nothing
// See can open.
func Run(args []string, stderr io.Writer) error {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		_, err := fmt.Fprintln(stderr, "Usage: see [file-or-folder]")
		return err
	}
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: see [file-or-folder]")
	}
	path := "."
	if len(args) == 1 {
		path = args[0]
	}
	abs, err := Openable(path)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, OpenArg, abs)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("cannot start See: %w", err)
	}
	return cmd.Process.Release()
}

// Openable resolves a path the command line names, which must be a file or
// a folder that exists.
func Openable(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("cannot open %s: %w", path, err)
	}
	if !fi.IsDir() && !fi.Mode().IsRegular() {
		return "", fmt.Errorf("cannot open %s: expected a file or folder", path)
	}
	return abs, nil
}

// Invocation reports whether the process was started as `see` rather than
// as the app itself, which takes the command line.
func Invocation(arg0 string) bool {
	name := filepath.Base(arg0)
	return name == "see" || strings.EqualFold(name, "see.exe")
}
