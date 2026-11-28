package app

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/localhistory"
)

const localHistoryDiffPrefix = diffTabPrefix + "local-history:"

type localHistoryView struct {
	path        string
	name        string
	doc         *doc
	buffer      *editor.Buffer
	version     uint64
	currentText string
	snapshots   []localhistory.Snapshot
	selected    int
	state       *editor.DiffState
	loading     bool
	loadErr     error
	targetErr   string
	pickerOpen  bool
	seq         uint64
	restored    bool
}

func localHistoryTab(path string) string { return localHistoryDiffPrefix + path }

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
		a.fail(localHistoryText("localHistoryUnsupported"))
		return
	}
	if st := g.eds[d.path]; st == nil || st.Buffer != d.buf {
		return
	}
	key := localHistoryTab(d.path)
	if v := g.diffs[key]; v != nil && v.localHistory != nil {
		a.activateIn(g, key)
		a.refreshLocalHistory(g, key, v)
		return
	}
	if g.diffs == nil {
		g.diffs = map[string]*gitDiffTab{}
	}
	lh := &localHistoryView{path: d.path, name: d.name, doc: d, buffer: d.buf, version: d.buf.Version(), currentText: d.buf.Snapshot().String(), selected: -1, loading: true}
	v := &gitDiffTab{name: d.name, localHistory: lh}
	g.diffs[key] = v
	g.addTab(key, len(g.order))
	a.activateIn(g, key)
	a.loadLocalHistory(v, "")
}

func (a *App) loadLocalHistory(v *gitDiffTab, keepID string) {
	lh := v.localHistory
	lh.seq++
	seq := lh.seq
	lh.loading, lh.loadErr = true, nil
	if a.localHistoryStore == nil {
		lh.loading, lh.loadErr = false, a.localHistoryErr
		return
	}
	store, path, post := a.localHistoryStore, lh.path, a.post.Post
	go func() {
		snapshots, err := store.List(path)
		post(func() {
			if v.localHistory != lh || lh.seq != seq || !a.hasLocalHistoryView(lh) {
				return
			}
			lh.loading, lh.loadErr = false, err
			if err != nil {
				return
			}
			lh.snapshots = snapshots
			lh.selected = -1
			if len(snapshots) == 0 {
				lh.state, v.state = nil, nil
				return
			}
			index := 0
			if keepID != "" {
				for i, snap := range snapshots {
					if snap.ID == keepID {
						index = i
						break
					}
				}
			}
			a.selectLocalHistorySnapshot(v, index)
		})
	}()
}

func (a *App) hasLocalHistoryView(lh *localHistoryView) bool {
	if lh == nil {
		return false
	}
	key := localHistoryTab(lh.path)
	for _, g := range a.groups() {
		if v := g.diffs[key]; v != nil && v.localHistory == lh {
			return true
		}
	}
	return false
}

func (a *App) selectLocalHistorySnapshot(v *gitDiffTab, index int) {
	if v == nil || v.localHistory == nil || index < 0 || index >= len(v.localHistory.snapshots) {
		return
	}
	lh := v.localHistory
	lh.selected = index
	snapshot := lh.snapshots[index]
	if snapshot.Corrupt {
		// Corrupt snapshots never become a diff: there is no trustworthy
		// content to show, and a nil state leaves restore with nothing
		// to apply.
		lh.state, v.state = nil, nil
		return
	}
	old := editor.NewBuffer(snapshot.Text)
	current := editor.NewBuffer(lh.currentText)
	lang := languageFor(lh.name, snapshot.Text)
	old.SetLanguage(lang)
	current.SetLanguage(lang)
	state := &editor.DiffState{Theme: a.edTheme, LineSelection: true}
	setBufferDiffWhitespace(state, old, current, a.cfg.HideWhitespaceChanges)
	a.applyDiffPrefs(state)
	state.LineSelection = !a.cfg.HideWhitespaceChanges
	state.GoToHunk(0)
	lh.state, v.state = state, state
}

func (a *App) currentLocalHistoryDoc(lh *localHistoryView) *doc {
	if lh == nil {
		return nil
	}
	d := a.docs[lh.path]
	if d == nil || d.loading || !a.localHistorySupported(d) {
		return nil
	}
	return d
}

func (a *App) localHistoryTargetCurrent(lh *localHistoryView) bool {
	d := a.currentLocalHistoryDoc(lh)
	return d != nil && d == lh.doc && d.buf == lh.buffer && d.buf.Version() == lh.version
}

func (a *App) refreshLocalHistory(g *group, key string, v *gitDiffTab) {
	if v == nil || v.localHistory == nil {
		return
	}
	lh := v.localHistory
	d := a.currentLocalHistoryDoc(lh)
	if d == nil {
		lh.targetErr = localHistoryText("localHistoryTargetClosed")
		return
	}
	keepID := ""
	if lh.selected >= 0 && lh.selected < len(lh.snapshots) {
		keepID = lh.snapshots[lh.selected].ID
	}
	lh.doc, lh.buffer = d, d.buf
	lh.version, lh.currentText = d.buf.Version(), d.buf.Snapshot().String()
	lh.targetErr = ""
	a.loadLocalHistory(v, keepID)
}

func (a *App) restoreLocalHistorySelected(g *group, key string, v *gitDiffTab) {
	if v == nil || v.localHistory == nil {
		return
	}
	lh := v.localHistory
	if lh.loading || lh.loadErr != nil || lh.targetErr != "" || !a.localHistoryTargetCurrent(lh) {
		return
	}
	if lh.selected >= 0 && lh.selected < len(lh.snapshots) && lh.snapshots[lh.selected].Corrupt {
		// Integrity failures are final: no edit derived from corrupt
		// bytes may reach the buffer, whatever the diff state says.
		return
	}
	edits := localHistorySelectedEdits(lh.state)
	if len(edits) == 0 {
		return
	}
	before := lh.buffer.Snapshot().String()
	now := time.Now()
	a.writeLocalHistory(lh.doc, before, now)
	if !applyLocalHistoryEdits(lh.buffer, lh.version, edits) {
		return
	}
	lh.doc.localHistoryVer = lh.buffer.Version()
	lh.doc.localHistorySaved = now.Add(time.Nanosecond)
	a.writeLocalHistory(lh.doc, lh.buffer.Snapshot().String(), lh.doc.localHistorySaved)
	lh.version, lh.currentText = lh.buffer.Version(), lh.buffer.Snapshot().String()
	a.selectLocalHistorySnapshot(v, lh.selected)
	lh.restored = true
	a.flash(localHistoryText("localHistoryRestored"))
}

func applyLocalHistoryEdits(buffer *editor.Buffer, version uint64, edits []editor.Edit) bool {
	if buffer == nil || buffer.Version() != version || len(edits) == 0 {
		return false
	}
	sort.SliceStable(edits, func(i, j int) bool {
		if edits[i].Start == edits[j].Start {
			return edits[i].End > edits[j].End
		}
		return edits[i].Start > edits[j].Start
	})
	buffer.Transact(func() {
		for _, edit := range edits {
			buffer.Edit(edit)
		}
	})
	return true
}

func (a *App) cloneLocalHistoryTab(v *gitDiffTab) *gitDiffTab {
	if v == nil || v.localHistory == nil {
		return nil
	}
	source := v.localHistory
	clone := *source
	clone.snapshots = append([]localhistory.Snapshot(nil), source.snapshots...)
	clone.state = nil
	clone.pickerOpen = false
	clone.loading = false
	clone.loadErr = nil
	copy := &gitDiffTab{name: v.name, localHistory: &clone}
	if source.selected >= 0 {
		a.selectLocalHistorySnapshot(copy, source.selected)
	}
	return copy
}

func localHistoryLineCount(text string) int {
	if text == "" {
		return 0
	}
	count := strings.Count(text, "\n")
	if !strings.HasSuffix(text, "\n") {
		count++
	}
	return count
}

type localHistoryLineSpan struct {
	start int
	end   int
	text  string
}

func localHistoryLineSpans(text string) []localHistoryLineSpan {
	var lines []localHistoryLineSpan
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			lines = append(lines, localHistoryLineSpan{start: start, end: i + 1, text: text[start : i+1]})
			start = i + 1
		}
	}
	if start < len(text) {
		lines = append(lines, localHistoryLineSpan{start: start, end: len(text), text: text[start:]})
	}
	return lines
}

func localHistorySelectedEdits(state *editor.DiffState) []editor.Edit {
	if state == nil {
		return nil
	}
	selection, ok := state.SelectedLines()
	if !ok || selection.End < selection.Start {
		return nil
	}
	oldText, newText := state.Old.Snapshot().String(), state.New.Snapshot().String()
	oldLines, newLines := localHistoryLineSpans(oldText), localHistoryLineSpans(newText)
	var edits []editor.Edit
	for _, hunk := range state.Hunks() {
		oldCount, newCount := hunk.OldEnd-hunk.OldStart, hunk.NewEnd-hunk.NewStart
		paired := min(oldCount, newCount)
		if selection.Side == editor.DiffOld {
			var pairedEdits []editor.Edit
			for offset := 0; offset < paired; offset++ {
				oldLine, newLine := hunk.OldStart+offset, hunk.NewStart+offset
				if oldLine < selection.Start || oldLine > selection.End || oldLine >= len(oldLines) || newLine >= len(newLines) {
					continue
				}
				text := oldLines[oldLine].text
				if !strings.HasSuffix(text, "\n") && newLines[newLine].end < len(newText) {
					text += "\n"
				}
				pairedEdits = append(pairedEdits, editor.Edit{Start: newLines[newLine].start, End: newLines[newLine].end, Text: text})
			}
			edits = append(edits, pairedEdits...)
			var tail strings.Builder
			for oldLine := hunk.OldStart + paired; oldLine < hunk.OldEnd; oldLine++ {
				if oldLine >= selection.Start && oldLine <= selection.End && oldLine >= 0 && oldLine < len(oldLines) {
					tail.WriteString(oldLines[oldLine].text)
				}
			}
			if tail.Len() > 0 {
				at := len(newText)
				if hunk.NewEnd < len(newLines) {
					at = newLines[hunk.NewEnd].start
				}
				text := tail.String()
				if at < len(newText) && !strings.HasSuffix(text, "\n") {
					text += "\n"
				} else if at == len(newText) && at > 0 && !strings.HasPrefix(text, "\n") {
					preceding := newText[:at]
					for i := len(pairedEdits) - 1; i >= 0; i-- {
						edit := pairedEdits[i]
						preceding = preceding[:edit.Start] + edit.Text + preceding[edit.End:]
					}
					if !strings.HasSuffix(preceding, "\n") {
						text = "\n" + text
					}
				}
				edits = append(edits, editor.Edit{Start: at, End: at, Text: text})
			}
		} else if selection.Side == editor.DiffNew {
			first, last := max(hunk.NewStart, selection.Start), min(hunk.NewEnd, selection.End+1)
			if first >= last || first < 0 || last > len(newLines) {
				continue
			}
			var replacement strings.Builder
			for newLine := first; newLine < last; newLine++ {
				oldOffset := newLine - hunk.NewStart
				if oldOffset >= paired {
					continue
				}
				oldLine := hunk.OldStart + oldOffset
				if oldLine < 0 || oldLine >= len(oldLines) {
					continue
				}
				text := oldLines[oldLine].text
				if !strings.HasSuffix(text, "\n") && newLines[newLine].end < len(newText) {
					text += "\n"
				}
				replacement.WriteString(text)
			}
			start, end := newLines[first].start, newLines[last-1].end
			text := replacement.String()
			reachesEOF := last == len(newLines) && end == len(newText)
			if reachesEOF {
				wantFinalNewline := strings.HasSuffix(oldText, "\n")
				prefix := newText[:start]
				if wantFinalNewline && !strings.HasSuffix(prefix+text, "\n") {
					text += "\n"
				} else if !wantFinalNewline && strings.HasSuffix(prefix+text, "\n") {
					if strings.HasSuffix(text, "\n") {
						text = strings.TrimSuffix(text, "\n")
					} else if start > 0 && strings.HasSuffix(prefix, "\n") {
						start--
					}
				}
			} else if end < len(newText) && text != "" && !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
			edits = append(edits, editor.Edit{Start: start, End: end, Text: text})
		}
	}
	return edits
}

func (a *App) localHistoryContent(c *ui.Context, g *group, v *gitDiffTab) {
	lh := v.localHistory
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Height(32).Padding(0, 12).Gap(8).AlignItems(ui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(colors(c).Separator).Children(func() {
			symbolIcon(c, icClock, 14, c.Theme().Accent)
			ui.Text(c, localHistoryText("localHistoryTab")).FontSize(12).Bold()
			ui.Text(c, "·").TextColor(colors(c).Dim)
			ui.Text(c, filepath.Base(lh.path)).FontSize(12).TextColor(colors(c).Muted).SingleLine().Ellipsis("…")
			ui.Box(c).Grow(1)
			ui.Text(c, localHistoryText("localHistoryLocalOnly")).FontSize(11).TextColor(colors(c).Muted).SingleLine()
		})
		switch {
		case lh.loading:
			ui.Row(c).Grow(1).FillWidth().Center().Gap(8).Children(func() {
				ui.Spinner(c)
				ui.Text(c, localHistoryText("localHistoryLoading")).FontSize(13).TextColor(colors(c).Muted)
			})
		case lh.loadErr != nil:
			ui.Column(c).Grow(1).FillWidth().Center().Gap(8).Children(func() {
				ui.Text(c, localHistoryText("localHistoryLoadError")+lh.loadErr.Error()).FontSize(13).TextColor(colors(c).Err)
				if ui.Button(c, localHistoryText("localHistoryRetry")).Clicked() {
					a.refreshLocalHistory(g, g.active, v)
				}
			})
		case len(lh.snapshots) == 0:
			ui.Column(c).Grow(1).FillWidth().Center().Gap(8).Children(func() {
				ui.Text(c, localHistoryText("localHistoryNoSnapshots")).FontSize(15).TextColor(colors(c).Muted)
				ui.Text(c, localHistoryText("localHistoryNoSnapshotsDetail")).FontSize(12).TextColor(colors(c).Dim)
			})
		default:
			if lh.state != nil && a.diffChangesHidden(lh.state) {
				a.hiddenWhitespaceDiff(c)
			} else if lh.state != nil {
				view := editor.DiffView(c, lh.state).Grow(1).FillWidth()
				if a.focusGroupTarget == g {
					view.Focus()
				}
				if a.focusGroupTarget == nil && view.FocusWithin() {
					a.observeFocusedGroup(g)
				}
			} else if lh.selected >= 0 && lh.selected < len(lh.snapshots) && lh.snapshots[lh.selected].Corrupt {
				ui.Column(c).Grow(1).FillWidth().Center().Gap(8).Children(func() {
					ui.Text(c, localHistoryText("localHistoryCorrupt")).FontSize(13).TextColor(colors(c).Err)
					ui.Text(c, localHistoryText("localHistoryCorruptDetail")).FontSize(12).TextColor(colors(c).Dim)
				})
			} else {
				ui.Box(c).Grow(1)
			}
		}
		a.localHistoryStatusBar(c, g, v)
	})
}

func (a *App) localHistoryStatusBar(c *ui.Context, g *group, v *gitDiffTab) {
	lh := v.localHistory
	palette := colors(c)
	edits := localHistorySelectedEdits(lh.state)
	ui.Row(c).FillWidth().Height(30).Padding(0, 8).Gap(7).AlignItems(ui.Center).
		BorderWidth(1, 0, 0, 0).BorderColor(palette.Separator).Background(palette.Editor).Children(func() {
		selectedLabel := localHistoryText("localHistoryChooseSnapshot")
		if lh.selected >= 0 && lh.selected < len(lh.snapshots) {
			selectedLabel = lh.snapshots[lh.selected].CreatedAt.Local().Format("2006-01-02 15:04:05")
		}
		picker := ui.ButtonBase(c.Key("local-history-snapshot")).Label(selectedLabel).Tooltip(localHistoryText("localHistoryChooseSnapshot")).
			Height(22).Padding(0, 8).Radius(11).KeepFocus().Disabled(lh.loading || len(lh.snapshots) == 0)
		picker.Background(palette.WarnBg)
		picker.Children(func() {
			ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
				ui.Text(c, selectedLabel).FontSize(11).TextColor(palette.Text).SingleLine().Ellipsis("…")
				symbolIcon(c, icChevron, 9, palette.Muted)
			})
		})
		picker.PopoverMenuButton(func(menu *ui.Menu) {
			for i, snapshot := range lh.snapshots {
				label := snapshot.CreatedAt.Local().Format("2006-01-02 15:04:05")
				if snapshot.Corrupt {
					label += "  ·  " + localHistoryText("localHistoryCorruptTag")
				} else {
					lines := localHistoryFormat("localHistorySnapshotLines", map[string]any{"lines": localHistoryLineCount(snapshot.Text)})
					label = fmt.Sprintf("%s  ·  %s", label, lines)
				}
				if menu.Item(label).Chosen() {
					a.selectLocalHistorySnapshot(v, i)
				}
			}
		}, ui.PopoverMenuButtonOptions{Open: &lh.pickerOpen, Panel: popoverMenuPanel})
		ui.Text(c, "→").FontSize(11).TextColor(palette.Dim).SingleLine()
		target := localHistoryText("localHistoryWorkingTree")
		if lh.doc != nil && lh.buffer != nil && lh.buffer.Modified() {
			target = localHistoryText("localHistoryCurrentUnsaved")
		}
		ui.Text(c, target).FontSize(11).TextColor(palette.Text).SingleLine()
		ui.Box(c).Grow(1)
		if lh.state != nil {
			added, deleted := lh.state.Stats()
			ui.Text(c, fmt.Sprintf("+%d", added)).FontSize(11).TextColor(a.gitStatusColor('A')).SingleLine()
			ui.Text(c, fmt.Sprintf("−%d", deleted)).FontSize(11).TextColor(a.gitStatusColor('D')).SingleLine()
		}
		fresh := a.localHistoryTargetCurrent(lh)
		if !fresh {
			message := lh.targetErr
			if message == "" {
				message = localHistoryText("localHistoryStale")
			}
			ui.Text(c, message).FontSize(11).TextColor(palette.Err).SingleLine().Ellipsis("…")
		} else if len(edits) == 0 && lh.state != nil && !a.diffChangesHidden(lh.state) {
			ui.Text(c, localHistoryText("localHistorySelectChangedLines")).FontSize(11).TextColor(palette.Muted).SingleLine()
		}
		if ui.Button(c, localHistoryText("localHistoryRefresh")).Disabled(lh.loading).Clicked() {
			a.refreshLocalHistory(g, g.active, v)
		}
		canRestore := fresh && !lh.loading && lh.loadErr == nil && lh.targetErr == "" && len(edits) > 0
		if ui.Button(c, localHistoryText("localHistoryRestoreSelected")).
			Tooltip(localHistoryText("localHistorySelectChangedLines")).Disabled(!canRestore).Clicked() {
			a.restoreLocalHistorySelected(g, g.active, v)
		}
		if lh.restored && ui.Button(c, localHistoryText("localHistoryOpenFile")).Clicked() {
			if g.has(lh.path) {
				a.activateIn(g, lh.path)
			} else {
				a.openIn(g, lh.path, goTo{})
			}
		}
	})
}
