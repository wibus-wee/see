package app

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"see/internal/workspace"
	"see/locales"
)

type problemsState struct {
	query string
	list  ui.ListState
}

// problemLocation retains the buffer version so a click cannot use an obsolete row.
type problemLocation struct {
	path       string
	version    uint64
	diagnostic editor.Diagnostic
	ordinal    int
}

func problemText(key string) string { return locales.Catalog.Format(i18n.Locale(), key, nil) }

func (a *App) problemLocations() []problemLocation {
	var rows []problemLocation
	query := strings.ToLower(strings.TrimSpace(a.problems.query))
	for path, d := range a.docs {
		if d.buf == nil || d.diagnosticVersion != d.buf.Version() || !workspace.Within(a.root, path) {
			continue
		}
		for i, diagnostic := range d.diagnostics {
			if diagnostic.Start < 0 || diagnostic.Start > d.buf.Len() {
				continue
			}
			if query != "" && !strings.Contains(strings.ToLower(workspace.RelPath(a.root, path)+" "+diagnostic.Message+" "+diagnostic.Source), query) {
				continue
			}
			rows = append(rows, problemLocation{path: path, version: d.diagnosticVersion, diagnostic: diagnostic, ordinal: i})
		}
	}
	slices.SortFunc(rows, func(a, b problemLocation) int {
		if n := cmp.Compare(a.path, b.path); n != 0 {
			return n
		}
		if n := cmp.Compare(a.diagnostic.Start, b.diagnostic.Start); n != 0 {
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

func (a *App) openProblem(p problemLocation) {
	d := a.docs[p.path]
	if d == nil || d.buf == nil || d.buf.Version() != p.version || d.diagnosticVersion != p.version || !slices.Contains(d.diagnostics, p.diagnostic) {
		return
	}
	if here, ok := a.here(); ok {
		a.hist.Push(here)
	}
	a.cancelNav()
	pos := d.buf.Pos(p.diagnostic.Start)
	a.openFile(p.path, &pos)
}

func (a *App) problemsNavigator(c *ui.Context) {
	ui.Column(c).FillWidth().Padding(8, 10).Gap(6).Children(func() {
		ui.Text(c, problemText("problemsList")).Bold()
		ui.SearchField(c, &a.problems.query).Label(problemText("filterProblems")).Placeholder(problemText("filterProblems")).FillWidth()
		ui.Text(c, problemText("problemsOpenFiles")).FontSize(11).TextColor(colors(c).Muted).FillWidth()
	})
	rows := a.problemLocations()
	if len(rows) == 0 {
		text := problemText("noProblemsOpenFiles")
		if strings.TrimSpace(a.problems.query) != "" {
			text = problemText("noMatchingProblems")
		}
		ui.Text(c, text).TextColor(colors(c).Muted).Padding(10, 12).FillWidth()
		return
	}
	ui.List(c, &a.problems.list, len(rows)).ItemKey(func(i int) any { return rows[i] }).Grow(1).FillWidth().Label(problemText("problemsList")).Rows(func(item ui.ListRow) {
		c := item.Context
		p := rows[item.Index]
		d := a.docs[p.path]
		pos := d.buf.Pos(p.diagnostic.Start)
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
		location := fmt.Sprintf("%s:%d:%d", workspace.RelPath(a.root, p.path), pos.Line+1, pos.Col+1)
		row := ui.ButtonBase(c).FillWidth().Padding(7, 10).Label(label + ": " + p.diagnostic.Message + " — " + location).Tooltip(p.diagnostic.Message).AlignItems(ui.Start)
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
