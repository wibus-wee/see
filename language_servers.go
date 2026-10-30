package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/langserver"
	"see/internal/lsp"
	"see/internal/shellpath"
	"see/internal/workspace"
)

// errNoGopls is returned when no gopls is installed.
var errNoGopls = errors.New("gopls is not installed")

// findGopls finds gopls: $SEE_GOPLS, then PATH, then Go's bin
// directories. A gopls inside the workspace is never used.
func findGopls(root string) (string, error) {
	var candidates []string
	if p := os.Getenv("SEE_GOPLS"); p != "" {
		candidates = append(candidates, p)
	}
	if p, err := exec.LookPath("gopls"); err == nil {
		candidates = append(candidates, p)
	}
	exe := "gopls"
	if runtime.GOOS == "windows" {
		exe = "gopls.exe"
	}
	if p := os.Getenv("GOBIN"); p != "" {
		candidates = append(candidates, filepath.Join(p, exe))
	}
	if p := os.Getenv("GOPATH"); p != "" {
		for _, dir := range filepath.SplitList(p) {
			candidates = append(candidates, filepath.Join(dir, "bin", exe))
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, "go", "bin", exe))
	}
	for _, p := range candidates {
		if !filepath.IsAbs(p) {
			continue
		}
		if root != "" && workspace.Within(root, p) {
			continue
		}
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return p, nil
		}
	}
	return "", errNoGopls
}

// startGopls is the langserver.Starter of the real gopls.
func startGopls(logf func(string, ...any)) langserver.Starter {
	return func(ctx context.Context, root string, onExit func(error)) (langserver.Client, error) {
		if err := shellpath.Wait(ctx); err != nil {
			return nil, err
		}
		path, err := findGopls(root)
		if err != nil {
			return nil, err
		}
		c, err := lsp.Start(ctx, path, nil, lsp.Options{
			RootPath: root,
			RootURI:  uriOf(root),
			Name:     filepath.Base(root),
			Logf:     logf,
			OnExit:   onExit,
		})
		if err != nil {
			return nil, err
		}
		return c, nil
	}
}

var errNoTypeScriptServer = errors.New("tsrs is not installed")

// newTypeScriptServer returns the manager of tsrs for root. When tsrs was
// missing, it starts once tsrs is found, as another window or process may
// have installed it since.
func newTypeScriptServer(root string, start langserver.Starter, onState func(langserver.State, error),
	onDiagnostics func(*langserver.Server, string, editor.Snapshot, []lsp.Diagnostic, string), logf func(string, ...any)) *langserver.Server {
	return langserver.New(langserver.Options{
		Name:          "tsrs",
		Root:          root,
		Start:         start,
		Language:      typeScriptLanguage,
		OnState:       onState,
		OnDiagnostics: onDiagnostics,
		Logf:          logf,
		Recover: func(err error) bool {
			if !errors.Is(err, errNoTypeScriptServer) {
				return false
			}
			_, findErr := findTypeScriptServer(root)
			return findErr == nil
		},
	})
}

func typeScriptLanguage(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ts":
		return "typescript"
	case ".tsx":
		return "typescriptreact"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	case ".jsx":
		return "javascriptreact"
	}
	return ""
}

func findTypeScriptServer(root string) (string, error) {
	p := os.Getenv("SEE_TYPESCRIPT_LANGUAGE_SERVER")
	if p == "" && managedTypeScriptInstalled() {
		p = managedTypeScriptEntry()
	}
	if p == "" {
		p, _ = exec.LookPath("tsrs")
	}
	if !filepath.IsAbs(p) {
		return "", errNoTypeScriptServer
	}
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", errNoTypeScriptServer
	}
	realRoot := root
	if r, err := filepath.EvalSymlinks(root); err == nil {
		realRoot = r
	}
	if root != "" && (workspace.Within(root, p) || workspace.Within(realRoot, resolved)) {
		return "", errNoTypeScriptServer
	}
	if fi, err := os.Stat(resolved); err != nil || !fi.Mode().IsRegular() {
		return "", errNoTypeScriptServer
	}
	return p, nil
}

func startTypeScriptServer(logf func(string, ...any)) langserver.Starter {
	return func(ctx context.Context, root string, onExit func(error)) (langserver.Client, error) {
		if err := shellpath.Wait(ctx); err != nil {
			return nil, err
		}
		path, err := findTypeScriptServer(root)
		if err != nil {
			return nil, err
		}
		args := []string{"--lsp", "-stdio"}
		if path == managedTypeScriptEntry() {
			runtimePath, err := exec.LookPath("node")
			if err != nil {
				runtimePath, err = exec.LookPath("bun")
			}
			if err != nil {
				return nil, errors.New("install Node.js or Bun to run tsrs")
			}
			args = append([]string{path}, args...)
			path = runtimePath
		}
		return lsp.Start(ctx, path, args, lsp.Options{RootPath: root, RootURI: uriOf(root), Name: filepath.Base(root), Logf: logf, OnExit: onExit})
	}
}
