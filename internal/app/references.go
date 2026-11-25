package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"see/internal/lsp"
	"see/internal/nav"
	"see/internal/workspace"
	"see/locales"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// The pinned references panel: a reading queue of the results Find
// References already fetches, kept in the bottom panel so it survives
// clicking into the code. The popover peek stays the quick way to see a
// list once; pinning moves it here, filtered and remembered, as the
// code-understanding exploration describes.

// referenceRun is one pinned list: the results of a request, kept after
// it is replaced so the reader can return to it.
type referenceRun struct {
	choices   *defChoices // the list, with the previews loadRelatedPreviews filled
	from      nav.Entry   // where it was asked, for the document version
	version   uint64      // the asking document's version when it was asked
	symbol    string      // the word the caret was on, labelling the run
	collapsed map[string]bool
}

// label names the run in recents: "source — 4 references".
func (r *referenceRun) label() string {
	name := r.symbol
	if name == "" {
		name = referenceTitle(r.choices.title)
	}
	return fmt.Sprintf("%s — %d %s", name, len(r.choices.locs), referenceTitle(r.choices.title))
}

// referenceTitle names a list of references or definitions in the app's
// language; other titles are already the names the app shows.
func referenceTitle(title string) string {
	switch title {
	case "references":
		return locales.MsgReferences()
	case "definitions":
		return locales.MsgDefinitions()
	case "callers":
		return locales.InspectorCallers()
	}
	return title
}

// referenceList is the panel's state: the runs it remembers, the one it
// shows, and the filter and walking position of that one.
type referenceList struct {
	runs     []*referenceRun // recent lists, newest first; the active one is runs[0]
	filter   string          // narrows rows by path, symbol name or line text
	cursor   int             // the row Space and Up/Down walk, an index of locs
	read     map[int]bool    // the rows Space marked read
	loading  bool            // a Pin References request is in flight
	err      error           // the last request's failure, shown with Retry
	noServer bool            // the file has no language server
	symbol   string          // the word the caret is on, for the request in flight
	fromG    *group          // the group the request was made from
	fromD    *doc            // the document the request was made from
	pending  bool            // a retry should re-ask, after trust or a server appearing
	field    ui.Handle       // the filter field's focus
	list     ui.ListState    // the rows' keyboard selection
	focused  bool            // the list had the keyboard in the last frame
}

const (
	maxReferenceRuns     = 5   // the recents kept
	maxReferencePreviews = 500 // the references previews are loaded for
)

// referenceGroups indexes locs by file, in file order, as relatedView
// groups them in the peek.
func referenceGroups(ch *defChoices) (paths []string, groups map[string][]int) {
	groups = make(map[string][]int)
	for i, t := range ch.locs {
		if _, ok := groups[t.path]; !ok {
			paths = append(paths, t.path)
		}
		groups[t.path] = append(groups[t.path], i)
	}
	return paths, groups
}

// relatedList draws the reference rows of ch grouped by file: a file
// row with its count and collapse arrow, then one row per reference
// showing the matched line. The peek and the panel share it; select
// and open are what a click and a double click do, filter hides rows
// that do not match, and read dims a row the reader finished.
func (a *App) relatedList(c *ui.Context, ch *defChoices, collapsed map[string]bool, filter string, read map[int]bool, selectRow, open func(int)) {
	if collapsed == nil {
		collapsed = make(map[string]bool)
	}
	filter = strings.ToLower(strings.TrimSpace(filter))
	paths, groups := referenceGroups(ch)
	for _, path := range paths {
		indices := groups[path]
		visible := indices[:0]
		for _, i := range indices {
			if referenceMatches(ch, i, filter) {
				visible = append(visible, i)
			}
		}
		if filter != "" && len(visible) == 0 {
			continue
		}
		file := ui.ButtonBase(c.Key(path)).FillWidth().Padding(5, 8).Gap(6).Label(locales.MsgToggle() + ch.title + locales.MsgIn() + workspace.RelPath(a.root, path)).Tooltip(path)
		if file.Hovered() {
			file.Background(colors(c).Pill)
		}
		file.Children(func() {
			arrow := "▾"
			if collapsed[path] {
				arrow = "▸"
			}
			ui.Text(c, arrow).TextColor(colors(c).Muted)
			ui.Text(c, filepath.Base(path)).FontSize(12).SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0).Shrink(1)
			ui.Text(c, workspace.RelPath(a.root, filepath.Dir(path))).FontSize(11).TextColor(colors(c).Muted).SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0).Grow(1).Shrink(1)
			ui.Text(c, fmt.Sprint(len(visible))).FontSize(11).TextColor(colors(c).Muted)
		})
		file.OnClick(func() { collapsed[path] = !collapsed[path] })
		if collapsed[path] {
			continue
		}
		for _, i := range visible {
			t := ch.locs[i]
			label := fmt.Sprintf("%s:%d", workspace.RelPath(a.root, t.path), t.pos.Line+1)
			text := t.name
			if n := t.pos.Line - t.previewStart; n >= 0 && n < len(t.preview) {
				text = strings.TrimSpace(t.preview[n])
			}
			if text == "" {
				text = label
			}
			row := ui.ButtonBase(c.Key(i)).FillWidth().Padding(4, 8, 4, 25).Radius(4).Justify(ui.Start).Label(label).Tooltip(locales.MsgSelectToPreviewDoubleClickToOpen())
			switch {
			case i == ch.selected:
				row.Background(colors(c).SelectedRow)
			case row.Hovered():
				row.Background(colors(c).Pill)
			}
			row.Children(func() {
				tx := ui.Text(c, text).FontSize(12).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
				if read != nil && read[i] {
					tx.TextColor(colors(c).Muted)
				}
			})
			if row.DoubleClicked() {
				open(i)
			} else {
				row.OnClick(func() { selectRow(i) })
			}
		}
	}
}

// referenceMatches reports whether the row of locs[i] passes filter,
// matched against its path, its symbol name and the matched line's text.
func referenceMatches(ch *defChoices, i int, filter string) bool {
	if filter == "" {
		return true
	}
	t := ch.locs[i]
	if strings.Contains(strings.ToLower(t.path), filter) || strings.Contains(strings.ToLower(t.name), filter) {
		return true
	}
	if n := t.pos.Line - t.previewStart; n >= 0 && n < len(t.preview) {
		return strings.Contains(strings.ToLower(t.preview[n]), filter)
	}
	return strings.Contains(strings.ToLower(workspace.RelPath("", t.path)), filter)
}

// pinReferences moves ch, the references list the peek shows, into
// the bottom panel and closes the peek.
func (a *App) pinReferences(ch *defChoices) {
	if ch == nil || ch.title != "references" || len(ch.locs) == 0 {
		return
	}
	a.pushReferenceRun(&referenceRun{
		choices: ch,
		from:    ch.from,
		version: a.referenceDocVersion(ch.from),
		symbol:  a.referenceSymbol(ch.from),
	})
	a.term.open, a.term.view, a.term.focus = true, panelReferences, nil
	a.choices = nil
}

// pinReferencesCommand runs Pin References: the references list the
// peek already shows is pinned where it is; without one, references at
// the caret are asked for and pinned when they arrive.
func (a *App) pinReferencesCommand() {
	if a.choices != nil && a.choices.title == "references" && len(a.choices.locs) > 0 {
		a.pinReferences(a.choices)
		return
	}
	a.findUsagesPinned()
}

// findUsagesPinned asks for references at the caret, as Find References
// does, but puts the results in the panel instead of a peek.
func (a *App) findUsagesPinned() {
	g := a.focus
	if g == nil {
		return
	}
	d := a.docs[g.active]
	st := g.eds[g.active]
	if d == nil || d.buf == nil || st == nil {
		return
	}
	m := a.serverFor(d.path)
	rl := &a.references
	rl.fromG, rl.fromD, rl.pending = g, d, false
	if m == nil {
		rl.noServer = true
		a.term.open, a.term.view, a.term.focus = true, panelReferences, nil
		return
	}
	rl.noServer = false
	if !a.cfg.IsTrusted(a.root) {
		rl.pending = true
		a.trustAsk = true
		a.afterTrust = func() { a.findUsagesPinned() }
		a.term.open, a.term.view, a.term.focus = true, panelReferences, nil
		return
	}
	a.term.maximized = false
	a.cancelNav()
	snap := d.buf.Snapshot()
	pos := st.CursorPos()
	from := nav.Entry{Path: d.path, Line: pos.Line, ByteColumn: pos.Col}
	line := snap.Line(pos.Line)
	rl.symbol = symbolAt(line, pos.Col)
	rl.loading, rl.err = true, nil
	a.term.open, a.term.view, a.term.focus = true, panelReferences, nil
	ctx, cancel := context.WithTimeout(a.wsCtx, 20*time.Second)
	a.navCancel = cancel
	a.finding = true
	a.findingLabel = "Finding references…"
	a.notice = ""
	seq, gen := a.navSeq, a.gen
	version := snap.Version()
	go func() {
		defer cancel()
		c, err := m.Get(ctx)
		var locations []lsp.Location
		enc := ""
		if err == nil {
			err = m.Sync(ctx, c, d.uri, snap)
		}
		if err == nil {
			enc = c.Encoding()
			at := lsp.Position{Line: pos.Line, Character: lsp.ColumnToLSP(line, pos.Col, enc)}
			if client, ok := c.(interface {
				References(context.Context, string, lsp.Position) ([]lsp.Location, error)
			}); ok {
				locations, err = client.References(ctx, d.uri, at)
			} else {
				err = errors.New("this language server does not support references")
			}
		}
		targets, _ := targetsOf(locations)
		a.post.Post(func() {
			if a.gen != gen || a.navSeq != seq {
				return
			}
			a.finding = false
			a.navCancel = nil
			rl.loading = false
			if err != nil {
				rl.err = err
				return
			}
			if len(targets) == 0 {
				a.referencesEmpty = true
				return
			}
			ch := &defChoices{g: g, from: from, enc: enc, locs: targets, title: "references"}
			a.git.peek = nil
			a.pushReferenceRun(&referenceRun{
				choices:   ch,
				from:      from,
				version:   version,
				symbol:    rl.symbol,
				collapsed: make(map[string]bool),
			})
			a.loadRelatedPreviews(ch)
			a.selectRelated(ch, ch.selected)
		})
	}()
}

// symbolAt is the identifier at col of line, for labelling a pinned list.
func symbolAt(line string, col int) string {
	isWord := func(r byte) bool {
		return r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= 0x80
	}
	col = min(col, len(line))
	start := col
	for start > 0 && isWord(line[start-1]) {
		start--
	}
	end := col
	for end < len(line) && isWord(line[end]) {
		end++
	}
	return line[start:end]
}

// pushReferenceRun shows run in the panel and remembers it, dropping the
// oldest past maxReferenceRuns and clearing the last request's state.
func (a *App) pushReferenceRun(run *referenceRun) {
	rl := &a.references
	if run.collapsed == nil {
		run.collapsed = make(map[string]bool)
	}
	rl.runs = append([]*referenceRun{run}, rl.runs...)
	if len(rl.runs) > maxReferenceRuns {
		rl.runs = rl.runs[:maxReferenceRuns]
	}
	rl.filter, rl.cursor = "", -1
	rl.read = make(map[int]bool)
	rl.err, rl.noServer, rl.pending = nil, false, false
	a.referencesEmpty = false
	if len(run.choices.locs) > maxReferencePreviews {
		// Loading previews for every hit of a huge list is memory the
		// panel does not need; the run keeps its rows without them.
		for i := range run.choices.locs[maxReferencePreviews:] {
			run.choices.locs[maxReferencePreviews+i].preview = nil
		}
	}
}

// referenceDocVersion is the version of the document the run was asked
// from, so a stale list — asked before an edit — can be dropped.
func (a *App) referenceDocVersion(from nav.Entry) uint64 {
	if d := a.docs[from.Path]; d != nil && d.buf != nil {
		return d.buf.Version()
	}
	return 0
}

// referenceSymbol is the word at the position the run was asked from,
// the ByteColumn the request recorded.
func (a *App) referenceSymbol(from nav.Entry) string {
	if d := a.docs[from.Path]; d != nil && d.buf != nil {
		snap := d.buf.Snapshot()
		if from.Line >= 0 && from.Line < snap.LineCount() {
			return symbolAt(snap.Line(from.Line), from.ByteColumn)
		}
	}
	return ""
}

// referencesStale reports whether the document the run was asked from
// changed since: an edit made the list's positions unreliable.
func (a *App) referencesStale(run *referenceRun) bool {
	if run == nil {
		return false
	}
	d := a.docs[run.from.Path]
	if d == nil || d.buf == nil {
		return false
	}
	return d.buf.Version() != run.version
}

// referenceAdvance marks the cursor's row read and walks to the next
// unread row, wrapping past read ones. Space on a row is this.
func (a *App) referenceAdvance() {
	rl := &a.references
	run := rl.activeRun()
	if run == nil {
		return
	}
	ch := run.choices
	if rl.cursor >= 0 && rl.cursor < len(ch.locs) {
		rl.read[rl.cursor] = true
	}
	next := -1
	for i := 1; i <= len(ch.locs); i++ {
		j := (rl.cursor + i) % len(ch.locs)
		if !rl.read[j] && referenceMatches(ch, j, strings.ToLower(strings.TrimSpace(rl.filter))) {
			next = j
			break
		}
	}
	if next < 0 {
		next = (rl.cursor + 1) % len(ch.locs)
	}
	rl.cursor = next
	a.selectRelated(ch, next)
}

// referenceMove walks the cursor without marking read, as Up and Down.
func (a *App) referenceMove(delta int) {
	rl := &a.references
	run := rl.activeRun()
	if run == nil || len(run.choices.locs) == 0 {
		return
	}
	ch := run.choices
	filter := strings.ToLower(strings.TrimSpace(rl.filter))
	for i := 1; i <= len(ch.locs); i++ {
		j := (rl.cursor + delta*i + len(ch.locs)) % len(ch.locs)
		if referenceMatches(ch, j, filter) {
			rl.cursor = j
			a.selectRelated(ch, j)
			return
		}
	}
}

// activeRun is the list the panel shows, or nil with none pinned yet.
func (rl *referenceList) activeRun() *referenceRun {
	if len(rl.runs) == 0 {
		return nil
	}
	return rl.runs[0]
}

// referenceShowRun restores a recent list, as its clock menu asks.
func (a *App) referenceShowRun(run *referenceRun) {
	if run == nil || a.referencesStale(run) {
		return
	}
	rl := &a.references
	for i, r := range rl.runs {
		if r == run {
			rl.runs = append([]*referenceRun{r}, append(slices.Clone(rl.runs[:i]), rl.runs[i+1:]...)...)
			break
		}
	}
	rl.filter, rl.cursor = "", -1
	rl.read = make(map[int]bool)
	rl.err, rl.noServer = nil, false
	a.referencesEmpty = false
}

// referencesView draws the pinned references panel: a filter field with
// the recents menu, a count, and the shared reference list of the active
// run, over which Space walks the rows as a reading queue.
func (a *App) referencesView(c *ui.Context) {
	rl := &a.references
	// A run whose document changed since it was asked is stale: its
	// positions no longer hold, so it is dropped, recents included.
	rl.runs = slices.DeleteFunc(rl.runs, a.referencesStale)
	run := rl.activeRun()
	pal := colors(c)
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Shrink(0).Padding(6, 12).Gap(8).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).Children(func() {
			clock := ui.ButtonBase(c).Label(locales.MsgRecentReferenceLists()).Tooltip(locales.MsgRecentReferenceLists()).Size(22, 22).Radius(6).Shrink(0).KeepFocus().Center()
			if clock.Hovered() {
				clock.Background(pal.Pill)
			}
			clock.Children(func() { symbolIcon(c, icClock, 13, pal.Muted) })
			clock.Menu(func(m *ui.Menu) {
				if len(rl.runs) == 0 {
					m.Item(locales.MsgNoRecentLists()).Disabled(true)
					return
				}
				for _, r := range rl.runs {
					item := m.Item(r.label())
					if r == run {
						item.Checked(true)
					}
					if item.Chosen() {
						a.referenceShowRun(r)
					}
				}
			})
			field := ui.SearchField(c, &rl.filter).Label(locales.MsgFilterReferences()).Placeholder(locales.MsgFilterByPathSymbolOrLineText()).FillWidth().Height(26).FontSize(12).Bind(&rl.field)
			if field.Changed() && run != nil {
				rl.cursor = -1
			}
			if rl.loading {
				ui.Spinner(c)
			}
			if run != nil {
				n, files := referenceCount(run.choices, rl.filter)
				ui.Text(c, locales.MsgReferencesCountDInDFiles(fmt.Sprint(n), fmt.Sprint(files))).FontSize(12).TextColor(pal.Muted).SingleLine().Shrink(0)
			}
		})
		switch {
		case rl.err != nil:
			ui.Row(c).FillWidth().Padding(6, 12).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, rl.err.Error()).FontSize(12).TextColor(pal.Err).Grow(1).Shrink(1)
				ui.Button(c, locales.MsgRetry()).Label(locales.MsgRetry()).OnClick(func() {
					rl.err = nil
					a.findUsagesPinned()
				})
			})
		case rl.noServer:
			ui.Text(c, locales.MsgNoLanguageServerIsConfiguredForThisFile()).FontSize(12).TextColor(pal.Muted).Padding(12)
		case rl.pending && a.trustAsk:
			ui.Column(c).FillWidth().Padding(8, 12).Gap(8).Children(func() {
				ui.Text(c, locales.TrustBannerLead(filepath.Base(a.root))).FontSize(12).FontWeight(600).FillWidth()
				ui.Text(c, locales.TrustBannerDetail()).FontSize(11).TextColor(pal.Muted).FillWidth()
				ui.Row(c).Gap(8).Children(func() {
					ui.Button(c, locales.TrustFolder()).Label(locales.TrustFolder()).OnClick(a.trust)
					ui.Button(c, locales.NotNow()).Label(locales.NotNow()).OnClick(func() {
						a.trustAsk, a.afterTrust, rl.pending = false, nil, false
						a.trustDismissed = true
					})
				})
			})
		case run == nil && a.referencesEmpty:
			ui.Column(c).Grow(1).FillWidth().Padding(12).Gap(4).Children(func() {
				ui.Text(c, locales.MsgNoReferencesFound()).FontSize(12)
				ui.Text(c, locales.MsgReferencesMayMissUnopened()).FontSize(11).TextColor(pal.Muted)
			})
		case run == nil:
			ui.Text(c, locales.MsgNoPinnedReferences()).FontSize(12).TextColor(pal.Muted).Padding(12)
		default:
			ch := run.choices
			box := ui.Column(c).Grow(1).FillWidth().Clip()
			box.Children(func() {
				ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
					ui.Column(c).Grow(2).FillHeight().Clip().Children(func() {
						if ch.previewEditor == nil {
							message := "Loading code preview…"
							if ch.buffers != nil {
								message = "Code preview unavailable"
							}
							ui.Text(c, message).FontSize(12).TextColor(pal.Muted).Padding(12)
						} else {
							editor.View(c.Key("reference-code"), ch.previewEditor).Grow(1).FillWidth()
						}
					})
					ui.Column(c).Grow(1).FillHeight().BorderWidth(0, 0, 0, 1).BorderColor(pal.Separator).Children(func() {
						ui.Scroll(c).Grow(1).FillWidth().Label(locales.MsgNavigationResults()).Children(func() {
							ui.Column(c).FillWidth().Children(func() {
								a.relatedList(c, ch, run.collapsed, rl.filter, rl.read,
									func(i int) { rl.cursor = i; a.selectRelated(ch, i) },
									func(i int) { a.jump(ch.g, ch.from, ch.locs[i], ch.enc) })
							})
						})
					})
				})
			})
			rl.focused = box.FocusWithin()
			box.OnShortcut(0, ui.KeySpace, a.referenceAdvance)
			box.OnShortcut(0, ui.KeyDown, func() { a.referenceMove(1) })
			box.OnShortcut(0, ui.KeyUp, func() { a.referenceMove(-1) })
			box.OnShortcut(0, ui.KeyEnter, func() {
				if rl.cursor >= 0 && rl.cursor < len(ch.locs) {
					a.jump(ch.g, ch.from, ch.locs[rl.cursor], ch.enc)
				}
			})
		}
	})
}

// referenceCount is the rows passing filter and the files they are in.
func referenceCount(ch *defChoices, filter string) (n, files int) {
	filter = strings.ToLower(strings.TrimSpace(filter))
	seen := map[string]bool{}
	for i, t := range ch.locs {
		if !referenceMatches(ch, i, filter) {
			continue
		}
		n++
		if !seen[t.path] {
			seen[t.path] = true
			files++
		}
	}
	return n, files
}

var icClock = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3 2"/></svg>`))
