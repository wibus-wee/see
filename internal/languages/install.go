package languages

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"see/internal/shellpath"
)

// ManagedTypeScriptDir contains only See's TypeScript server installation.
func ManagedTypeScriptDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "See", "language-servers", "tsrs")
}

func ManagedTypeScriptEntry() string {
	dir := ManagedTypeScriptDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "node_modules", "tsrs", "bin", "tsrs")
}

func ManagedTypeScriptExists() bool {
	dir := ManagedTypeScriptDir()
	if dir == "" {
		return false
	}
	_, err := os.Lstat(dir)
	return err == nil
}

func ManagedTypeScriptInstalled() bool {
	p := ManagedTypeScriptEntry()
	if p == "" {
		return false
	}
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// InstallTypeScript stages the packages before exposing an installation.
func InstallTypeScript(ctx context.Context, dest string) error {
	if err := shellpath.Wait(ctx); err != nil {
		return err
	}
	if dest == "" {
		return errors.New("cannot locate the application data folder")
	}
	if _, err := os.Lstat(dest); err == nil {
		return errors.New("a server installation already exists; remove it first")
	} else if !os.IsNotExist(err) {
		return err
	}
	bun, bunErr := exec.LookPath("bun")
	npm, npmErr := exec.LookPath("npm")
	if bunErr != nil && npmErr != nil {
		return errors.New("install Bun or Node.js with npm first")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(dest), ".tsrs-install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := os.WriteFile(filepath.Join(stage, "package.json"), []byte("{\"private\":true}\n"), 0600); err != nil {
		return err
	}
	var cmd *exec.Cmd
	if bunErr == nil {
		cmd = exec.CommandContext(ctx, bun, "add", "--no-cache", "tsrs@0.20.1-ts7.1.0-beta")
	} else {
		cmd = exec.CommandContext(ctx, npm, "install", "--no-audit", "--no-fund", "--ignore-scripts", "tsrs@0.20.1-ts7.1.0-beta")
		cmd.Env = append(os.Environ(), "npm_config_cache="+filepath.Join(stage, ".npm-cache"))
	}
	cmd.Dir = stage
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("installation failed: %s", shortOutput(string(output)))
	}
	if _, err := os.Stat(filepath.Join(stage, "node_modules", "tsrs", "bin", "tsrs")); err != nil {
		return errors.New("installation did not contain the language server")
	}

	return os.Rename(stage, dest)
}

// shortOutput is the last of a command's output, as an error says it.
func shortOutput(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 80 {
		s = s[:77] + "…"
	}
	return s
}
