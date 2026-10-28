package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// shellPathReady is set before any windows or workers start. A nil channel
// means there is no shell lookup (including in tests and on other platforms).
var shellPathReady <-chan struct{}

func startShellPath() {
	if runtime.GOOS != "darwin" {
		return
	}
	done := make(chan struct{})
	shellPathReady = done
	shell, inherited := os.Getenv("SHELL"), os.Getenv("PATH")
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if path, err := loginShellPath(ctx, shell, inherited); err == nil {
			_ = os.Setenv("PATH", path)
		}
	}()
}

func waitShellPath(ctx context.Context) error {
	if shellPathReady == nil {
		return nil
	}
	select {
	case <-shellPathReady:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Read PATH between NULs so shell startup banners cannot become path entries.
func loginShellPath(ctx context.Context, shell, inherited string) (string, error) {
	if !filepath.IsAbs(shell) {
		shell = "/bin/zsh"
	}
	cmd := exec.CommandContext(ctx, shell, "-ilc", `printf '\0%s\0' "$PATH"`)
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	start := bytes.IndexByte(out, 0)
	if start < 0 {
		return "", os.ErrInvalid
	}
	end := bytes.IndexByte(out[start+1:], 0)
	if end < 0 {
		return "", os.ErrInvalid
	}
	value := string(out[start+1 : start+1+end])
	if value == "" {
		return "", os.ErrInvalid
	}
	seen := make(map[string]bool)
	var dirs []string
	for _, dir := range append(filepath.SplitList(value), filepath.SplitList(inherited)...) {
		if !filepath.IsAbs(dir) || seen[dir] {
			continue
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	if len(dirs) == 0 {
		return "", os.ErrInvalid
	}
	return strings.Join(dirs, string(os.PathListSeparator)), nil
}
