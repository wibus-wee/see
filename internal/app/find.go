package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/sahilm/fuzzy"

	"see/internal/config"
	"see/internal/git"
	"see/internal/lsp"
	"see/internal/nav"
	"see/internal/search"
	uriutil "see/internal/uri"
	"see/internal/workspace"

	"see/internal/langserver"
)

// ----- The find navigator: text in the workspace, with ripgrep -----

// findState is the find navigator's: its query and options, and the
// results streaming in from ripgrep.
type findState struct {
	opts        search.Options
	field       ui.Handle
	focus       bool
	showFilters bool
	showReplace bool
	replacement string
	replacing   bool
	confirm     bool
	replaceNote string
	replaceErr  bool
	running     bool
	seq         int
	cancel      context.CancelFunc
	timer       *time.Timer
	ran         search.Options // what the results are of
	results     []search.File  // by path
	stats       search.Stats
	err         error
	rg          string // ripgrep's path, found once per workspace
	rgErr       error
	tree        ui.OutlineState[findKey]
	row         int
}

// findKey is a row of the results: a file, or one of its matches.
type findKey struct {
	path  string
	match int // -1 for the file's row
}

func (f *findState) init() {
	f.row = -1
	f.tree.List.Selected = &f.row
}

func (f *findState) reset() {
	if f.cancel != nil {
		f.cancel()
	}
	if f.timer != nil {
		f.timer.Stop()
	}
	opts := f.opts
	*f = findState{opts: opts}
	f.init()
}

func (f *findState) currentQuery() config.SavedFindQuery {
	opts := f.opts
	return config.SavedFindQuery{
		Query: opts.Query, CaseSensitive: opts.CaseSensitive, WholeWord: opts.WholeWord,
		Regexp: opts.Regexp, Include: opts.Include, Exclude: opts.Exclude,
	}
}

func (a *App) setSavedFindQuery(query config.SavedFindQuery, save bool) {
	if a.workspaceID == "" || strings.TrimSpace(query.Query) == "" {
		return
	}
	byWorkspace := a.cfg.SavedFindQueries
	queries := byWorkspace[a.workspaceID]
	i := slices.Index(queries, query)
	if save == (i >= 0) {
		return
	}
	if save {
		if byWorkspace == nil {
			byWorkspace = make(map[string][]config.SavedFindQuery)
			a.cfg.SavedFindQueries = byWorkspace
		}
		byWorkspace[a.workspaceID] = slices.Insert(queries, 0, query)
	} else if len(queries) == 1 {
		delete(byWorkspace, a.workspaceID)
	} else {
		byWorkspace[a.workspaceID] = slices.Delete(queries, i, i+1)
	}
	a.saveConfigSoon()
}

func (a *App) rerunSavedFindQuery(query config.SavedFindQuery) {
	f := &a.find
	if f.replacing {
		return
	}
	f.opts = search.Options{
		Query: query.Query, CaseSensitive: query.CaseSensitive, WholeWord: query.WholeWord,
		Regexp: query.Regexp, Include: query.Include, Exclude: query.Exclude,
		MaxMatches: f.opts.MaxMatches,
	}
	f.showFilters = query.Include != "" || query.Exclude != ""
	f.showReplace = false
	f.replacement, f.replaceNote, f.replaceErr = "", "", false
	a.runFind()
}

// findSoon searches once typing stops for a moment.
func (a *App) findSoon() {
	a.find.confirm = false
	if a.find.timer != nil {
		a.find.timer.Stop()
	}
	a.find.timer = time.AfterFunc(250*time.Millisecond, func() { a.post.Post(a.runFind) })
}

// runFind searches the workspace for the query, dropping the search on
// its way; results come in batches as ripgrep finds them.
func (a *App) runFind() {
	f := &a.find
	if f.timer != nil {
		f.timer.Stop()
		f.timer = nil
	}
	f.confirm = false
	if f.replacing {
		return
	}
	if f.cancel != nil {
		f.cancel()
		f.cancel = nil
	}
	f.seq++
	f.results, f.stats, f.err = nil, search.Stats{}, nil
	f.tree.Open = ui.Selection[findKey]{}
	f.row = -1
	f.ran = f.opts
	if strings.TrimSpace(f.opts.Query) == "" || a.root == "" {
		f.running = false
		return
	}
	if f.rg == "" && f.rgErr == nil {
		f.rg, f.rgErr = findRipgrep(a.root)
	}
	if f.rgErr != nil {
		f.err = f.rgErr
		f.running = false
		return
	}
	seq, gen, opts, rg := f.seq, a.gen, f.opts, f.rg
	roots := existingWorkspaceRoots(a.workspaceFolderPaths())
	excludePatterns := mergedSearchExcludes(a.workspaceSettings)
	excludeArgs := ripgrepExcludeGlobsForRules(excludePatterns)
	ignoreFiles := ignoreFileOptions(a.workspaceSettings)
	ctx, cancel := context.WithCancel(a.wsCtx)
	f.cancel = cancel
	f.running = true
	go func() {
		defer cancel()
		var mu sync.Mutex
		var batch []search.File
		last := time.Now()
		flush := func() {
			mu.Lock()
			b := batch
			batch = nil
			mu.Unlock()
			if len(b) == 0 {
				return
			}
			a.post.Post(func() {
				a.addFindResultsForSearch(gen, seq, b)
			})
		}
		limit := opts.MaxMatches
		if limit <= 0 {
			limit = 20000
		}
		var total search.Stats
		var searchErr error
		for _, root := range roots {
			rootOpts := opts
			rootOpts.NoIgnoreFiles = ignoreFiles
			rootOpts.MaxMatches = limit - total.Matches
			if rootOpts.MaxMatches <= 0 {
				break
			}
			st, err := search.Run(ctx, rg, root, rootOpts, append(slices.Clone(excludeArgs), nestedWorkspaceRootGlobs(root, roots)...), func(file search.File) {
				if rel, relErr := filepath.Rel(root, file.Path); relErr == nil && excludedByRules(excludePatterns, root, filepath.ToSlash(rel)) {
					return
				}
				mu.Lock()
				batch = append(batch, file)
				mu.Unlock()
				if time.Since(last) > 80*time.Millisecond {
					last = time.Now()
					flush()
				}
			})
			total.Files += st.Files
			total.Matches += st.Matches
			total.Truncated = total.Truncated || st.Truncated
			if err != nil {
				searchErr = errors.Join(searchErr, err)
				if errors.Is(err, context.Canceled) {
					break
				}
			}
			if total.Truncated {
				break
			}
		}
		flush()
		a.post.Post(func() {
			if !a.findResultCurrent(gen, seq) {
				return
			}
			a.find.running, a.find.cancel = false, nil
			a.find.stats.Files = len(a.find.results)
			a.find.stats.Matches = 0
			for _, file := range a.find.results {
				a.find.stats.Matches += len(file.Matches)
			}
			a.find.stats.Truncated = total.Truncated
			if searchErr != nil && !errors.Is(searchErr, context.Canceled) {
				a.find.err = searchErr
			}
		})
	}()
}

func (a *App) findResultCurrent(gen int, seq int) bool {
	return a.gen == gen && a.find.seq == seq
}

func (a *App) addFindResultsForSearch(gen int, seq int, files []search.File) {
	if a.findResultCurrent(gen, seq) {
		a.addFindResults(files)
	}
}

// addFindResults puts files of results in path order, open. Overlapping
// workspace roots can find the same file twice; its matches merge into one
// file identity instead of becoming two replace targets.
func (a *App) addFindResults(files []search.File) {
	f := &a.find
	for _, file := range files {
		i, found := slices.BinarySearchFunc(f.results, file.Path, func(r search.File, p string) int { return strings.Compare(r.Path, p) })
		if found {
			before := len(f.results[i].Matches)
			f.results[i].Matches = mergeFindMatches(f.results[i].Matches, file.Matches)
			f.stats.Matches += len(f.results[i].Matches) - before
			continue
		}
		f.results = slices.Insert(f.results, i, file)
		f.tree.Open.Add(findKey{path: file.Path, match: -1})
		f.stats.Files++
		f.stats.Matches += len(file.Matches)
	}
}

func mergeFindMatches(base, extra []search.Match) []search.Match {
	type key struct{ line, col, end int }
	seen := make(map[key]bool, len(base)+len(extra))
	out := make([]search.Match, 0, len(base)+len(extra))
	for _, m := range slices.Concat(base, extra) {
		k := key{m.Line, m.Col, m.End}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, m)
	}
	slices.SortFunc(out, func(a, b search.Match) int {
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		return a.Col - b.Col
	})
	return out
}

func (a *App) findFile(path string) *search.File {
	i, ok := slices.BinarySearchFunc(a.find.results, path, func(r search.File, p string) int { return strings.Compare(r.Path, p) })
	if !ok {
		return nil
	}
	return &a.find.results[i]
}

// findChildren lists a result row's children: a file's matches.
func (a *App) findChildren(k findKey) []findKey {
	if k.match >= 0 {
		return nil
	}
	file := a.findFile(k.path)
	if file == nil {
		return nil
	}
	out := make([]findKey, len(file.Matches))
	for i := range file.Matches {
		out[i] = findKey{path: k.path, match: i}
	}
	return out
}

// openMatch shows a match in the focused group, selected.
func (a *App) openMatch(k findKey) {
	file := a.findFile(k.path)
	if file == nil || k.match < 0 || k.match >= len(file.Matches) {
		return
	}
	m := file.Matches[k.match]
	a.cancelNav()
	pos := editor.Pos{Line: m.Line, Col: m.Col}
	a.openIn(a.focus, k.path, goTo{pos: &pos, selLen: m.End - m.Col})
}

// ----- Open Quickly: files by name, ranked by sahilm/fuzzy -----

type quickOpen struct {
	open            bool
	query           string
	ranked          string // the query results are of
	files           []string
	listing         bool
	scanGen         int // invalidations observed by the in-flight scan
	filesSeq        uint64
	filesCancel     context.CancelFunc
	fileMatchLabels map[string]string
	truncated       bool
	err             error
	results         []search.Scored
	commands        []command // palette rows results indexes, command mode only
	list            ui.ListState
	sel             int
	line            int // line to open, from a trailing :N in the query
	col             int // column to open, from a trailing :N:N
	gen             int
	symbols         []quickSymbol
	symbolCancel    context.CancelFunc
	symbolLoading   bool
	symbolErr       error
	filePath        string
	fileVersion     uint64
	fileSymbols     []quickSymbol
	fileLoaded      bool
	targetQuery     string
	targetFiles     []string
	targetPath      string
	targetLoading   bool
}

type quickSymbol struct {
	target target
	enc    string
	name   string
}

// maxQuickFiles bounds the files Open Quickly lists.
const maxQuickFiles = 200000

func (a *App) openQuickly() {
	a.openQuickPanel("")
}

func (a *App) openQuickPanel(query string) {
	q := &a.quick
	q.gen++
	if q.symbolCancel != nil {
		q.symbolCancel()
		q.symbolCancel = nil
	}
	q.open, q.query, q.ranked, q.sel = true, query, "\x00", 0
	q.line, q.col = 0, 0
	q.fileLoaded = false
	q.fileSymbols = nil
	// Opening the panel is the user's retry after a scan failure: the
	// sticky error clears here, so the scan below can run again.
	q.err = nil
	q.list.Selected = &q.sel
	if !strings.HasPrefix(query, ":") && !strings.HasPrefix(query, "@") {
		a.ensureFiles()
	}
}

// ensureFiles lists the workspace's files for Open Quickly and the tree's
// filter, once: the list lasts until the workspace changes or a refresh
// forgets it. A scan failure is sticky — q.err holds it — because anything
// else re-scans every frame forever: the failure post invalidates the
// ranking, the re-rank calls here, and the loop never ends. Opening Open
// Quickly clears the error, and workspace invalidations clear it alongside
// the file list — and invalidate the open panel's ranking, so the re-rank
// reaches this function and the scan is retried there. Those are the two
// deliberate retry points; anywhere else the error stands.
func (a *App) ensureFiles() {
	q := &a.quick
	if q.files != nil || q.listing || q.err != nil || a.root == "" {
		return
	}
	q.listing = true
	scanGen := q.scanGen
	gen := a.gen
	rootsEpoch := a.workspaceRootsEpoch
	q.filesSeq++
	seq := q.filesSeq
	logicalMultiRoot := len(a.workspaceFolderPaths()) > 1
	roots := existingWorkspaceRoots(a.workspaceFolderPaths())
	excludePatterns := mergedSearchExcludes(a.workspaceSettings)
	ignoreFiles := ignoreFileOptions(a.workspaceSettings)
	rg, _ := findRipgrep(a.root) // "" lists by walking
	ctx, cancel := context.WithCancel(a.wsCtx)
	q.filesCancel = cancel
	ignored := make(map[string]*git.Ignored, len(a.ignored))
	for root, ig := range a.ignored {
		ignored[root] = ig
	}
	nested := make(map[string][]string, len(roots))
	for _, root := range roots {
		nested[root] = nestedWorkspaceRootPrefixes(root, roots)
	}
	go func() {
		var files []string
		seenFiles := map[string]bool{}
		var truncated bool
		var err error
		for _, root := range roots {
			limit := maxQuickFiles - len(files)
			if limit <= 0 {
				truncated = true
				break
			}
			ig := ignored[root]
			if !ignoreFiles.All && (ignoreFiles.Global || ignoreFiles.Parent) {
				ig, _ = git.ReadIgnoredSources(ctx, root, !ignoreFiles.Global, !ignoreFiles.Parent)
			}
			nestedPrefixes := nested[root]
			skipDir := func(rel string) bool {
				return withinNestedWorkspaceRoot(rel, nestedPrefixes) || !ignoreFiles.All && ig != nil && ig.Dir(rel)
			}
			var rootFiles []string
			var rootTruncated bool
			rootFiles, rootTruncated, err = search.ListFiles(ctx, rg, root, limit, skipDir, func(rel string) bool {
				return withinNestedWorkspaceRoot(rel, nestedPrefixes) || excludedByRules(excludePatterns, root, rel)
			}, ignoreFiles)
			if err != nil {
				break
			}
			if logicalMultiRoot {
				for i, rel := range rootFiles {
					rootFiles[i] = filepath.Join(root, filepath.FromSlash(rel))
				}
			}
			for _, file := range rootFiles {
				if !seenFiles[file] {
					seenFiles[file] = true
					files = append(files, file)
				}
			}
			truncated = truncated || rootTruncated
			if rootTruncated {
				break
			}
		}
		a.post.Post(func() { a.finishQuickFileListing(gen, rootsEpoch, seq, scanGen, files, truncated, err, cancel) })
	}()
}

func (a *App) finishQuickFileListing(gen int, rootsEpoch, seq uint64, scanGen int, files []string, truncated bool, err error, cancel context.CancelFunc) {
	q := &a.quick
	if a.gen != gen || a.workspaceRootsEpoch != rootsEpoch || q.filesSeq != seq {
		return
	}
	cancel()
	q.filesCancel = nil
	q.listing = false
	if scanGen != q.scanGen {
		// The workspace changed while this scan ran: its result is
		// behind the disk by at least one event, and committing it
		// would serve that stale list indefinitely. Discard
		// instead: files stays nil, and the ranking is invalidated
		// so a consuming panel or filter re-enters the scan on its
		// next pass — without this the nil list would already have
		// been ranked, and nothing would scan again.
		if !strings.HasPrefix(q.query, ">") && !strings.HasPrefix(q.query, "#") && !strings.HasPrefix(q.query, "@") {
			q.ranked = "\x00"
		}
		a.filter.forQuery = ""
		return
	}
	q.files, q.truncated, q.err = files, truncated, err
	if err != nil {
		// A failed scan leaves no file list behind: a partial list
		// would pass for the workspace's contents while it is not,
		// and an empty non-nil list would pass for an empty
		// workspace. files stays nil — no valid list exists — and
		// err stays set, sticky until the panel is opened or the
		// workspace changes, so this frame does not re-scan forever.
		q.files = nil
	} else if q.files == nil {
		q.files = []string{}
	}
	if !strings.HasPrefix(q.query, ">") && !strings.HasPrefix(q.query, "#") && !strings.HasPrefix(q.query, "@") {
		q.ranked = "\x00" // rank files again
	}
	a.filter.forQuery = "" // filter again over the new list
}

func (a *App) invalidateQuickFiles() {
	q := &a.quick
	if q.filesCancel != nil {
		q.filesCancel()
		q.filesCancel = nil
	}
	q.filesSeq++
	q.listing, q.files, q.truncated, q.err = false, nil, false, nil
	q.fileMatchLabels = nil
	q.targetQuery, q.targetFiles, q.targetPath = "", nil, ""
	q.ranked = "\x00"
}

func (a *App) searchWorkspaceFiles(files []string, query string, limit int) []search.Scored {
	if limit <= 0 {
		return nil
	}
	type candidate struct {
		path, label string
	}
	var paths []string
	owners := make(map[string][]candidate)
	for _, path := range files {
		root := a.workspaceRootForPath(path)
		rel := path
		if root != "" {
			rel = workspace.RelPath(root, path)
		}
		labels := []string{a.workspacePath(path)}
		if len(a.workspaceFolders) > 1 && root != "" {
			labels = append(labels, filepath.ToSlash(filepath.Join(a.workspaceFolderName(root), filepath.FromSlash(rel))), filepath.ToSlash(filepath.Join(filepath.Base(root), filepath.FromSlash(rel))), rel)
		}
		labels = slices.Compact(labels)
		for _, label := range labels {
			paths = append(paths, label)
			owners[label] = append(owners[label], candidate{path: path, label: label})
		}
	}
	matches := search.Files(paths, query, min(len(paths), limit*4))
	results := make([]search.Scored, 0, min(limit, len(matches)))
	seen := make(map[string]bool)
	labels := make(map[string]string)
	used := make(map[string]int)
	for _, match := range matches {
		candidates := owners[match.Path]
		index := used[match.Path]
		if index >= len(candidates) {
			continue
		}
		used[match.Path] = index + 1
		owner := candidates[index]
		if seen[owner.path] {
			continue
		}
		seen[owner.path] = true
		labels[owner.path] = owner.label
		results = append(results, search.Scored{Path: owner.path, Hits: match.Hits})
		if len(results) == limit {
			break
		}
	}
	a.quick.fileMatchLabels = labels
	return results
}

// rankCommands lists the commands the palette offers, ranked against
// the query. A command matches on three spellings at once — its dotted
// id (font.bigger), its English name, and the localized title — so a
// user typing either language, or the id itself, finds it. The best
// score of the three wins; rows keep the command, not the matched
// spelling, as their identity.
func (a *App) rankCommands(query string) ([]command, []search.Scored) {
	var visible []command
	for _, cmd := range commands() {
		switch cmd.id {
		case "git.revealLineCommit":
			if !a.canRevealLineCommit() {
				continue
			}
		case "history.file":
			if !a.canShowFileHistory() {
				continue
			}
		case "git.revealHead", "git.jumpToDate", "git.showLog", "git.showHistory":
			if a.git.root == "" {
				continue
			}
		case "git.stash":
			if a.git.root == "" || len(a.git.changes) == 0 {
				continue
			}
		}
		visible = append(visible, cmd)
	}
	if query == "" {
		scored := make([]search.Scored, len(visible))
		for i, cmd := range visible {
			scored[i] = search.Scored{Path: commandLabel(cmd.id)}
		}
		return visible, scored
	}
	type match struct {
		cmd   command
		score int
	}
	var matches []match
	for _, cmd := range visible {
		best := -1
		for _, spelling := range []string{commandLabel(cmd.id), cmd.name, cmd.id} {
			if m := fuzzy.FindNoSort(query, []string{spelling}); len(m) > 0 && m[0].Score > best {
				best = m[0].Score
			}
		}
		if best >= 0 {
			matches = append(matches, match{cmd, best})
		}
	}
	slices.SortStableFunc(matches, func(x, y match) int { return y.score - x.score })
	cmds := make([]command, len(matches))
	scored := make([]search.Scored, len(matches))
	for i, m := range matches {
		cmds[i] = m.cmd
		scored[i] = search.Scored{Path: commandLabel(m.cmd.id)}
	}
	return cmds, scored
}

// rankQuick ranks the files for the query when it changed.
func (a *App) rankQuick() {
	q := &a.quick
	fileQuery, _, targeted := splitQuickFileSymbol(q.query)
	if targeted {
		a.ensureFiles()
		if fileQuery != q.targetQuery || len(q.files) != len(q.targetFiles) || len(q.files) > 0 && &q.files[0] != &q.targetFiles[0] {
			q.targetQuery, q.targetFiles, q.targetPath = fileQuery, q.files, ""
			if files := a.searchWorkspaceFiles(q.files, fileQuery, 60); len(files) > 0 {
				q.targetPath = a.docPath(a.workspaceFilePath(files[0].Path))
			}
		}
		path, version, loading := q.targetPath, uint64(0), false
		if path != "" {
			if a.workspaceTrusted() {
				d := a.docs[path]
				if d == nil {
					d = &doc{path: path, uri: uriOf(path), name: filepath.Base(path)}
					a.docs[path] = d
					a.loadDoc(d)
				}
				if d.buf != nil {
					version = d.buf.Version()
				}
				loading = d.loading
			}
		}
		if q.targetLoading != loading {
			q.targetLoading = loading
			q.ranked = "\x00"
		}
		if path != q.filePath || version != q.fileVersion {
			q.filePath, q.fileVersion, q.fileLoaded, q.fileSymbols = path, version, false, nil
			q.ranked = "\x00"
		}
	}
	if strings.HasPrefix(q.query, "@") {
		path, version := "", uint64(0)
		if st := a.activeEditor(); st != nil {
			path, version = a.activePath(), st.Buffer.Version()
		}
		if path != q.filePath || version != q.fileVersion {
			q.filePath, q.fileVersion, q.fileLoaded, q.fileSymbols = path, version, false, nil
			q.ranked = "\x00"
		}
	}
	if q.ranked == q.query {
		return
	}
	q.ranked = q.query
	q.gen++
	if q.symbolCancel != nil {
		q.symbolCancel()
		q.symbolCancel = nil
	}
	q.symbolLoading, q.symbolErr = false, nil
	q.symbols = nil
	if strings.HasPrefix(q.query, ":") {
		q.results = nil
		q.line, q.col = 0, 0
		if path, line, col, ok := splitQuickPosition(q.query); ok && path == "" && a.activeEditor() != nil {
			q.line, q.col = line, col
			q.results = []search.Scored{{Path: fmt.Sprintf("%s %d:%d", commandLabel("line.goto"), line, max(col, 1))}}
		}
	} else if strings.HasPrefix(q.query, ">") {
		q.commands, q.results = a.rankCommands(strings.TrimSpace(q.query[1:]))
	} else if strings.HasPrefix(q.query, "@") || targeted {
		q.results = nil
		if q.filePath != "" && a.workspaceTrusted() {
			if d := a.docs[q.filePath]; targeted && d != nil && d.loading {
				q.symbolLoading = true
			} else if targeted && a.docs[q.filePath] != nil && a.docs[q.filePath].err != nil {
				q.symbolErr = a.docs[q.filePath].err
			} else if q.fileLoaded {
				a.filterFileSymbols()
			} else {
				a.searchFileSymbols()
			}
		}
	} else if strings.HasPrefix(q.query, "#") {
		q.results = nil
		if a.root != "" && a.workspaceTrusted() && strings.TrimSpace(q.query[1:]) != "" {
			a.searchSymbols(strings.TrimSpace(q.query[1:]))
		}
	} else {
		a.ensureFiles()
		query := q.query
		q.line, q.col = 0, 0
		if path, line, col, ok := splitQuickPosition(query); ok && line > 0 {
			q.line, q.col = line, col
			query = path
		}
		if query == "" {
			q.results = a.recentTabs()
		} else {
			q.results = a.searchWorkspaceFiles(q.files, query, 60)
		}
	}
	q.sel = 0
}

func (a *App) searchSymbols(query string) {
	q := &a.quick
	ctx, cancel := context.WithTimeout(a.wsCtx, 20*time.Second)
	q.symbolCancel, q.symbolLoading = cancel, true
	gen, ranked, request := a.gen, q.ranked, q.gen
	servers := []*langserver.Server{a.lsp, a.tsLSP, a.clangLSP, a.cmakeLSP}
	go func() {
		defer cancel()
		select {
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			return
		}
		type answer struct {
			symbols []quickSymbol
			err     error
		}
		answers := make(chan answer, len(servers))
		count := 0
		for _, m := range servers {
			if m == nil {
				continue
			}
			count++
			go func(m *langserver.Server) {
				c, err := m.Get(ctx)
				var out []quickSymbol
				if err == nil {
					if client, ok := c.(interface {
						WorkspaceSymbols(context.Context, string) ([]lsp.WorkspaceSymbol, error)
					}); ok {
						var symbols []lsp.WorkspaceSymbol
						symbols, err = client.WorkspaceSymbols(ctx, query)
						for _, s := range symbols {
							if path, e := uriutil.ToPath(s.Location.URI); e == nil {
								out = append(out, quickSymbol{target: target{path: path, pos: s.Location.Range.Start}, enc: c.Encoding(), name: s.Name})
							}
						}
					}
				}
				answers <- answer{out, err}
			}(m)
		}
		var symbols []quickSymbol
		var firstErr error
		for i := 0; i < count; i++ {
			var result answer
			select {
			case result = <-answers:
			case <-ctx.Done():
				firstErr = ctx.Err()
				i = count
				continue
			}
			symbols = append(symbols, result.symbols...)
			if firstErr == nil {
				firstErr = result.err
			}
			if len(result.symbols) > 0 {
				partial := append([]quickSymbol(nil), symbols...)
				a.post.Post(func() {
					if a.gen != gen || !q.open || q.query != ranked || q.ranked != ranked || q.gen != request {
						return
					}
					q.symbols = partial
					q.results = make([]search.Scored, len(partial))
				})
			}
		}
		a.post.Post(func() {
			if a.gen != gen || !q.open || q.query != ranked || q.ranked != ranked || q.gen != request {
				return
			}
			q.symbolLoading, q.symbolCancel = false, nil
			q.symbols = symbols
			if len(symbols) == 0 {
				q.symbolErr = firstErr
			}
			q.results = make([]search.Scored, len(symbols))
			q.sel = 0
		})
	}()
}

func (a *App) chooseQuick(i int) {
	q := &a.quick
	a.rankQuick()
	if i < 0 || i >= len(q.results) {
		return
	}
	if strings.HasPrefix(q.query, ":") {
		st := a.activeEditor()
		if st == nil {
			return
		}
		if here, ok := a.here(); ok {
			a.hist.Push(here)
		}
		a.cancelNav()
		offset := st.Buffer.Offset(editor.Pos{Line: q.line - 1, Col: max(q.col-1, 0)})
		st.Select(offset, offset)
		a.focusEditor = st
		q.open = false
		return
	}
	p := q.results[i].Path
	q.open = false
	_, _, targeted := splitQuickFileSymbol(q.query)
	if strings.HasPrefix(q.query, "#") || strings.HasPrefix(q.query, "@") || targeted {
		s := q.symbols[i]
		if a.focus != nil {
			if st := a.activeEditor(); st != nil {
				pos := st.CursorPos()
				a.hist.Push(nav.Entry{Path: a.activePath(), Line: pos.Line, ByteColumn: pos.Col})
			}
			a.cancelNav()
			pos := s.target.pos
			a.openIn(a.focus, s.target.path, goTo{lsp: &pos, enc: s.enc})
		}
		return
	}
	if strings.HasPrefix(q.query, ">") {
		if i < len(q.commands) {
			cmd := q.commands[i]
			if a.commandEnabled(cmd.id) {
				cmd.run(a)
			}
		}
		return
	}
	if q.line > 0 {
		path := a.workspaceFilePath(p)
		pos := editor.Pos{Line: q.line - 1, Col: q.col}
		if q.col > 0 {
			pos.Col = q.col - 1
		}
		a.openFile(path, &pos)
	} else {
		a.previewOpen(a.workspaceFilePath(p), true)
	}
}

// joinRoot is the one join of a quick-open path into the workspace: the
// list it comes from is validated when read, and anything malformed
// degrades to the root rather than reaching outside it.
func joinRoot(root, rel string) string {
	p, err := workspace.JoinRel(root, rel)
	if err != nil {
		return root
	}
	return p
}

func splitQuickFileSymbol(query string) (string, string, bool) {
	if query == "" || strings.ContainsAny(query[:1], ">#@:") {
		return "", "", false
	}
	i := strings.LastIndex(query, "@")
	if i <= 0 || query[i-1] == '/' || strings.Contains(query[i+1:], "/") {
		return "", "", false
	}
	return query[:i], query[i+1:], true
}

// maxQuickRecents bounds the files an empty Open Quickly lists.
const maxQuickRecents = 15

// recentTabs lists the files activated lately for Open Quickly's
// empty query, the one active most lately first. A file activated and
// since closed still lists, so it can be opened again without typing,
// while a file gone from disk or outside the workspace does not; and
// the current document sits it out, first as it would be.
func (a *App) recentTabs() []search.Scored {
	var out []search.Scored
	for i := len(a.recents) - 1; i >= 0 && len(out) < maxQuickRecents; i-- {
		path := a.recents[i]
		if a.root == "" || !a.withinWorkspace(path) || path == a.activePath() {
			continue
		}
		if fi, err := os.Stat(path); err != nil || !fi.Mode().IsRegular() {
			continue
		}
		if len(a.workspaceFolders) > 1 {
			out = append(out, search.Scored{Path: path})
		} else if rel := workspace.RelPath(a.root, path); rel != path {
			out = append(out, search.Scored{Path: rel})
		}
	}
	return out
}

// splitQuickPosition splits a trailing :line or :line:col from an Open
// Quickly query, so that "foo.go:42" opens foo.go at line 42.
func splitQuickPosition(q string) (string, int, int, bool) {
	parts := strings.Split(q, ":")
	if len(parts) >= 3 {
		if line, err := strconv.Atoi(parts[len(parts)-2]); err == nil && line > 0 {
			if col, err := strconv.Atoi(parts[len(parts)-1]); err == nil && col >= 0 {
				return strings.Join(parts[:len(parts)-2], ":"), line, col, true
			}
		}
	}
	if len(parts) >= 2 {
		if line, err := strconv.Atoi(parts[len(parts)-1]); err == nil && line > 0 {
			return strings.Join(parts[:len(parts)-1], ":"), line, 0, true
		}
	}
	return q, 0, 0, false
}
