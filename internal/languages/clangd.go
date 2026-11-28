package languages

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"see/internal/langserver"
	"see/internal/lsp"
	"see/internal/shellpath"
	"see/internal/uri"
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
	explicit := p != ""
	if p == "" {
		p, _ = exec.LookPath("clangd")
	}
	if !filepath.IsAbs(p) {
		return "", ErrNoClangd
	}
	if err, denied := validateCandidate(root, p); err != nil {
		if denied && explicit {
			return "", err
		}
		return "", ErrNoClangd
	}
	return p, nil
}

func StartClangd(logf func(string, ...any)) langserver.Starter {
	return func(ctx context.Context, root string, folders []lsp.WorkspaceFolder, onExit func(error)) (langserver.Client, error) {
		if err := shellpath.Wait(ctx); err != nil {
			return nil, err
		}
		p, err := FindClangd(root)
		if err != nil {
			return nil, err
		}
		return lsp.Start(ctx, p, nil, lsp.Options{RootPath: root, RootURI: uri.FromPath(root), WorkspaceFolders: folders, Name: filepath.Base(root), Logf: logf, OnExit: onExit})
	}
}

func NewClangdServer(root string, folders []lsp.WorkspaceFolder, onState func(langserver.State, error), onDiagnostics langserver.DiagnosticsHandler, logf func(string, ...any)) *langserver.Server {
	return langserver.New(langserver.Options{Name: "clangd", Root: root, Folders: folders, Start: StartClangd(logf), Language: ClangLanguage, OnState: onState, OnDiagnostics: onDiagnostics, Logf: logf, Recover: func(err error) bool {
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
	explicit := p != ""
	if p == "" {
		p, _ = exec.LookPath("cmake-language-server")
	}
	if !filepath.IsAbs(p) {
		return "", ErrNoCMakeServer
	}
	if err, denied := validateCandidate(root, p); err != nil {
		if denied && explicit {
			return "", err
		}
		return "", ErrNoCMakeServer
	}
	return p, nil
}

func NewCMakeServer(root string, folders []lsp.WorkspaceFolder, onState func(langserver.State, error), onDiagnostics langserver.DiagnosticsHandler, logf func(string, ...any)) *langserver.Server {
	start := func(ctx context.Context, root string, folders []lsp.WorkspaceFolder, onExit func(error)) (langserver.Client, error) {
		if err := shellpath.Wait(ctx); err != nil {
			return nil, err
		}
		p, err := FindCMakeServer(root)
		if err != nil {
			return nil, err
		}
		return lsp.Start(ctx, p, nil, lsp.Options{RootPath: root, RootURI: uri.FromPath(root), WorkspaceFolders: folders, Name: filepath.Base(root), Logf: logf, OnExit: onExit})
	}
	return langserver.New(langserver.Options{Name: "cmake-language-server", Root: root, Folders: folders, Start: start, Language: CMakeLanguage, OnState: onState, OnDiagnostics: onDiagnostics, Logf: logf, Recover: func(err error) bool {
		if !errors.Is(err, ErrNoCMakeServer) {
			return false
		}
		_, err = FindCMakeServer(root)
		return err == nil
	}})
}
