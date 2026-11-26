package app

import (
	"path/filepath"
	"regexp"
	"strings"

	"see/internal/git"
	"see/internal/workspace"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

// The right-side pane is an inspector like Xcode's: a segmented strip of
// tabs at its top picks the page — file details, the file's history, or
// the Quick Help of the caret's symbol — and the divider and its width
// are shared by all of them.

// inspectorFileHistory is the History page's commits of the file the
// focused editor shows, read once per path.
type inspectorFileHistory struct {
	path    string // relative to git.root
	head    string // HEAD when the page was read
	commits []git.Commit
	loading bool
	err     string
	seq     uint64
	scroll  ui.ScrollState
}

const inspectorHistoryPage = 50

// inspectorTab is the page the strip selects, kept over restarts.
func (a *App) inspectorTab() string {
	switch a.cfg.InspectorTab {
	case "file", "history":
		return a.cfg.InspectorTab
	}
	return "quickhelp"
}

// setInspectorTab switches the pane's page, as clicking one of Xcode's
// inspector icons does.
func (a *App) setInspectorTab(tab string) {
	if a.cfg.InspectorTab == tab {
		return
	}
	a.cfg.InspectorTab = tab
	a.saveConfigSoon()
}

// inspectorTabIdx is the strip's index of the page's name.
func (a *App) inspectorTabIdx() int {
	switch a.cfg.InspectorTab {
	case "file":
		return 0
	case "history":
		return 1
	}
	return 2
}

// setInspectorTabIdx persists the strip's selection as its page's name.
func (a *App) setInspectorTabIdx(i int) {
	switch i {
	case 0:
		a.setInspectorTab("file")
	case 1:
		a.setInspectorTab("history")
	default:
		a.setInspectorTab("quickhelp")
	}
}

// inspectorTabs switches the pane's pages, the same segmented control
// the navigator bar uses, over a thumb that slides to the choice.
func (a *App) inspectorTabs(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).FillWidth().Padding(8, 10, 10, 10).Children(func() {
		// The pane is narrow, so the three segments share the track
		// equally; the thumb's pitch is the share the layout gives them.
		const segGap = 2
		// Row padding 10+10, track padding 3+3, two gaps between segments.
		segW := (a.inspectorW() - 26 - 2*segGap) / 3
		in := &a.inspector
		in.tab = a.inspectorTabIdx()
		parts := ui.SegmentedBase(c, &in.tab, 3)
		parts.Track.FillWidth().Height(30).Padding(3).Gap(segGap).Radius(15).
			Background(t.Text.Alpha(0.06)).Label(inspectorText("inspectorTabs"))
		if c.Preferences().HighContrast {
			parts.Track.Border(1, t.Border)
		}
		segments := []struct {
			icon  *ui.SVG
			short string
			label string
		}{
			{icPage, inspectorText("inspectorFileShort"), inspectorText("inspectorFileTip")},
			{icClock, inspectorText("inspectorHistoryShort"), inspectorText("inspectorHistoryTip")},
			{icQuestion, inspectorText("inspectorQuickHelpShort"), inspectorText("inspectorQuickHelpTip")},
		}
		at := parts.Track.AnimateTo("thumb", float32(in.tab), thumbSpring)
		parts.Track.Children(func() {
			ui.Box(c).Width(segW).Height(24).Radius(12).Shrink(0).MarginEnd(-(segW + segGap)).Start(at * (segW + segGap)).
				Material(glass.Glass{Interactive: true}).PassThrough()
			for i, s := range segments {
				i, s := i, s
				seg := parts.Segment(i).Grow(1).Height(24).Radius(12).Label(s.label).Tooltip(s.label)
				seg.Children(func() {
					color := colors(c).Muted
					if i == in.tab {
						color = colors(c).Text
					}
					ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
						symbolIcon(c, s.icon, 13, color)
						ui.Text(c, s.short).FontSize(10).TextColor(color).SingleLine()
					})
				})
			}
		})
		if parts.Track.Changed() {
			a.setInspectorTabIdx(in.tab)
		}
	})
}

// inspectorPlaceholder is a page's empty state, as inspectorEmpty is for
// the Quick Help page but with the icon of the page's own.
func (a *App) inspectorPlaceholder(c *ui.Context, icon *ui.SVG, text string) {
	ui.Column(c).FillWidth().Grow(1).Center().Padding(0, 20).Gap(8).Children(func() {
		symbolIcon(c, icon, 32, colors(c).Dim)
		ui.Text(c, text).FontSize(12).TextColor(colors(c).Muted).TextAlign(ui.Center).FillWidth()
	})
}

// inspectorFileDoc is the document the File and History pages are about:
// the focused editor's, as Xcode's inspectors show the active file.
func (a *App) inspectorFileDoc() *doc {
	g := a.focus
	if g == nil {
		return nil
	}
	if v := g.diffs[g.active]; v != nil {
		if v.revision != nil {
			return a.docs[v.revision.path]
		}
		if v.localHistory != nil {
			return a.docs[v.localHistory.path]
		}
	}
	return a.docs[tabPath(g.active)]
}

// inspectorFilePage is Xcode's File inspector: what the file is, where
// it is, and how it is read — name, type, size and modified, the path
// that opens the file manager, encoding and line endings, and the file's
// source control status.
func (a *App) inspectorFilePage(c *ui.Context) {
	d := a.inspectorFileDoc()
	if d == nil || d.untitled || strings.HasPrefix(d.path, diffTabPrefix) {
		a.inspectorPlaceholder(c, icPage, inspectorText("inspectorNoFile"))
		return
	}
	ui.Scroll(c).Grow(1).FillWidth().TrackScroll(&a.inspector.fileScroll).
		Label(inspectorText("inspectorFilePane")).Children(func() {
		ui.Column(c).FillWidth().Padding(6, 6, 14, 6).Gap(20).Children(func() {
			inspectorSection(c, inspectorText("inspectorIdentity"), func() {
				row := ui.Row(c).FillWidth().Padding(10).Gap(10).AlignItems(ui.Center)
				row.Children(func() {
					fileIcon(c, d.name, 30)
					ui.Column(c).Shrink(1).Gap(1).Children(func() {
						ui.Text(c, d.name).FontSize(13).FontWeight(600).SingleLine().Ellipsis("…")
						ui.Text(c, inspectorFileType(d)).FontSize(11).TextColor(colors(c).Muted).SingleLine()
					})
				})
			})
			inspectorSection(c, inspectorText("inspectorInfo"), func() {
				inspectorField(c, inspectorText("inspectorSize"), size(d.size))
				if !d.stamp.modTime.IsZero() {
					inspectorHairline(c)
					inspectorField(c, inspectorText("inspectorModified"), d.stamp.modTime.Local().Format("Jan 2, 2006 15:04"))
				}
			})
			inspectorSection(c, inspectorText("inspectorLocation"), func() {
				loc := filepath.ToSlash(filepath.Dir(d.path))
				if rel, err := filepath.Rel(a.realRoot, filepath.Dir(d.path)); err == nil && a.realRoot != "" && !strings.HasPrefix(rel, "..") {
					loc = filepath.ToSlash(rel)
					if loc == "." {
						loc = filepath.Base(a.realRoot)
					}
				}
				row := ui.ButtonBase(c).Label(loc).Tooltip(d.path).FillWidth().Padding(7, 10)
				if row.Hovered() {
					row.Background(colors(c).Pill)
				}
				row.Children(func() {
					ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
						symbolIcon(c, icFolder, 13, colors(c).Muted)
						ui.Text(c, loc).FontSize(12).SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0).Shrink(1)
						ui.Box(c).Grow(1)
						symbolIcon(c, icExternal, 9, colors(c).Dim)
					})
				})
				row.OnClick(func() {
					a.services.OpenURL("file://" + filepath.Dir(d.path))
				})
			})
			if d.kind == workspace.Text || d.plain {
				inspectorSection(c, inspectorText("inspectorTextSettings"), func() {
					enc := "UTF-8"
					if d.bom {
						enc = inspectorText("inspectorEncodingBOM")
					}
					if d.lossy {
						enc += inspectorText("inspectorEncodingLossy")
					}
					inspectorField(c, inspectorText("inspectorEncoding"), enc)
					inspectorHairline(c)
					inspectorField(c, inspectorText("inspectorLineEndings"), inspectorLineEndings(d))
				})
			}
			if a.git.root != "" {
				if status, key := inspectorGitStatus(a, d); status != "" {
					inspectorSection(c, inspectorText("inspectorSourceControl"), func() {
						inspectorField(c, inspectorText("inspectorStatus"), inspectorText(key))
					})
				}
			}
		})
	})
}

// inspectorField is one label–value row of a section, as Xcode's
// property rows: the name muted, its value at the end.
func inspectorField(c *ui.Context, label, value string) {
	ui.Row(c).FillWidth().Padding(4, 10).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(colors(c).Muted).SingleLine().Shrink(0)
		ui.Box(c).Grow(1)
		ui.Text(c, value).FontSize(12).SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0).Shrink(1)
	})
}

// inspectorHairline is the separator between a section's rows, fainter
// than the pane's dividers.
func inspectorHairline(c *ui.Context) {
	ui.Row(c).FillWidth().Padding(0, 0, 0, 10).Children(func() {
		ui.Box(c).FillWidth().Height(1).Background(colors(c).Hairline.Alpha(0.6))
	})
}

// inspectorFileType names the file's kind for the header, as Xcode's
// Type field: a friendly name where one is known, else the extension.
func inspectorFileType(d *doc) string {
	switch d.kind {
	case workspace.Binary:
		return inspectorText("inspectorTypeBinary")
	case workspace.BadEncoding:
		return inspectorText("inspectorTypeUnknown")
	}
	names := map[string]string{
		".go": "Go source", ".mod": "Go module file", ".sum": "Go checksum file",
		".ts": "TypeScript source", ".tsx": "TypeScript source", ".js": "JavaScript source",
		".jsx": "JavaScript source", ".mts": "TypeScript source", ".cts": "TypeScript source",
		".json": "JSON", ".jsonc": "JSON with Comments", ".yaml": "YAML", ".yml": "YAML",
		".toml": "TOML", ".xml": "XML", ".html": "HTML", ".css": "CSS", ".svg": "SVG image",
		".md": "Markdown", ".markdown": "Markdown", ".txt": "Plain text",
		".c": "C source", ".h": "C/C++ header", ".cc": "C++ source", ".cpp": "C++ source",
		".hpp": "C/C++ header", ".rs": "Rust source", ".py": "Python source",
		".java": "Java source", ".kt": "Kotlin source", ".swift": "Swift source",
		".sh": "Shell script", ".bash": "Shell script", ".zsh": "Shell script",
		".png": "PNG image", ".jpg": "JPEG image", ".jpeg": "JPEG image", ".gif": "GIF image",
		".webp": "WebP image", ".mp4": "Video", ".mov": "Video", ".mkv": "Video",
		".zig": "Zig source", ".cmake": "CMake file",
	}
	ext := strings.ToLower(filepath.Ext(d.name))
	if name := names[ext]; name != "" {
		return name
	}
	if ext == "" {
		if d.kind == workspace.Text || d.plain {
			return inspectorText("inspectorTypeText")
		}
		return inspectorText("inspectorTypeUnknown")
	}
	return inspectorText("inspectorTypeText")
}

// inspectorLineEndings is the file's dominant line ending, read from its
// loaded text: CRLF, CR, or LF, as Xcode's Line Endings field.
func inspectorLineEndings(d *doc) string {
	text := d.diskText
	if text == "" && d.buf != nil {
		text = d.buf.Snapshot().String()
	}
	if len(text) > 1<<20 {
		text = text[:1<<20]
	}
	if strings.Contains(text, "\r\n") {
		return "CRLF"
	}
	if strings.Contains(text, "\r") {
		return "CR"
	}
	return "LF"
}

// inspectorGitStatus is the file's source control status letter and its
// message's key, or "" when the file is unchanged or not in the repo.
func inspectorGitStatus(a *App, d *doc) (string, string) {
	for _, ch := range a.git.changes {
		abs := filepath.Join(a.git.root, filepath.FromSlash(ch.Path))
		if abs != d.path {
			continue
		}
		switch {
		case ch.Index == '?' || ch.Work == '?':
			return "?", "inspectorStatusUntracked"
		case ch.Index == 'U' || ch.Work == 'U' || ch.Index == 'A' && ch.Work == 'A':
			return "U", "inspectorStatusConflicted"
		case ch.Index == 'R' || ch.Work == 'R':
			return "R", "inspectorStatusRenamed"
		case ch.Index == 'A' || ch.Work == 'A':
			return "A", "inspectorStatusAdded"
		case ch.Index == 'D' || ch.Work == 'D':
			return "D", "inspectorStatusDeleted"
		default:
			return "M", "inspectorStatusModified"
		}
	}
	return "", ""
}

// inspectorHistoryPage is Xcode's History inspector: the commits that
// touched the file the focused editor shows, newest first, each opening
// its commit view.
func (a *App) inspectorHistoryPage(c *ui.Context) {
	h := &a.inspector.history
	a.syncInspectorHistory()
	switch {
	case a.git.root == "":
		a.inspectorPlaceholder(c, icClock, inspectorText("inspectorNotRepo"))
	case a.inspectorFileDoc() == nil:
		a.inspectorPlaceholder(c, icPage, inspectorText("inspectorNoFile"))
	case h.loading && len(h.commits) == 0:
		ui.Column(c).Grow(1).FillWidth().Center().Children(func() { ui.Spinner(c) })
	case h.err != "":
		a.inspectorPlaceholder(c, icClock, h.err)
	case len(h.commits) == 0:
		a.inspectorPlaceholder(c, icClock, inspectorText("inspectorNoHistory"))
	default:
		ui.Scroll(c).Grow(1).FillWidth().TrackScroll(&h.scroll).
			Label(inspectorText("inspectorHistoryPane")).Children(func() {
			ui.Column(c).FillWidth().Padding(6, 6, 14, 6).Gap(20).Children(func() {
				inspectorSection(c, inspectorText("inspectorRecent"), func() {
					for i, cm := range h.commits {
						cm := cm
						if i > 0 {
							inspectorHairline(c)
						}
						row := ui.ButtonBase(c).Label(cm.Subject).Tooltip(cm.Short+" "+cm.Subject).
							FillWidth().Padding(7, 10)
						if row.Hovered() {
							row.Background(colors(c).Pill)
						}
						row.Children(func() {
							ui.Row(c).FillWidth().Gap(9).AlignItems(ui.Center).Children(func() {
								a.commitAvatars(c, cm, avatarSize)
								ui.Column(c).Grow(1).Shrink(1).Gap(1).Children(func() {
									ui.Text(c, cm.Subject).FontSize(12).FontWeight(600).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
									ui.Text(c, a.commitByline(cm)+" · "+cm.Short).FontSize(11).
										TextColor(colors(c).Muted).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
								})
								compare := ui.ButtonBase(c).Size(22, 22).Padding(0).Radius(4).Center().KeepFocus().
									Label(commandLabel("history.compare")).Tooltip(commandLabel("history.compare"))
								if compare.Hovered() {
									compare.Background(colors(c).Pill)
								}
								compare.Children(func() { symbolIcon(c, icReplaceMatch, 12, colors(c).Muted) })
								if compare.Clicked() {
									a.compareInspectorRevision(cm.Hash)
								}
							})
						})
						row.OnClick(func() { a.openCommit(cm) })
					}
				})
			})
		})
	}
}

// syncInspectorHistory keeps the History page's commits of the focused
// file: it reads them when the file, HEAD, or the page changed.
func (a *App) syncInspectorHistory() {
	h := &a.inspector.history
	var rel string
	if d := a.inspectorFileDoc(); d != nil && !d.untitled && !strings.HasPrefix(d.path, diffTabPrefix) && a.git.root != "" {
		if r, err := filepath.Rel(a.git.root, d.path); err == nil && !strings.HasPrefix(r, "..") {
			rel = r
		}
	}
	if h.path == rel && h.head == a.git.head && (h.loading || h.err != "" || h.commits != nil || rel == "") {
		return
	}
	h.seq++
	h.path, h.head, h.err = rel, a.git.head, ""
	h.commits = nil
	if rel == "" {
		h.loading = false
		return
	}
	h.loading = true
	seq, gen, scopeSeq, ctx, root, path := h.seq, a.gen, a.git.scopeSeq, a.wsCtx, a.git.root, rel
	go func() {
		commits, err := git.LogFile(ctx, root, false, 0, inspectorHistoryPage, path)
		a.post.Post(func() {
			if a.gen != gen || a.git.scopeSeq != scopeSeq || h.seq != seq {
				return
			}
			h.loading = false
			if err != nil {
				h.err = gitErrLine(err)
				return
			}
			h.commits = commits
		})
	}()
}

// docURL is the first web address of a doc comment's markdown, the one
// Xcode's "Open in Developer Documentation" button goes to.
var docURL = regexp.MustCompile(`https?://[^\s)\]]+`)
