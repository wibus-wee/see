package app

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/i18n"

	"see/internal/config"
	"see/internal/lsp"
	"see/internal/search"
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

func (a *App) multiRootWorkspacesEnabled() bool {
	return a.cfg.BetaEnabled(config.BetaMultiRootWorkspaces)
}

func (a *App) workspaceFolderPaths() []string {
	paths := make([]string, len(a.workspaceFolders))
	for i, folder := range a.workspaceFolders {
		paths[i] = folder.Path
	}
	return paths
}

func existingWorkspaceRoots(roots []string) []string {
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			out = append(out, root)
		}
	}
	return out
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
	clean := filepath.Clean(path)
	for _, folder := range a.workspaceFolders {
		realRoot := folder.Path
		if resolved, err := filepath.EvalSymlinks(folder.Path); err == nil {
			realRoot = resolved
		}
		if mapped := workspace.DocPath(folder.Path, realRoot, clean); mapped != clean {
			return mapped
		}
	}
	return workspace.DocPath(a.root, a.realRoot, clean)
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
		return maps.Clone(in)
	}
	settings.FilesExclude = enabled(settings.FilesExclude)
	settings.FilesWatcherExclude = enabled(settings.FilesWatcherExclude)
	settings.SearchExclude = enabled(settings.SearchExclude)
	return settings
}

func sameStringMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for pattern, when := range a {
		if b[pattern] != when {
			return false
		}
	}
	return true
}

func sameExcludePatterns(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for pattern, enabled := range a {
		other, ok := b[pattern]
		if !ok || other != enabled {
			return false
		}
	}
	return true
}

func (a *App) applyWorkspaceSettings(settings workspace.Settings) {
	settings = normalizedWorkspaceSettings(settings)
	watcherChanged := !sameExcludePatterns(a.workspaceSettings.FilesWatcherExclude, settings.FilesWatcherExclude) || !sameStringMap(a.workspaceSettings.FilesWatcherExcludeWhen, settings.FilesWatcherExcludeWhen)
	if sameExcludePatterns(a.workspaceSettings.FilesExclude, settings.FilesExclude) &&
		sameStringMap(a.workspaceSettings.FilesExcludeWhen, settings.FilesExcludeWhen) &&
		!watcherChanged &&
		sameStringMap(a.workspaceSettings.FilesWatcherExcludeWhen, settings.FilesWatcherExcludeWhen) &&
		sameExcludePatterns(a.workspaceSettings.SearchExclude, settings.SearchExclude) &&
		sameStringMap(a.workspaceSettings.SearchExcludeWhen, settings.SearchExcludeWhen) &&
		a.workspaceSettings.UseIgnoreFiles() == settings.UseIgnoreFiles() &&
		a.workspaceSettings.UseGlobalIgnoreFiles() == settings.UseGlobalIgnoreFiles() &&
		a.workspaceSettings.UseParentIgnoreFiles() == settings.UseParentIgnoreFiles() &&
		a.workspaceSettings.ExplorerExcludeGitIgnore == settings.ExplorerExcludeGitIgnore {
		return
	}
	a.workspaceSettings = settings
	if watcherChanged {
		for _, folder := range a.workspaceFolders {
			a.startWatchFolder(folder.Path)
		}
	}
	for dir := range a.dirs {
		a.refreshDir(dir)
	}
	a.invalidateQuickFiles()
	a.filter.forQuery, a.filter.tree, a.filter.roots = "", nil, nil
	a.runFind()
}

type workspaceExcludeRules struct {
	patterns map[string]bool
	clauses  map[string]string
}

func mergedSearchExcludes(settings workspace.Settings) workspaceExcludeRules {
	out := workspaceExcludeRules{}
	keys := map[string]bool{}
	for pattern := range settings.FilesExclude {
		keys[pattern] = true
	}
	for pattern := range settings.FilesExcludeWhen {
		keys[pattern] = true
	}
	for pattern := range settings.SearchExclude {
		keys[pattern] = true
	}
	for pattern := range settings.SearchExcludeWhen {
		keys[pattern] = true
	}
	for pattern := range keys {
		enabled, useBool := settings.FilesExclude[pattern]
		when, useClause := settings.FilesExcludeWhen[pattern]
		if searchEnabled, ok := settings.SearchExclude[pattern]; ok {
			enabled, useBool, useClause = searchEnabled, true, false
		} else if searchWhen, ok := settings.SearchExcludeWhen[pattern]; ok {
			when, useBool, useClause = searchWhen, false, true
		}
		if useBool {
			if out.patterns == nil {
				out.patterns = map[string]bool{}
			}
			out.patterns[pattern] = enabled
		} else if useClause {
			if out.clauses == nil {
				out.clauses = map[string]string{}
			}
			out.clauses[pattern] = when
		}
	}
	return out
}

func ignoreFileOptions(settings workspace.Settings) search.IgnoreFileOptions {
	return search.IgnoreFileOptions{
		All:    !settings.UseIgnoreFiles(),
		Global: settings.UseIgnoreFiles() && !settings.UseGlobalIgnoreFiles(),
		Parent: settings.UseIgnoreFiles() && !settings.UseParentIgnoreFiles(),
	}
}

func siblingChecker(root string) func(string) bool {
	return func(siblingRel string) bool {
		_, err := os.Lstat(filepath.Join(root, filepath.FromSlash(siblingRel)))
		return err == nil
	}
}

func excludedByRules(rules workspaceExcludeRules, root, rel string) bool {
	return workspace.ExcludedWithSiblings(rules.patterns, rules.clauses, rel, siblingChecker(root))
}

func ripgrepExcludeGlob(pattern string) string {
	pattern = strings.TrimSuffix(filepath.ToSlash(pattern), "/")
	if !strings.Contains(pattern, "/") {
		return "/" + pattern
	}
	return pattern
}

func ripgrepExcludeGlobsForRules(rules workspaceExcludeRules) []string {
	for pattern := range rules.clauses {
		if strings.HasPrefix(pattern, "!") {
			return nil
		}
	}
	return ripgrepExcludeGlobs(rules.patterns)
}

func exceptionRevealPatterns(rules workspaceExcludeRules) map[string]bool {
	out := maps.Clone(rules.patterns)
	if len(rules.clauses) == 0 {
		return out
	}
	if out == nil {
		out = map[string]bool{}
	}
	for pattern := range rules.clauses {
		out[pattern] = true
	}
	return out
}

func ripgrepExcludeGlobs(patterns map[string]bool) []string {
	out := make([]string, 0, len(patterns))
	for pattern, enabled := range patterns {
		if !enabled {
			continue
		}
		if strings.HasPrefix(pattern, "!") {
			// ripgrep prunes excluded directories before See can apply VS Code's
			// exception, so exception-bearing sets are filtered exactly after search.
			return nil
		}
		out = append(out, ripgrepExcludeGlob(pattern))
	}
	slices.Sort(out)
	return out
}

func nestedWorkspaceRootGlobs(root string, roots []string) []string {
	var out []string
	for _, other := range roots {
		if other == root || !workspace.Within(root, other) {
			continue
		}
		if rel, err := filepath.Rel(root, other); err == nil && rel != "." {
			out = append(out, "/"+filepath.ToSlash(rel))
		}
	}
	slices.Sort(out)
	return out
}

func nestedWorkspaceRootPrefixes(root string, roots []string) []string {
	globs := nestedWorkspaceRootGlobs(root, roots)
	prefixes := make([]string, len(globs))
	for i, glob := range globs {
		prefixes[i] = strings.TrimPrefix(glob, "/")
	}
	return prefixes
}

func withinNestedWorkspaceRoot(rel string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
			return true
		}
	}
	return false
}
