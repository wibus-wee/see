package app

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/localhistory"
	"see/internal/workspace"
	"see/locales"
)

const (
	localHistoryDebounce   = 1500 * time.Millisecond
	localHistoryCheckpoint = 30 * time.Second
)

type localHistoryPanel struct {
	doc       *doc
	group     *group
	editor    *editor.State
	buffer    *editor.Buffer
	path      string
	version   uint64
	destStart int
	destEnd   int

	snapshots []localhistory.Snapshot
	selected  int
	list      ui.ListState
	preview   *editor.State
	loading   bool
	err       error
	seq       uint64
}

func localHistoryText(key string) string {
	return locales.Catalog.Format(i18n.Locale(), key, nil)
}

func (a *App) localHistoryDocumentEligible(d *doc) bool {
	return d != nil && !d.untitled && !d.lossy && !d.video && d.image == nil && d.kind == workspace.Text && d.buf != nil
}

func (a *App) localHistorySupported(d *doc) bool {
	if !a.localHistoryDocumentEligible(d) {
		return false
	}
	text := d.buf.String()
	return localHistoryTextSupported(text)
}

func localHistoryTextSupported(text string) bool {
	return len(text) <= localhistory.MaxTextBytes && utf8.ValidString(text) && !strings.ContainsRune(text, '\x00')
}

func (a *App) captureLoadedLocalHistory(d *doc, diskText string) {
	if d == nil || d.buf == nil {
		return
	}
	now := time.Now()
	current := d.buf.String()
	if a.localHistoryDocumentEligible(d) {
		if localHistoryTextSupported(diskText) {
			a.writeLocalHistory(d, diskText, now)
		}
		if current != diskText && localHistoryTextSupported(current) {
			a.writeLocalHistory(d, current, now.Add(time.Nanosecond))
		}
	}
	d.localHistoryVer = d.buf.Version()
	d.localHistorySaved = now
}

func (a *App) captureEditedLocalHistory(d *doc) {
	if d == nil || d.buf == nil {
		return
	}
	version := d.buf.Version()
	if version == d.localHistoryVer {
		return
	}
	d.localHistoryVer = version
	if !a.localHistoryDocumentEligible(d) {
		return
	}
	snapshot := d.buf.Snapshot()
	text := snapshot.String()
	if !localHistoryTextSupported(text) {
		return
	}
	now := time.Now()
	if d.localHistorySaved.IsZero() || now.Sub(d.localHistorySaved) >= localHistoryCheckpoint {
		if d.localHistoryTimer != nil {
			d.localHistoryTimer.Stop()
			d.localHistoryTimer = nil
		}
		d.localHistorySaved = now
		a.writeLocalHistory(d, text, now)
		return
	}
	if d.localHistoryTimer != nil {
		d.localHistoryTimer.Stop()
	}
	path := d.path
	d.localHistoryTimer = time.AfterFunc(localHistoryDebounce, func() {
		a.post.Post(func() {
			if a.docs[path] != d || d.buf == nil || d.path != path || d.localHistoryVer != version || d.buf.Version() != version {
				return
			}
			d.localHistoryTimer = nil
			d.localHistorySaved = time.Now()
			a.writeLocalHistory(d, text, now)
		})
	})
}

func (a *App) captureCurrentLocalHistory(d *doc) {
	if d == nil || d.buf == nil {
		return
	}
	if d.localHistoryTimer != nil {
		d.localHistoryTimer.Stop()
		d.localHistoryTimer = nil
	}
	d.localHistoryVer = d.buf.Version()
	d.localHistorySaved = time.Now()
	if !a.localHistoryDocumentEligible(d) {
		return
	}
	text := d.buf.Snapshot().String()
	if localHistoryTextSupported(text) {
		a.writeLocalHistory(d, text, d.localHistorySaved)
	}
}

func (a *App) writeLocalHistory(d *doc, text string, at time.Time) {
	if a.localHistoryStore == nil || d == nil || !a.localHistoryDocumentEligible(d) || !localHistoryTextSupported(text) {
		return
	}
	store, path, post := a.localHistoryStore, d.path, a.post.Post
	go func() {
		if _, _, err := store.Add(path, text, at); err != nil {
			post(func() { a.note(localHistoryText("localHistoryWriteError")+err.Error(), true) })
		}
	}()
}

func (a *App) showActiveLocalHistory() {
	if a.focus == nil {
		return
	}
	if d := a.docs[a.activePath()]; d != nil {
		a.showLocalHistory(a.focus, d)
	}
}

func (a *App) showLocalHistory(g *group, d *doc) {
	if g == nil || !a.localHistorySupported(d) {
		a.note(localHistoryText("localHistoryUnsupported"), true)
		return
	}
	st := g.eds[d.path]
	if st == nil || st.Buffer != d.buf {
		return
	}
	selection := st.Selection()
	p := &localHistoryPanel{
		doc:       d,
		group:     g,
		editor:    st,
		buffer:    d.buf,
		path:      d.path,
		version:   d.buf.Version(),
		destStart: selection.Start(),
		destEnd:   selection.End(),
		selected:  -1,
		loading:   true,
	}
	a.localHistoryPanel = p
	a.quick.open = false
	a.git.picker.open = false
	a.jumpDate.open = false
	a.codeActions = nil
	a.loadLocalHistory(p)
}

func (a *App) loadLocalHistory(p *localHistoryPanel) {
	if p == nil || a.localHistoryPanel != p {
		return
	}
	p.loading, p.err = true, nil
	p.seq++
	seq := p.seq
	if a.localHistoryStore == nil {
		p.loading, p.err = false, a.localHistoryErr
		return
	}
	store, path, post := a.localHistoryStore, p.path, a.post.Post
	go func() {
		snapshots, err := store.List(path)
		post(func() {
			if a.localHistoryPanel != p || p.seq != seq {
				return
			}
			p.loading, p.err = false, err
			p.snapshots = snapshots
			p.selected = -1
			p.preview = nil
			if err == nil && len(snapshots) > 0 {
				a.selectLocalHistory(p, 0)
			}
		})
	}()
}

func (a *App) selectLocalHistory(p *localHistoryPanel, index int) {
	if p == nil || index < 0 || index >= len(p.snapshots) {
		return
	}
	p.selected = index
	snap := p.snapshots[index]
	buf := editor.NewBuffer(snap.Text)
	buf.SetLanguage(languageFor(p.doc.name, snap.Text))
	p.preview = &editor.State{
		Buffer:      buf,
		ReadOnly:    true,
		LineNumbers: editor.LineNumbersOn,
		Label:       localHistoryText("localHistoryPreview") + " — " + p.doc.name,
		Theme:       a.edTheme,
		SoftWrap:    true,
	}
}

func (a *App) closeLocalHistory() {
	p := a.localHistoryPanel
	if p == nil {
		return
	}
	a.localHistoryPanel = nil
	if p.doc != nil && a.docs[p.path] == p.doc && p.group != nil && p.group.eds[p.path] == p.editor {
		a.focus = p.group
		a.focusEditor = p.editor
	}
}

func (a *App) localHistoryTargetCurrent(p *localHistoryPanel) bool {
	return p != nil && p.doc != nil && p.group != nil && p.editor != nil && p.buffer != nil &&
		a.docs[p.path] == p.doc && p.doc.path == p.path && p.doc.buf == p.buffer &&
		p.group.hasDocument(p.path) && p.group.eds[p.path] == p.editor &&
		p.editor.Buffer == p.buffer && p.buffer.Version() == p.version
}

func replaceLocalHistorySelection(buf *editor.Buffer, version uint64, start, end int, text string) bool {
	if buf == nil || buf.Version() != version || start < 0 || end < start || end > buf.Len() {
		return false
	}
	buf.Transact(func() { buf.Edit(editor.Edit{Start: start, End: end, Text: text}) })
	return true
}

func (a *App) restoreLocalHistory() {
	p := a.localHistoryPanel
	if p == nil || p.preview == nil || p.selected < 0 || p.selected >= len(p.snapshots) {
		return
	}
	if !a.localHistoryTargetCurrent(p) {
		return
	}
	selection := p.preview.Selection()
	start, end := selection.Start(), selection.End()
	if start == end {
		return
	}
	text := p.preview.Buffer.Snapshot().String()[start:end]
	before := p.buffer.Snapshot().String()
	now := time.Now()
	a.writeLocalHistory(p.doc, before, now)
	if !replaceLocalHistorySelection(p.buffer, p.version, p.destStart, p.destEnd, text) {
		return
	}
	p.doc.localHistoryVer = p.buffer.Version()
	p.doc.localHistorySaved = now.Add(time.Nanosecond)
	a.writeLocalHistory(p.doc, p.buffer.Snapshot().String(), now.Add(time.Nanosecond))
	p.editor.GoTo(p.buffer.Snapshot().Pos(p.destStart + len(text)))
	name := p.doc.name
	a.closeLocalHistory()
	a.note(localHistoryText("localHistoryRestored")+name, false)
}

func localHistoryLineCount(text string) int {
	if text == "" {
		return 0
	}
	lines := strings.Count(text, "\n")
	if !strings.HasSuffix(text, "\n") {
		lines++
	}
	return max(1, lines)
}

func (a *App) localHistoryPanelView(c *ui.Context) {
	p := a.localHistoryPanel
	if p == nil {
		return
	}
	ui.Overlay(c, func() {
		backdrop := ui.Column(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).AlignItems(ui.Center).Justify(ui.Center).Background(ui.RGBA(0, 0, 0, 0.3))
		backdrop.OnClick(a.closeLocalHistory)
		backdrop.Children(func() {
			panel := ui.Column(c.Key("local-history-panel")).Focusable().AutoFocus().Width(1060).MaxWidthPercent(96).Height(640).MaxHeightPercent(94).
				Padding(18).Gap(0).Radius(16).Background(colors(c).Editor).Border(1, colors(c).PopoverEdge).
				Shadow(0, 22, 60, 0, ui.RGBA(0, 0, 0, 0.45))
			panel.OnClick(func() {})
			panel.Children(func() {
				a.localHistoryHeader(c, p)
				ui.Box(c).Height(1).FillWidth().Background(colors(c).Separator).Margin(14, 0)
				ui.Row(c).Grow(1).FillWidth().Gap(14).AlignItems(ui.Stretch).Children(func() {
					a.localHistoryVersions(c, p)
					a.localHistoryPreview(c, p)
				})
				ui.Box(c).Height(1).FillWidth().Background(colors(c).Separator).Margin(14, 0)
				a.localHistoryFooter(c, p)
			})
		})
	})
}

func (a *App) localHistoryHeader(c *ui.Context, p *localHistoryPanel) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Column(c).Grow(1).Gap(3).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				symbolIcon(c, icClock, 19, c.Theme().Accent)
				ui.Text(c, localHistoryText("localHistoryTitle")).FontSize(20).Bold()
				ui.Text(c, "·").TextColor(colors(c).Dim)
				ui.Text(c, p.doc.name).FontSize(13).TextColor(colors(c).Muted).SingleLine().Ellipsis("…")
			})
			ui.Text(c, localHistoryText("localHistoryLocalOnly")).FontSize(12).TextColor(colors(c).Muted)
		})
		if ui.Button(c.Key("local-history-close"), localHistoryText("localHistoryClose")).Height(30).Clicked() {
			a.closeLocalHistory()
		}
	})
}

func (a *App) localHistoryVersions(c *ui.Context, p *localHistoryPanel) {
	ui.Column(c).Width(250).Shrink(0).Gap(8).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, localHistoryText("localHistoryVersions")).FontSize(12).Bold().TextColor(colors(c).Muted)
			ui.Box(c).Grow(1)
			if len(p.snapshots) > 0 {
				ui.Text(c, fmt.Sprint(len(p.snapshots))).FontSize(11).TextColor(colors(c).Dim)
			}
		})
		switch {
		case p.loading:
			ui.Row(c).Padding(12).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Text(c, localHistoryText("localHistoryLoading")).FontSize(12).TextColor(colors(c).Muted)
			})
		case p.err != nil:
			ui.Column(c).Padding(12).Gap(10).Children(func() {
				ui.Text(c, localHistoryText("localHistoryLoadError")+p.err.Error()).FontSize(12).TextColor(colors(c).Err)
				ui.Button(c, localHistoryText("localHistoryRetry")).OnClick(func() { a.loadLocalHistory(p) })
			})
		case len(p.snapshots) == 0:
			ui.Column(c).Padding(12).Gap(8).Children(func() {
				ui.Text(c, localHistoryText("localHistoryNoSnapshots")).FontSize(13).Bold()
				ui.Text(c, localHistoryText("localHistoryNoSnapshotsDetail")).FontSize(11).TextColor(colors(c).Muted)
			})
		default:
			p.list.Selected = &p.selected
			list := ui.List(c.Key("local-history-snapshots"), &p.list, len(p.snapshots)).ItemKey(func(i int) any { return p.snapshots[i].ID }).Grow(1).FillWidth().Label(localHistoryText("localHistoryVersions"))
			list.Rows(func(item ui.ListRow) {
				snap := p.snapshots[item.Index]
				row := ui.Column(item.Context.Key(snap.ID)).FillWidth().Padding(10).Gap(4).Radius(8)
				selected := item.Selected()
				lineColor := colors(c).Muted
				if selected {
					row.Background(colors(c).SelectedRow)
					lineColor = colors(c).Text
				}
				row.Children(func() {
					ui.Text(item.Context, snap.CreatedAt.Local().Format("2006-01-02 15:04:05")).FontSize(12).Bold().TextColor(colors(c).Text).SingleLine()
					lines := localHistoryLineCount(snap.Text)
					ui.Text(item.Context, locales.Catalog.Format(i18n.Locale(), "localHistoryLines", map[string]any{"lines": lines})).FontSize(11).TextColor(lineColor).SingleLine()
				})
			})
			if list.Changed() && p.selected >= 0 && p.selected < len(p.snapshots) {
				a.selectLocalHistory(p, p.selected)
			}
		}
	})
}

func (a *App) localHistoryPreview(c *ui.Context, p *localHistoryPanel) {
	ui.Column(c).Grow(1).Fill().MinWidth(0).Gap(8).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, localHistoryText("localHistoryPreview")).FontSize(12).Bold().TextColor(colors(c).Muted)
			ui.Box(c).Grow(1)
			if p.selected >= 0 && p.selected < len(p.snapshots) {
				ui.Text(c, p.snapshots[p.selected].CreatedAt.Local().Format("2006-01-02 15:04")).FontSize(11).TextColor(colors(c).Dim)
			}
		})
		ui.Text(c, localHistoryText("localHistorySelectionHint")).FontSize(11).TextColor(colors(c).Muted)
		if p.preview != nil {
			ui.Box(c).Grow(1).FillWidth().MinHeight(0).Radius(8).Border(1, colors(c).Separator).Background(colors(c).Editor).Children(func() {
				editor.View(c.Key("local-history-code-preview"), p.preview).Grow(1).Fill().AutoFocus()
			})
		} else {
			ui.Column(c).Grow(1).FillWidth().Center().Children(func() {
				ui.Text(c, localHistoryText("localHistorySelectVersion")).FontSize(13).TextColor(colors(c).Muted)
			})
		}
	})
}

func (a *App) localHistoryFooter(c *ui.Context, p *localHistoryPanel) {
	selectionText := ""
	canRestore := false
	buttonLabel := localHistoryText("localHistorySelectSource")
	if p.preview != nil {
		selection := p.preview.Selection()
		start, end := selection.Start(), selection.End()
		if start != end {
			text := p.preview.Buffer.Snapshot().String()[start:end]
			selectionText = locales.Catalog.Format(i18n.Locale(), "localHistorySelectionCount", map[string]any{
				"start":      p.preview.Buffer.Pos(start).Line + 1,
				"end":        p.preview.Buffer.Pos(end).Line + 1,
				"lines":      localHistoryLineCount(text),
				"characters": utf8.RuneCountInString(text),
			})
			canRestore = a.localHistoryTargetCurrent(p)
			if p.destStart == p.destEnd {
				line := p.buffer.Snapshot().Pos(p.destStart).Line + 1
				buttonLabel = locales.Catalog.Format(i18n.Locale(), "localHistoryInsertAtLine", map[string]any{"line": line})
			} else {
				buttonLabel = localHistoryText("localHistoryReplaceSelection")
			}
		}
	}
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Column(c).Grow(1).Gap(4).Children(func() {
			if selectionText != "" {
				ui.Text(c, selectionText).FontSize(12).Bold()
			} else {
				ui.Text(c, localHistoryText("localHistorySelectSource")).FontSize(12).TextColor(colors(c).Muted)
			}
			if !a.localHistoryTargetCurrent(p) {
				ui.Text(c, localHistoryText("localHistoryStale")).FontSize(11).TextColor(colors(c).Err)
			} else {
				ui.Text(c, localHistoryText("localHistoryDestinationHint")).FontSize(11).TextColor(colors(c).Muted)
			}
		})
		ui.PrimaryButton(c.Key("local-history-restore"), buttonLabel).Height(34).MinWidth(210).Disabled(!canRestore || p.loading || p.err != nil).OnClick(a.restoreLocalHistory)
	})
}
