package app

import (
	"cmp"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"see/internal/git"
	"see/internal/langserver"
	"see/internal/languages"
	"see/internal/lsp"
	"see/internal/workspace"
	"see/locales"
)

type problemsState struct {
	query    string
	list     ui.ListState
	disk     map[string]*diskProblems
	started  bool
	starting int
}

type diskProblems struct {
	server   *langserver.Server
	client   langserver.Client
	encoding string
	items    []lsp.Diagnostic
}

type problemLocation struct {
	path       string
	version    uint64
	diagnostic editor.Diagnostic
	pos        editor.Pos
	disk       *diskProblems
	ordinal    int
}

func problemText(key string) string { return locales.Catalog.Format(i18n.Locale(), key, nil) }

func (a *App) problemLocations() []problemLocation {
	var rows []problemLocation
	if a.root == "" {
		return nil
	}
	query := strings.ToLower(strings.TrimSpace(a.problems.query))
	matches := func(path, message, source string) bool {
		return query == "" || strings.Contains(strings.ToLower(a.workspacePath(path)+" "+message+" "+source), query)
	}
	for path, d := range a.docs {
		if !currentProblemBuffer(d) || !a.withinWorkspace(path) {
			continue
		}
		for i, diagnostic := range d.diagnostics {
			if diagnostic.Start < 0 || diagnostic.Start > d.buf.Len() {
				continue
			}
			if matches(path, diagnostic.Message, diagnostic.Source) {
				rows = append(rows, problemLocation{path: path, version: d.diagnosticVersion, diagnostic: diagnostic, pos: d.buf.Pos(diagnostic.Start), ordinal: i})
			}
		}
	}
	for path, report := range a.problems.disk {
		if !a.currentDiskProblems(path, report) {
			continue
		}
		for i, diagnostic := range report.items {
			if matches(path, diagnostic.Message, diagnostic.Source) {
				start := diagnostic.Range.Start
				rows = append(rows, problemLocation{
					path: path, disk: report, ordinal: i, pos: editor.Pos{Line: start.Line, Col: start.Character},
					diagnostic: editor.Diagnostic{Severity: diagnosticSeverity(diagnostic.Severity), Message: diagnostic.Message, Source: diagnostic.Source},
				})
			}
		}
	}
	slices.SortFunc(rows, func(a, b problemLocation) int {
		if n := cmp.Compare(a.path, b.path); n != 0 {
			return n
		}
		if n := cmp.Compare(a.pos.Line, b.pos.Line); n != 0 {
			return n
		}
		if n := cmp.Compare(a.pos.Col, b.pos.Col); n != 0 {
			return n
		}
		if n := cmp.Compare(a.diagnostic.Severity, b.diagnostic.Severity); n != 0 {
			return n
		}
		if n := cmp.Compare(a.diagnostic.Message, b.diagnostic.Message); n != 0 {
			return n
		}
		return cmp.Compare(a.ordinal, b.ordinal)
	})
	return rows
}

func currentProblemBuffer(d *doc) bool {
	return d != nil && d.buf != nil && !d.gone && d.diagnostics != nil && d.diagnosticVersion == d.buf.Version()
}

func (a *App) currentDiskProblems(path string, report *diskProblems) bool {
	if a.diagnosticsServerFor(path) != report.server || report.server.Current() != report.client {
		return false
	}
	d := a.docs[path]
	return d == nil || (!d.gone && !currentProblemBuffer(d) && (d.buf == nil || !d.buf.Modified()))
}

func (a *App) openProblem(p problemLocation) {
	d := a.docs[p.path]
	if p.disk != nil {
		if a.problems.disk[p.path] != p.disk || !a.currentDiskProblems(p.path, p.disk) {
			return
		}
	} else if !currentProblemBuffer(d) || d.diagnosticVersion != p.version || !slices.Contains(d.diagnostics, p.diagnostic) {
		return
	}
	if here, ok := a.here(); ok {
		a.hist.Push(here)
	}
	a.cancelNav()
	to := goTo{pos: &p.pos}
	if p.disk != nil {
		pos := p.disk.items[p.ordinal].Range.Start
		to = goTo{lsp: &pos, enc: p.disk.encoding}
	}
	a.openFileAt(p.path, to)
}

func (a *App) showProblems() {
	a.sidebarOff, a.navigator = false, navProblems
	a.startProjectDiagnostics()
}

func (a *App) startProjectDiagnostics() {
	if a.root == "" || a.problems.started || !a.workspaceTrusted() {
		return
	}
	a.problems.started = true
	a.problems.starting++
	gen, ctx := a.gen, a.wsCtx
	folders := slices.Clone(a.workspaceFolders)
	type scanRoot struct {
		root, realRoot string
		ignored        *git.Ignored
	}
	roots := make([]scanRoot, 0, len(folders))
	for _, folder := range folders {
		realRoot := folder.Path
		if resolved, err := filepath.EvalSymlinks(folder.Path); err == nil {
			realRoot = resolved
		}
		roots = append(roots, scanRoot{root: folder.Path, realRoot: realRoot, ignored: a.ignored[folder.Path]})
	}
	type lane struct {
		server *langserver.Server
		covers func(string) bool
	}
	lanes := []lane{
		{a.lsp, func(path string) bool {
			name := filepath.Base(path)
			return strings.EqualFold(filepath.Ext(path), ".go") || name == "go.mod" || name == "go.work"
		}},
		{a.tsLSP, func(path string) bool { return languages.TypeScriptLanguage(path) != "" }},
		{a.cmakeLSP, func(path string) bool { return languages.CMakeLanguage(path) != "" }},
		{a.clangLSP, func(path string) bool { return languages.ClangLanguage(path) != "" }},
	}
	for i := range a.customLSPSpecs {
		lanes = append(lanes, lane{a.customLSPs[i], a.customCovers[i]})
	}
	go func() {
		found := map[*langserver.Server]string{}
		var scanErr error
		for _, scan := range roots {
			if len(found) == len(lanes) || ctx.Err() != nil {
				break
			}
			err := filepath.WalkDir(scan.realRoot, func(path string, entry fs.DirEntry, err error) error {
				if ctx.Err() != nil || len(found) == len(lanes) {
					return fs.SkipAll
				}
				if err != nil {
					return nil
				}
				rel, _ := filepath.Rel(scan.realRoot, path)
				if entry.IsDir() {
					if path != scan.realRoot && (workspace.Ignored(entry.Name()) || scan.ignored.Dir(filepath.ToSlash(rel))) {
						return fs.SkipDir
					}
					return nil
				}
				if entry.Type().IsRegular() {
					p := filepath.Join(scan.root, rel)
					for _, lane := range lanes {
						if lane.server != nil && lane.covers(p) {
							if _, ok := found[lane.server]; !ok {
								found[lane.server] = p
							}
							break
						}
					}
				}
				return nil
			})
			if err != nil && scanErr == nil {
				scanErr = err
			}
		}
		a.post.Post(func() {
			if gen != a.gen {
				return
			}
			a.problems.starting--
			if scanErr != nil {
				a.logf("project diagnostics: %v", scanErr)
			}
			for _, path := range found {
				a.startProblemServer(path)
			}
		})
	}()
}

func (a *App) startProblemServer(path string) {
	if !a.workspaceTrusted() {
		return
	}
	m := a.diagnosticsServerFor(path)
	if m == nil {
		return
	}
	if state, _ := m.Status(); state != langserver.Off {
		return
	}
	ctx, gen := a.wsCtx, a.gen
	a.problems.starting++
	go func() {
		_, _ = m.Get(ctx)
		a.post.Post(func() {
			if gen == a.gen {
				a.problems.starting--
			}
		})
	}()
}

func (a *App) problemsNavigator(c *ui.Context) {
	ui.Column(c).FillWidth().Padding(8, 10).Gap(6).Children(func() {
		ui.Text(c, problemText("problemsList")).Bold()
		ui.SearchField(c, &a.problems.query).Label(problemText("filterProblems")).Placeholder(problemText("filterProblems")).FillWidth()
		ui.Text(c, problemText("problemsProject")).FontSize(11).TextColor(colors(c).Muted).FillWidth()
	})
	rows := a.problemLocations()
	if len(rows) == 0 {
		if a.root != "" && !a.workspaceTrusted() {
			ui.Column(c).Padding(6, 10).Gap(6).Children(func() {
				ui.Text(c, a.trustProblemDetail()).TextColor(colors(c).Muted).FillWidth()
				ui.Button(c, a.trustButtonLabel()).OnClick(func() { a.trustAsk = true })
			})
			return
		}
		text := "noProblemsReported"
		switch {
		case strings.TrimSpace(a.problems.query) != "":
			text = "noMatchingProblems"
		case a.problems.starting > 0:
			text = "problemsStarting"
		}
		ui.Text(c, problemText(text)).TextColor(colors(c).Muted).Padding(6, 10).FillWidth()
		return
	}
	ui.List(c, &a.problems.list, len(rows)).ItemKey(func(i int) any { return rows[i] }).Grow(1).FillWidth().Label(problemText("problemsList")).Rows(func(item ui.ListRow) {
		c := item.Context
		p := rows[item.Index]
		label := problemText("problemError")
		color := colors(c).Err
		switch p.diagnostic.Severity {
		case editor.SeverityWarning:
			label, color = problemText("problemWarning"), colors(c).Muted
		case editor.SeverityInfo:
			label, color = problemText("problemInfo"), colors(c).Muted
		case editor.SeverityHint:
			label, color = problemText("problemHint"), colors(c).Dim
		}
		location := fmt.Sprintf("%s:%d:%d", a.workspacePath(p.path), p.pos.Line+1, p.pos.Col+1)
		row := ui.ButtonBase(c).FillWidth().Padding(7, 10).Label(label + ": " + p.diagnostic.Message + " — " + location).AlignItems(ui.Start)
		row.OnClick(func() { a.openProblem(p) })
		row.Children(func() {
			ui.Column(c).FillWidth().Gap(2).Children(func() {
				ui.Text(c, label+": "+p.diagnostic.Message).FontSize(12).TextColor(color).FillWidth()
				ui.Text(c, location).FontSize(11).TextColor(colors(c).Muted).FillWidth()
				if p.diagnostic.Source != "" {
					ui.Text(c, p.diagnostic.Source).FontSize(11).TextColor(colors(c).Dim).FillWidth()
				}
			})
		})
	})
}
