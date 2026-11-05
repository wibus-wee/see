package languages

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui/editor"
	"see/internal/langserver"
	"see/internal/lsp"
	"see/internal/shellpath"
	"see/internal/uri"
	"see/internal/workspace"
)

var ErrNoClangd = errors.New("clangd is not installed")

func ClangLanguage(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".c":
		return "c"
	case ".h", ".cc", ".cpp", ".cxx", ".c++", ".hh", ".hpp", ".hxx", ".h++", ".ipp", ".tpp":
		return "cpp"
	case ".m":
		return "objective-c"
	case ".mm":
		return "objective-cpp"
	}
	return ""
}

func FindClangd(root string) (string, error) {
	p := os.Getenv("SEE_CLANGD")
	if p == "" {
		p, _ = exec.LookPath("clangd")
	}
	if !filepath.IsAbs(p) {
		return "", ErrNoClangd
	}
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", ErrNoClangd
	}
	realRoot := root
	if r, err := filepath.EvalSymlinks(root); err == nil {
		realRoot = r
	}
	if root != "" && (workspace.Within(root, p) || workspace.Within(realRoot, resolved)) {
		return "", ErrNoClangd
	}
	if fi, err := os.Stat(resolved); err != nil || !fi.Mode().IsRegular() {
		return "", ErrNoClangd
	}
	return p, nil
}

func StartClangd(logf func(string, ...any)) langserver.Starter {
	return func(ctx context.Context, root string, onExit func(error)) (langserver.Client, error) {
		if err := shellpath.Wait(ctx); err != nil {
			return nil, err
		}
		p, err := FindClangd(root)
		if err != nil {
			return nil, err
		}
		return lsp.Start(ctx, p, nil, lsp.Options{RootPath: root, RootURI: uri.FromPath(root), Name: filepath.Base(root), Logf: logf, OnExit: onExit})
	}
}

func NewClangdServer(root string, onState func(langserver.State, error), onDiagnostics func(*langserver.Server, string, editor.Snapshot, []lsp.Diagnostic, string), logf func(string, ...any)) *langserver.Server {
	return langserver.New(langserver.Options{Name: "clangd", Root: root, Start: StartClangd(logf), Language: ClangLanguage, OnState: onState, OnDiagnostics: onDiagnostics, Logf: logf, Recover: func(err error) bool {
		if !errors.Is(err, ErrNoClangd) {
			return false
		}
		_, err = FindClangd(root)
		return err == nil
	}})
}

var ErrNoCMakeServer = errors.New("cmake-language-server is not installed")

func CMakeLanguage(path string) string {
	if filepath.Base(path) == "CMakeLists.txt" || strings.EqualFold(filepath.Ext(path), ".cmake") {
		return "cmake"
	}
	return ""
}

func FindCMakeServer(root string) (string, error) {
	p := os.Getenv("SEE_CMAKE_LANGUAGE_SERVER")
	if p == "" {
		p, _ = exec.LookPath("cmake-language-server")
	}
	if !filepath.IsAbs(p) {
		return "", ErrNoCMakeServer
	}
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", ErrNoCMakeServer
	}
	realRoot := root
	if r, err := filepath.EvalSymlinks(root); err == nil {
		realRoot = r
	}
	if root != "" && (workspace.Within(root, p) || workspace.Within(realRoot, resolved)) {
		return "", ErrNoCMakeServer
	}
	if fi, err := os.Stat(resolved); err != nil || !fi.Mode().IsRegular() {
		return "", ErrNoCMakeServer
	}
	return p, nil
}

func NewCMakeServer(root string, onState func(langserver.State, error), onDiagnostics func(*langserver.Server, string, editor.Snapshot, []lsp.Diagnostic, string), logf func(string, ...any)) *langserver.Server {
	start := func(ctx context.Context, root string, onExit func(error)) (langserver.Client, error) {
		if err := shellpath.Wait(ctx); err != nil {
			return nil, err
		}
		p, err := FindCMakeServer(root)
		if err != nil {
			return nil, err
		}
		return lsp.Start(ctx, p, nil, lsp.Options{RootPath: root, RootURI: uri.FromPath(root), Name: filepath.Base(root), Logf: logf, OnExit: onExit})
	}
	return langserver.New(langserver.Options{Name: "cmake-language-server", Root: root, Start: start, Language: CMakeLanguage, OnState: onState, OnDiagnostics: onDiagnostics, Logf: logf, Recover: func(err error) bool {
		if !errors.Is(err, ErrNoCMakeServer) {
			return false
		}
		_, err = FindCMakeServer(root)
		return err == nil
	}})
}
