package languages

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"see/internal/langserver"
	"see/internal/lsp"
	"see/internal/proc"
	"see/internal/shellpath"
	"see/internal/uri"
)

// ErrNoGopls is returned when no gopls is installed.
var ErrNoGopls = errors.New("gopls is not installed")

// validateCandidate applies the launch boundary's trust check to a
// candidate a finder is considering. A candidate the boundary denies
// because it lives in the workspace is reported with denied set; any
// other failure — missing, not a regular file — is only unusable.
func validateCandidate(root, p string) (err error, denied bool) {
	if err := proc.ValidateExecutable(root, p); err != nil {
		return err, errors.Is(err, proc.ErrUntrustedExecutable)
	}
	if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
		if err == nil {
			err = errors.New("not a regular file")
		}
		return err, false
	}
	return nil, false
}

// FindGopls finds gopls: $SEE_GOPLS, then the copies an installation added
// (in the order given), then PATH, then Go's bin directories. A gopls
// inside the workspace is never used; when a copy the user pointed at —
// an environment variable or an installation — is denied for that, the
// failure says so rather than claiming nothing is installed.
func FindGopls(root string, installed ...string) (string, error) {
	type candidate struct {
		path     string
		explicit bool
	}
	var candidates []candidate
	if p := os.Getenv("SEE_GOPLS"); p != "" {
		candidates = append(candidates, candidate{p, true})
	}
	for _, p := range installed {
		candidates = append(candidates, candidate{p, true})
	}
	if p, err := exec.LookPath("gopls"); err == nil {
		candidates = append(candidates, candidate{p, false})
	}
	exe := "gopls"
	if runtime.GOOS == "windows" {
		exe = "gopls.exe"
	}
	if p := os.Getenv("GOBIN"); p != "" {
		candidates = append(candidates, candidate{filepath.Join(p, exe), false})
	}
	if p := os.Getenv("GOPATH"); p != "" {
		for _, dir := range filepath.SplitList(p) {
			candidates = append(candidates, candidate{filepath.Join(dir, "bin", exe), false})
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, candidate{filepath.Join(home, "go", "bin", exe), false})
	}
	var denial error
	for _, c := range candidates {
		if !filepath.IsAbs(c.path) {
			continue
		}
		err, denied := validateCandidate(root, c.path)
		if err == nil {
			return c.path, nil
		}
		if denied && c.explicit && denial == nil {
			denial = err
		}
	}
	if denial != nil {
		return "", denial
	}
	return "", ErrNoGopls
}

// StartGopls is the langserver.Starter of the real gopls.
func StartGopls(logf func(string, ...any)) langserver.Starter {
	return func(ctx context.Context, root string, folders []lsp.WorkspaceFolder, onExit func(error)) (langserver.Client, error) {
		if err := shellpath.Wait(ctx); err != nil {
			return nil, err
		}
		path, err := FindGopls(root)
		if err != nil {
			return nil, err
		}
		c, err := lsp.Start(ctx, path, nil, lsp.Options{
			RootPath:         root,
			RootURI:          uri.FromPath(root),
			WorkspaceFolders: folders,
			ServerID:         "gopls",
			Name:             filepath.Base(root),
			Logf:             logf,
			OnExit:           onExit,
		})
		if err != nil {
			return nil, err
		}
		return c, nil
	}
}

var ErrNoTypeScriptServer = errors.New("tsrs is not installed")

// NewTypeScriptServer returns the manager of tsrs for root. When tsrs was
// missing, it starts once tsrs is found, as another window or process may
// have installed it since.
func NewTypeScriptServer(root string, folders []lsp.WorkspaceFolder, start langserver.Starter, onState func(langserver.State, error),
	onDiagnostics langserver.DiagnosticsHandler, logf func(string, ...any)) *langserver.Server {
	return langserver.New(langserver.Options{
		Name:          "tsrs",
		Root:          root,
		Folders:       folders,
		Start:         start,
		Language:      TypeScriptLanguage,
		OnState:       onState,
		OnDiagnostics: onDiagnostics,
		Logf:          logf,
		Recover: func(err error) bool {
			if !errors.Is(err, ErrNoTypeScriptServer) {
				return false
			}
			_, findErr := FindTypeScript(root)
			return findErr == nil
		},
	})
}

func TypeScriptLanguage(path string) string {
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

// FindTypeScript finds tsrs: $SEE_TYPESCRIPT_LANGUAGE_SERVER, then See's
// own installation, then PATH. A tsrs inside the workspace is never used;
// a copy the user pointed at being denied for that is a denial, not a
// missing installation.
func FindTypeScript(root string) (string, error) {
	p := os.Getenv("SEE_TYPESCRIPT_LANGUAGE_SERVER")
	explicit := p != ""
	if p == "" && ManagedTypeScriptInstalled() {
		p = ManagedTypeScriptEntry()
		explicit = true
	}
	if p == "" {
		p, _ = exec.LookPath("tsrs")
	}
	if !filepath.IsAbs(p) {
		return "", ErrNoTypeScriptServer
	}
	if err, denied := validateCandidate(root, p); err != nil {
		if denied && explicit {
			return "", err
		}
		return "", ErrNoTypeScriptServer
	}
	return p, nil
}

func StartTypeScript(logf func(string, ...any)) langserver.Starter {
	return func(ctx context.Context, root string, folders []lsp.WorkspaceFolder, onExit func(error)) (langserver.Client, error) {
		if err := shellpath.Wait(ctx); err != nil {
			return nil, err
		}
		path, err := FindTypeScript(root)
		if err != nil {
			return nil, err
		}
		args := []string{"--lsp", "-stdio"}
		if path == ManagedTypeScriptEntry() {
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
		return lsp.Start(ctx, path, args, lsp.Options{RootPath: root, RootURI: uri.FromPath(root), WorkspaceFolders: folders, Name: filepath.Base(root), Logf: logf, OnExit: onExit})
	}
}
