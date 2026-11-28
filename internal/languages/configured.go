package languages

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"see/internal/config"
	"see/internal/langserver"
	"see/internal/lsp"
	"see/internal/shellpath"
	"see/internal/uri"
)

// configuredExtensions maps the spec's extensions, lowercased and dotted,
// to their position in Extensions.
func configuredExtensions(spec config.LSPConfig) map[string]int {
	exts := map[string]int{}
	for i, e := range spec.Extensions {
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		exts[strings.ToLower(e)] = i
	}
	return exts
}

// ConfiguredLanguage is the langserver.Options.Language of a configured
// server: the language ID it reports for path, or "" for a file it does
// not cover.
func ConfiguredLanguage(spec config.LSPConfig) func(path string) string {
	exts := configuredExtensions(spec)
	return func(path string) string {
		i, ok := exts[strings.ToLower(filepath.Ext(path))]
		if !ok {
			return ""
		}
		// The extension selects which of the configured languages to
		// report: [".ts", ".js"] with ["typescript", "javascript"] names
		// each file's own. Fewer languages than extensions all get the
		// first, as ["rust"] alone describes every [".rs"] file.
		if len(spec.Languages) > 0 {
			return spec.Languages[min(i, len(spec.Languages)-1)]
		}
		return ""
	}
}

// ConfiguredCovers reports whether the configured server handles path:
// an extension it lists matches, or — when it lists languages — the
// file's known language is one.
func ConfiguredCovers(spec config.LSPConfig) func(path string) bool {
	exts := configuredExtensions(spec)
	langs := map[string]bool{}
	for _, l := range spec.Languages {
		langs[l] = true
	}
	return func(path string) bool {
		if _, ok := exts[strings.ToLower(filepath.Ext(path))]; ok {
			return true
		}
		// A server configured by language alone, languages = ["rust"],
		// still claims a file whose known language it names.
		return langs[fileLanguage(path)]
	}
}

// fileLanguage is what the built-in servers call path's language, so a
// server configured by language matches the same files.
func fileLanguage(path string) string {
	if strings.EqualFold(filepath.Ext(path), ".go") {
		return "go"
	}
	if l := TypeScriptLanguage(path); l != "" {
		return l
	}
	if l := CMakeLanguage(path); l != "" {
		return l
	}
	return ClangLanguage(path)
}

// FindConfigured resolves the server's executable as the built-in finders
// do: on PATH, never inside the workspace. The command is one the user
// configured, so the trust boundary denying it is reported as the denial
// it is — the software is installed; it is where it is that cannot run.
func FindConfigured(root string, spec config.LSPConfig) (string, error) {
	if len(spec.Command) == 0 {
		return "", fmt.Errorf("%s is not configured with a command", spec.Name)
	}
	p := spec.Command[0]
	if !filepath.IsAbs(p) {
		found, err := exec.LookPath(p)
		if err != nil {
			return "", fmt.Errorf("%s is not installed", spec.Name)
		}
		p = found
	}
	if err, denied := validateCandidate(root, p); err != nil {
		if denied {
			return "", err
		}
		return "", fmt.Errorf("%s is not installed", spec.Name)
	}
	return p, nil
}

// StartConfigured is the langserver.Starter of a configured server: it
// runs the configured command with its configured environment, as the
// built-in starters run theirs.
func StartConfigured(spec config.LSPConfig, logf func(string, ...any)) langserver.Starter {
	return func(ctx context.Context, root string, folders []lsp.WorkspaceFolder, onExit func(error)) (langserver.Client, error) {
		if err := shellpath.Wait(ctx); err != nil {
			return nil, err
		}
		p, err := FindConfigured(root, spec)
		if err != nil {
			return nil, err
		}
		return lsp.Start(ctx, p, spec.Command[1:], lsp.Options{
			RootPath:              root,
			RootURI:               uri.FromPath(root),
			WorkspaceFolders:      folders,
			ServerID:              spec.Name,
			Name:                  filepath.Base(root),
			InitializationOptions: spec.InitializationOptions,
			Env:                   envList(spec.Env),
			Logf:                  logf,
			OnExit:                onExit,
		})
	}
}

func envList(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out
}

// NewConfiguredServer returns the manager of one configured server for
// root, with the same lifecycle and recovery the built-in ones have.
func NewConfiguredServer(spec config.LSPConfig, root string, folders []lsp.WorkspaceFolder, onState func(langserver.State, error),
	onDiagnostics langserver.DiagnosticsHandler, logf func(string, ...any)) *langserver.Server {
	notInstalled := func(err error) bool {
		return err != nil && strings.HasSuffix(err.Error(), "is not installed")
	}
	return langserver.New(langserver.Options{
		Name: spec.Name, Root: root, Folders: folders, Start: StartConfigured(spec, logf), Language: ConfiguredLanguage(spec),
		OnState: onState, OnDiagnostics: onDiagnostics, Logf: logf,
		Recover: func(err error) bool {
			if !notInstalled(err) {
				return false
			}
			_, err = FindConfigured(root, spec)
			return err == nil
		},
	})
}

// ConfiguredSpecs drops configured servers that cannot apply: nameless,
// commandless, or covering no file.
func ConfiguredSpecs(specs []config.LSPConfig) []config.LSPConfig {
	out := specs[:0:0]
	for _, s := range specs {
		if s.Name == "" || len(s.Command) == 0 {
			continue
		}
		if len(s.Extensions) == 0 && len(s.Languages) == 0 {
			continue
		}
		out = append(out, s)
	}
	return out
}
