package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const cliOpenArg = "--see-cli-open"

// runCLI starts a separate GUI process so the invoking shell is not held open.
func runCLI(args []string, stderr io.Writer) error {
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
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("cannot open %s: %w", path, err)
	}
	if !fi.IsDir() && !fi.Mode().IsRegular() {
		return fmt.Errorf("cannot open %s: expected a file or folder", path)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, cliOpenArg, abs)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("cannot start See: %w", err)
	}
	return cmd.Process.Release()
}

func isCLIInvocation(arg0 string) bool {
	name := filepath.Base(arg0)
	return name == "see" || (strings.EqualFold(filepath.Ext(name), ".exe") && strings.EqualFold(name, "see.exe"))
}

func (a *app) openCLIPath(path string) {
	path, err := filepath.Abs(path)
	if err != nil {
		a.welcomeNote = fmt.Sprintf("Could not open %s: %v", path, describeErr(err))
		return
	}
	fi, err := os.Stat(path)
	if err != nil {
		a.welcomeNote = fmt.Sprintf("Could not open %s: %v", path, describeErr(err))
		return
	}
	if fi.IsDir() {
		a.openWorkspace(path)
		return
	}
	root := filepath.Dir(path)
	a.openWorkspace(root)
	if a.root == root {
		a.openFile(path, nil)
	}
}
