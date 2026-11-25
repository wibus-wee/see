package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/nav"
	"see/internal/workspace"
	"see/locales"
)

// bookmarksState is the navigator's bookmark list: its filter and rows.
type bookmarksState struct {
	query string
	list  ui.ListState
}

func bookmarkText(key string) string { return locales.Catalog.Format(i18n.Locale(), key, nil) }

// bookmarkableEditor is the editor a bookmark can sit on: loaded text
// whose cursor reads its line.
func (a *App) bookmarkableEditor() (*doc, *editor.State, bool) {
	g := a.focus
	if g == nil || g.active == "" || strings.HasPrefix(g.active, markdownTabPrefix) {
		return nil, nil, false
	}
	d := a.docs[g.active]
	st := a.activeEditor()
	if d == nil || st == nil || d.untitled || d.kind != workspace.Text {
		return nil, nil, false
	}
	return d, st, true
}

// toggleBookmark marks or unmarks the editor's line.
func (a *App) toggleBookmark() {
	d, st, ok := a.bookmarkableEditor()
	if !ok {
		return
	}
	a.bookmarks.Toggle(d.path, st.CursorPos().Line)
	a.saveConfigSoon()
}

// bookmarkedLine lists the lines of path the user marked, for the
// editor's gutter and background.
func (a *App) bookmarkedLines(path string) []int {
	var lines []int
	for _, bm := range a.bookmarks.List {
		if bm.Path == path {
			lines = append(lines, bm.Line)
		}
	}
	return lines
}

// bookmarkLineBackgrounds tints the marked lines, a light wash a
// diagnostic's underline still reads through.
func bookmarkLineBackgrounds(lines []int, th *editor.Theme) []editor.LineBackground {
	var out []editor.LineBackground
	for _, line := range lines {
		out = append(out, editor.LineBackground{First: line, Last: line, Color: th.Info.Alpha(0.10)})
	}
	return out
}

// bookmarkGutterMarks dots the marked lines in the gutter.
func (a *App) bookmarkGutterMarks(path string) []editor.GutterMark {
	var out []editor.GutterMark
	for _, line := range a.bookmarkedLines(path) {
		out = append(out, editor.GutterMark{Line: line, Color: a.themeAccent})
	}
	return out
}

// openBookmark follows a mark the way goToProblem follows a diagnostic:
// the trail keeps where the user was.
func (a *App) openBookmark(bm nav.Bookmark) {
	if here, ok := a.here(); ok {
		a.hist.Push(here)
	}
	a.cancelNav()
	a.openFile(bm.Path, &editor.Pos{Line: bm.Line})
}

// bookmarksNavigator is the navigator's pane: a filter and the marks.
func (a *App) bookmarksNavigator(c *ui.Context) {
	ui.Column(c).FillWidth().Padding(8, 10).Gap(6).Children(func() {
		ui.Text(c, bookmarkText("bookmarksTitle")).Bold()
		ui.SearchField(c, &a.bookmarksUI.query).Label(bookmarkText("filterBookmarks")).Placeholder(bookmarkText("filterBookmarks")).FillWidth()
	})
	rows := a.filteredBookmarks()
	if len(rows) == 0 {
		text := bookmarkText("bookmarksEmpty")
		if strings.TrimSpace(a.bookmarksUI.query) != "" {
			text = bookmarkText("noMatchingBookmarks")
		}
		ui.Text(c, text).TextColor(colors(c).Muted).Padding(10, 12).FillWidth()
		return
	}
	ui.List(c, &a.bookmarksUI.list, len(rows)).ItemKey(func(i int) any { return rows[i] }).Grow(1).FillWidth().Label(bookmarkText("bookmarksTitle")).Rows(func(item ui.ListRow) {
		c := item.Context
		bm := rows[item.Index]
		var preview string
		if d := a.docs[bm.Path]; d != nil && d.buf != nil {
			snap := d.buf.Snapshot()
			if bm.Line < snap.LineCount() {
				preview = strings.TrimSpace(snap.Line(bm.Line))
			}
		}
		location := fmt.Sprintf("%s:%d", workspace.RelPath(a.root, bm.Path), bm.Line+1)
		row := ui.ButtonBase(c).FillWidth().Padding(7, 10).Label(location + " — " + preview).Tooltip(location).AlignItems(ui.Start)
		row.OnClick(func() { a.openBookmark(bm) })
		row.Children(func() {
			ui.Column(c).FillWidth().Gap(2).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					symbolIcon(c, icBookmark, 12, c.Theme().Accent)
					ui.Text(c, filepath.Base(bm.Path)).FontSize(12).Padding(4, 0, 0, 0).FillWidth()
					ui.Text(c, fmt.Sprintf(":%d", bm.Line+1)).FontSize(11).TextColor(colors(c).Muted)
				})
				if preview != "" {
					ui.Text(c, preview).FontSize(11).TextColor(colors(c).Muted).FillWidth().NoWrap()
				}
			})
		})
		row.ContextMenu(func(m *ui.Menu) {
			if m.Item(commandLabel("bookmark.remove")).Chosen() {
				a.bookmarks.Remove(bm.Path, bm.Line)
				a.saveConfigSoon()
			}
			if m.Item(commandLabel("bookmarks.clear")).Chosen() {
				a.bookmarks.Clear()
				a.saveConfigSoon()
			}
		})
	})
}

// filteredBookmarks is the marks a filter leaves, newest first.
func (a *App) filteredBookmarks() []nav.Bookmark {
	q := strings.ToLower(strings.TrimSpace(a.bookmarksUI.query))
	list := a.bookmarks.List
	out := make([]nav.Bookmark, 0, len(list))
	for i := len(list) - 1; i >= 0; i-- {
		bm := list[i]
		if q == "" || strings.Contains(strings.ToLower(workspace.RelPath(a.root, bm.Path)), q) {
			out = append(out, bm)
		}
	}
	return out
}
