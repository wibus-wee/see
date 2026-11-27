package app

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/i18n"

	"see/internal/config"
	"see/internal/lsp"
	"see/internal/uri"
	"see/internal/workspace"
	"see/locales"
)

func lspWorkspaceFolders(folders []workspace.Folder) []lsp.WorkspaceFolder {
	out := make([]lsp.WorkspaceFolder, len(folders))
	for i, folder := range folders {
		out[i] = lsp.WorkspaceFolder{URI: uri.FromPath(folder.Path), Name: folder.Name}
	}
	return out
}

func (a *App) workspaceFolderPaths() []string {
	paths := make([]string, len(a.workspaceFolders))
	for i, folder := range a.workspaceFolders {
		paths[i] = folder.Path
	}
	return paths
}

func (a *App) workspaceTrusted() bool {
	if len(a.workspaceFolders) == 0 {
		return a.root != "" && a.cfg.IsTrusted(a.root)
	}
	for _, folder := range a.workspaceFolders {
		if !a.cfg.IsTrusted(folder.Path) {
			return false
		}
	}
	return true
}

func (a *App) trustBannerLead() string {
	if len(a.workspaceFolders) > 1 {
		return locales.Catalog.Format(i18n.Locale(), "trustWorkspaceLead", nil)
	}
	return locales.TrustBannerLead(filepath.Base(a.root))
}

func (a *App) trustButtonLabel() string {
	if len(a.workspaceFolders) > 1 {
		return locales.Catalog.Format(i18n.Locale(), "trustWorkspace", nil)
	}
	return locales.TrustFolder()
}

func (a *App) trustProblemDetail() string {
	if len(a.workspaceFolders) > 1 {
		return locales.Catalog.Format(i18n.Locale(), "problemsTrustWorkspace", nil)
	}
	return locales.Catalog.Format(i18n.Locale(), "problemsTrust", nil)
}

func (a *App) trustSearchButton() string {
	if len(a.workspaceFolders) > 1 {
		return locales.Catalog.Format(i18n.Locale(), "trustWorkspaceSearchSymbols", nil)
	}
	return locales.MsgTrustFolderToSearchSymbols()
}

func workspaceUpdateFailed(path string, err error) string {
	return locales.Catalog.Format(i18n.Locale(), "workspaceUpdateFailed", map[string]any{
		"path": path, "reason": err.Error(),
	})
}

func (a *App) gitRepositorySelectionEnabled() bool {
	return len(a.workspaceFolders) > 1 || a.cfg.GitAutoDiscover
}

func (a *App) isWorkspaceFolder(path string) bool {
	for _, folder := range a.workspaceFolders {
		if path == folder.Path {
			return true
		}
	}
	return false
}

func (a *App) workspaceRootForPath(path string) string {
	var root string
	for _, folder := range a.workspaceFolders {
		if workspace.Within(folder.Path, path) && len(folder.Path) > len(root) {
			root = folder.Path
		}
	}
	if root == "" && len(a.workspaceFolders) == 0 && a.root != "" && workspace.Within(a.root, path) {
		return a.root
	}
	return root
}

func (a *App) withinWorkspace(path string) bool {
	return a.workspaceRootForPath(path) != ""
}

func (a *App) workspacePath(path string) string {
	root := a.workspaceRootForPath(path)
	if root == "" {
		return path
	}
	rel := workspace.RelPath(root, path)
	if len(a.workspaceFolders) <= 1 {
		return rel
	}
	name := a.workspaceFolderLabel(root)
	if rel == "." {
		return name
	}
	return filepath.ToSlash(filepath.Join(name, filepath.FromSlash(rel)))
}

func (a *App) workspaceFilePath(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(a.root, filepath.FromSlash(path))
}

func (a *App) workspaceFolderLabel(root string) string {
	name := filepath.Base(root)
	for _, folder := range a.workspaceFolders {
		if folder.Path == root && folder.Name != "" {
			name = folder.Name
			break
		}
	}
	duplicate := false
	for _, folder := range a.workspaceFolders {
		if folder.Path != root && a.workspaceFolderName(folder.Path) == name {
			duplicate = true
			break
		}
	}
	if duplicate {
		label := name + " · " + filepath.Base(filepath.Dir(root))
		for _, folder := range a.workspaceFolders {
			if folder.Path != root && a.workspaceFolderName(folder.Path)+" · "+filepath.Base(filepath.Dir(folder.Path)) == label {
				return filepath.Clean(root)
			}
		}
		return label
	}
	return name
}

func (a *App) workspaceFolderName(root string) string {
	for _, folder := range a.workspaceFolders {
		if folder.Path == root && folder.Name != "" {
			return folder.Name
		}
	}
	return filepath.Base(root)
}

func workspaceAddFolderLabel() string {
	return locales.Catalog.Format(i18n.Locale(), "workspaceAddFolder", nil)
}

func workspaceRemoveFolderLabel() string {
	return locales.Catalog.Format(i18n.Locale(), "workspaceRemoveFolder", nil)
}

func (a *App) docPath(path string) string {
	root := a.workspaceRootForPath(path)
	if root == "" {
		return workspace.DocPath(a.root, a.realRoot, path)
	}
	realRoot := root
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		realRoot = resolved
	}
	return workspace.DocPath(root, realRoot, path)
}

func (a *App) codeWorkspace() bool {
	return strings.EqualFold(filepath.Ext(a.workspaceID), ".code-workspace")
}

func (a *App) workspaceTitle() string {
	if a.codeWorkspace() {
		return strings.TrimSuffix(filepath.Base(a.workspaceID), filepath.Ext(a.workspaceID))
	}
	if len(a.workspaceFolders) > 1 {
		return filepath.Base(a.workspaceID)
	}
	if len(a.workspaceFolders) == 1 {
		return a.workspaceFolders[0].Name
	}
	return filepath.Base(a.root)
}

func savedWorkspaceFolders(folders []workspace.Folder) []config.WorkspaceFolder {
	out := make([]config.WorkspaceFolder, len(folders))
	for i, folder := range folders {
		out[i] = config.WorkspaceFolder{Path: folder.Path, Name: folder.Name}
	}
	return out
}

func loadSavedWorkspaceFolders(folders []config.WorkspaceFolder) ([]workspace.Folder, error) {
	out := make([]workspace.Folder, 0, len(folders))
	seen := map[string]bool{}
	for _, folder := range folders {
		root, err := workspace.CanonicalRoot(folder.Path)
		if err != nil {
			return nil, err
		}
		if seen[root] {
			continue
		}
		seen[root] = true
		name := strings.TrimSpace(folder.Name)
		if name == "" {
			name = filepath.Base(root)
		}
		out = append(out, workspace.Folder{Path: root, Name: name})
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func normalizedWorkspaceSettings(settings workspace.Settings) workspace.Settings {
	enabled := func(in map[string]bool) map[string]bool {
		if len(in) == 0 {
			return nil
		}
		out := map[string]bool{}
		for pattern, on := range in {
			if on {
				out[pattern] = true
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	return workspace.Settings{FilesExclude: enabled(settings.FilesExclude), SearchExclude: enabled(settings.SearchExclude)}
}

func sameExcludePatterns(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for pattern := range a {
		if !b[pattern] {
			return false
		}
	}
	return true
}

func (a *App) applyWorkspaceSettings(settings workspace.Settings) {
	settings = normalizedWorkspaceSettings(settings)
	if sameExcludePatterns(a.workspaceSettings.FilesExclude, settings.FilesExclude) &&
		sameExcludePatterns(a.workspaceSettings.SearchExclude, settings.SearchExclude) {
		return
	}
	a.workspaceSettings = settings
	for dir := range a.dirs {
		a.refreshDir(dir)
	}
	a.invalidateQuickFiles()
	a.filter.forQuery, a.filter.tree, a.filter.roots = "", nil, nil
	a.runFind()
}

func mergedSearchExcludes(settings workspace.Settings) map[string]bool {
	if len(settings.FilesExclude) == 0 && len(settings.SearchExclude) == 0 {
		return nil
	}
	out := map[string]bool{}
	for pattern := range settings.FilesExclude {
		out[pattern] = true
	}
	for pattern := range settings.SearchExclude {
		out[pattern] = true
	}
	return out
}

func ripgrepExcludeGlob(pattern string) string {
	pattern = strings.TrimSuffix(filepath.ToSlash(pattern), "/")
	if !strings.Contains(pattern, "/") {
		return "/" + pattern
	}
	return pattern
}

func ripgrepExcludeGlobs(patterns map[string]bool) []string {
	out := make([]string, 0, len(patterns))
	for pattern := range patterns {
		out = append(out, ripgrepExcludeGlob(pattern))
	}
	slices.Sort(out)
	return out
}
