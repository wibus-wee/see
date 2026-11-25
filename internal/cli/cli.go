// Package cli is See's command line: the arguments it takes, and starting
// the app for a file or a folder as a process of its own, so that the
// shell that asked is not held open.
package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"see/internal/proc"
)

// OpenArg is the argument the app process is started with to open a path,
// which tells it apart from a command line invocation.
const OpenArg = "--see-cli-open"

// OpenDiffArg starts the app process showing the diff of the two paths
// after it.
const OpenDiffArg = "--see-cli-open-diff"

// OpenStdinArg starts the app process with the file holding what the
// shell's standard input had, which it reads into an untitled document.
const OpenStdinArg = "--see-cli-stdin"

// RunArg selects CLI mode when a shell launcher starts the executable.
const RunArg = "--see-cli"

// StdinPath is where `see -` writes the standard input for the app
// process to pick up, in the user's cache; the last invocation wins.
func StdinPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "See", "stdin"), nil
}

const usage = `Usage: see [file-or-folder]
       see --diff <a> <b>     show the diff of two files or directories
       see -                  read the standard input into an untitled document
       see --new-window <p>   open in a window of its own, even if it is open`

var errHelp = fmt.Errorf("help")

// Request is what an invocation asks for: a diff of two paths, the
// standard input as an untitled document, or a path to open, and
// whether it opens in a window of its own.
type Request struct {
	Diff      [2]string // the two paths compared; [0] == "" when none
	Stdin     bool
	NewWindow bool
	Path      string // the path to open, or the file stdin is kept in
}

// parse reads the arguments as the shell command does. "-" alone names
// the standard input, as "--stdin" does.
func parse(args []string) (Request, error) {
	var req Request
	onePath := fmt.Errorf("usage: see [file-or-folder]")
	for i := 0; i < len(args); i++ {
		arg := args[i]
		take := func() (string, bool) {
			if i+1 >= len(args) {
				return "", false
			}
			i++
			return args[i], true
		}
		switch {
		case arg == "--":
			p, ok := take()
			if !ok {
				return req, nil
			}
			if req.Path != "" || i+1 < len(args) {
				return req, onePath
			}
			req.Path = p
		case arg == "--help" || arg == "-h":
			return req, errHelp
		case arg == "--diff":
			a, oka := take()
			b, okb := take()
			if !oka || !okb {
				return req, fmt.Errorf("usage: see --diff <file-or-dir> <file-or-dir>")
			}
			req.Diff = [2]string{a, b}
		case arg == "--new-window":
			req.NewWindow = true
		case arg == "--stdin":
			req.Stdin = true
		case strings.HasPrefix(arg, "-") && arg != "-":
			return req, fmt.Errorf("unknown flag %s\n%s", arg, usage)
		default:
			if req.Path != "" {
				return req, onePath
			}
			req.Path = arg
		}
	}
	if req.Path == "-" {
		req.Path, req.Stdin = "", true
	}
	switch {
	case req.Diff[0] != "" && (req.Path != "" || req.Stdin):
		return req, fmt.Errorf("usage: see --diff <file-or-dir> <file-or-dir>")
	case req.Stdin && req.Path != "":
		return req, fmt.Errorf("usage: see - reads the standard input instead of a file")
	}
	return req, nil
}

// Run opens what the arguments name, or explains how to. It starts the
// app and returns; see the usage errors when the arguments name nothing
// See can open.
func Run(args []string, stderr io.Writer) error {
	req, err := parse(args)
	if err == errHelp {
		_, err := fmt.Fprintln(stderr, usage)
		return err
	}
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
	start := func(argv ...string) error {
		cmd := proc.Command(exe, argv...)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("cannot start See: %w", err)
		}
		return cmd.Process.Release()
	}
	switch {
	case req.Diff[0] != "":
		// Each side must exist as a file or a folder, so a typo fails
		// here rather than in a window that opens anyway.
		for i, p := range req.Diff {
			if req.Diff[i], err = Openable(p); err != nil {
				return err
			}
		}
		argv := []string{OpenDiffArg}
		if req.NewWindow {
			argv = append(argv, "--new-window")
		}
		return start(append(argv, req.Diff[0], req.Diff[1])...)
	case req.Stdin:
		path, err := StdinPath()
		if err != nil {
			return err
		}
		if err := writeStdin(path, os.Stdin); err != nil {
			return err
		}
		return start(handoffArgs(req, OpenStdinArg, path)...)
	default:
		path := req.Path
		if path == "" {
			path = "."
		}
		abs, err := Openable(path)
		if err != nil {
			return err
		}
		return start(handoffArgs(req, OpenArg, abs)...)
	}
}

// handoffArgs is the argument list the app process is started with for
// what the invocation asked, its flags between the marker and the path.
func handoffArgs(req Request, marker, path string) []string {
	argv := []string{marker}
	if req.NewWindow {
		argv = append(argv, "--new-window")
	}
	return append(argv, path)
}

// writeStdin puts what the pipe holds at path, atomically; the app
// process reads and removes it.
func writeStdin(path string, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("cannot read stdin: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".stdin-")
	if err != nil {
		return fmt.Errorf("cannot read stdin: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := io.Copy(tmp, r); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("cannot read stdin: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cannot read stdin: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("cannot read stdin: %w", err)
	}
	return nil
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

// Invocation extracts the shell command's arguments, if this is a CLI launch.
func Invocation(arg0 string, args []string) ([]string, bool) {
	if len(args) != 0 {
		switch args[0] {
		case RunArg:
			return args[1:], true
		case OpenArg, OpenDiffArg, OpenStdinArg:
			return nil, false
		}
	}
	command := filepath.Base(arg0) == "see"
	if command && runtime.GOOS == "linux" && len(args) == 0 && filepath.IsAbs(arg0) {
		// Linux desktop entries invoke the absolute executable; the installed
		// shell command is a symlink. This distinguishes GUI launch without
		// changing generated desktop entries or shell command behavior.
		info, err := os.Lstat(arg0)
		command = err == nil && info.Mode()&os.ModeSymlink != 0
	}
	return args, command
}

// OpenPath is the path an app process is asked to open by its arguments:
// after OpenArg, as `see` starts it, or alone, as a file manager or a
// shell does. A relative path is taken from wd, the current directory
// when wd is "".
func OpenPath(args []string, wd string) (string, bool) {
	var path string
	switch {
	case len(args) > 1 && args[0] == OpenArg:
		path = pathArg(args[1:])
	case len(args) > 1 && args[0] == OpenStdinArg:
		path = pathArg(args[1:])
	case len(args) > 2 && args[0] == OpenDiffArg:
		path = args[1]
	case len(args) > 0 && !strings.HasPrefix(args[0], "-"):
		path = args[0]
	default:
		return "", false
	}
	return resolve(path, wd)
}

// pathArg is the path among the arguments of an OpenArg or OpenStdinArg
// handoff: the one that is not one of their flags.
func pathArg(args []string) string {
	for _, a := range args {
		if a != "--new-window" && !strings.HasPrefix(a, "--") {
			return a
		}
	}
	return ""
}

// resolve makes path absolute against wd, the working directory the
// launch happened in; "" keeps the process's own.
func resolve(path, wd string) (string, bool) {
	if path == "" {
		return "", false
	}
	if wd != "" && !filepath.IsAbs(path) {
		path = filepath.Join(wd, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	return abs, true
}

// OpenRequest reads what the app process is asked to do by the arguments
// the command line started it with: the diff OpenDiffArg names, the
// standard input OpenStdinArg kept in a file, or the path OpenArg names.
// Flags the invocation knew follow the argument and fill the Request.
func OpenRequest(args []string, wd string) (Request, bool) {
	var req Request
	var rest []string
	switch {
	case len(args) > 1 && args[0] == OpenDiffArg:
		rest = args[1:]
		var diff []string
		for _, arg := range rest {
			switch {
			case arg == "--new-window":
				req.NewWindow = true
			case !strings.HasPrefix(arg, "-"):
				diff = append(diff, arg)
			}
		}
		if len(diff) != 2 {
			return req, false
		}
		for i := range diff {
			p, ok := resolve(diff[i], wd)
			if !ok {
				return req, false
			}
			req.Diff[i] = p
		}
	case len(args) > 1 && (args[0] == OpenArg || args[0] == OpenStdinArg):
		stdin := args[0] == OpenStdinArg
		rest = args[1:]
		for _, arg := range rest {
			switch {
			case arg == "--new-window":
				req.NewWindow = true
			case !strings.HasPrefix(arg, "-"):
				req.Path = arg
			}
		}
		path, ok := resolve(req.Path, wd)
		if !ok {
			return req, false
		}
		req.Path, req.Stdin = path, stdin
	default:
		return req, false
	}
	return req, true
}

// Split is the folder to open for path, and the file in it to show, ""
// when path is a folder itself or is not there.
func Split(path string) (dir, file string) {
	if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
		return filepath.Dir(path), path
	}
	return path, ""
}
