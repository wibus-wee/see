package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"see/internal/cli"
)

func installSeeCommand(exe, dir string) (path string, already bool, err error) {
	if runtime.GOOS != "windows" {
		return installSeeLink(exe, dir)
	}
	path, already, err = installSeeBatch(exe, dir)
	if err == nil {
		// Bash uses the extensionless link when Windows permits symlinks.
		_, _, _ = installSeeLink(exe, dir)
	}
	return
}

func installSeeBatch(exe, dir string) (path string, already bool, err error) {
	path = filepath.Join(dir, "see.cmd")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return path, false, err
	}
	script := seeBatch(exe)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if os.IsExist(err) {
		if data, err := os.ReadFile(path); err == nil && string(data) == script {
			return path, true, nil
		}
	}
	if err != nil {
		return path, false, err
	}
	_, err = f.WriteString(script)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
	}
	return path, false, err
}

func uninstallSeeCommand(exe, dir string) (path string, removed bool, err error) {
	if runtime.GOOS != "windows" {
		return uninstallSeeLink(exe, dir)
	}
	path = filepath.Join(dir, "see.cmd")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return uninstallSeeLink(exe, dir)
	}
	if err != nil {
		return path, false, err
	}
	if string(data) != seeBatch(exe) {
		return path, false, fmt.Errorf("command does not launch the running application")
	}
	link := filepath.Join(dir, "see")
	if target, err := os.Readlink(link); err == nil && target == exe {
		if err := os.Remove(link); err != nil {
			return link, false, err
		}
	}
	if err := os.Remove(path); err != nil {
		return path, false, err
	}
	return path, true, nil
}

func seeBatch(exe string) string {
	return "@echo off\r\nsetlocal DisableDelayedExpansion\r\n\"" + strings.ReplaceAll(exe, "%", "%%") + "\" " + cli.RunArg + " %*\r\n"
}
