// Package shellpath gives a macOS app started from Finder or the Dock the
// PATH of the user's login shell, as a terminal would have it: such apps
// otherwise get launchd's short default and miss tools such as gopls, git
// or bun installed by the user.
package shellpath

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

// ready is set before any windows or workers start. A nil channel means
// there is no shell lookup (including in tests and on other platforms).
var ready <-chan struct{}

// Start reads the login shell's PATH in the background and sets it on the
// process. It does nothing outside macOS.
func Start() {
	if runtime.GOOS != "darwin" {
		return
	}
	done := make(chan struct{})
	ready = done
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

// Wait waits until PATH is set, if a lookup started.
func Wait(ctx context.Context) error {
	if ready == nil {
		return nil
	}
	select {
	case <-ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Ready is closed once PATH is set; it is nil when no lookup started.
func Ready() <-chan struct{} { return ready }

// Override makes Wait and Ready use r until restore is called, for tests.
func Override(r <-chan struct{}) (restore func()) {
	previous := ready
	ready = r
	return func() { ready = previous }
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
