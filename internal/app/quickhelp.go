package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"see/locales"
	"strings"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"

	"see/internal/langserver"
	"see/internal/lsp"
	"see/internal/nav"
)

// The Quick Help inspector is a persistent pane beside the editors, as
// Xcode's right-side inspector: it follows the caret's symbol so that
// documentation stays on screen while the code it explains is read, and
// pins so that one symbol's notes survive a move elsewhere.

// quickHelpInspector is the pane's state, for the window rather than a
// group: it follows the focused group's caret.
type quickHelpInspector struct {
	pinned bool
	// subject is the symbol shown or asked for; identity is doc +
	// buffer version + offset, so an edit marks the shown text stale
	// and a request for where the caret went is told from its answer.
	subject    inspectorSubject
	hasSubject bool
	// content is what stays on screen: the last answer's markdown and
	// declaration target, or what err says failed, over which loading
	// draws a progress line.
	markdown  string
	declared  *target
	enc       string
	hasResult bool
	err       error
	// seq and cancel make only the latest request land; loading reports
	// whether one is on its way, for the header's progress line.
	seq     uint64
	cancel  context.CancelFunc
	loading bool
	// pending is the caret position the debounce timer asks about.
	pending    inspectorSubject
	hasPending bool
	timer      *time.Timer
	scroll     ui.ScrollState
	// tab is the strip's selected page; fileScroll and history are the
	// other pages' state.
	tab        int
	fileScroll ui.ScrollState
	history    inspectorFileHistory
}

// inspectorSubject identifies a symbol request and its answer.
type inspectorSubject struct {
	path    string
	doc     *doc
	version uint64 // of the buffer the offset is of
	offset  int    // byte offset of the symbol's start
	name    string // the word the request was made for
}

const (
	inspectorDebounce = 250 * time.Millisecond
	inspectorMinWidth = 220
	inspectorMaxWidth = 420
	inspectorDefWidth = 280
)

// inspectorOpen reports whether the pane is up.
func (a *App) inspectorOpen() bool { return a.cfg.QuickHelpInspector }

// toggleInspector opens and closes the pane, from its command, its jump
// bar button and Settings.
func (a *App) toggleInspector() {
	a.cfg.QuickHelpInspector = !a.cfg.QuickHelpInspector
	a.settingsChanged()
	if !a.cfg.QuickHelpInspector {
		in := &a.inspector
		in.pinned = false
		in.hasPending = false
		if in.timer != nil {
			in.timer.Stop()
			in.timer = nil
		}
		if in.cancel != nil {
			in.cancel()
			in.cancel = nil
		}
		in.seq++
		in.loading = false
	}
}

// inspectorW gives the pane's width, from the settings, clamped to the
// bounds the divider holds.
func (a *App) inspectorW() float32 {
	w := a.cfg.QuickHelpWidth
	if w <= 0 {
		return inspectorDefWidth
	}
	return min(max(w, inspectorMinWidth), inspectorMaxWidth)
}

// followInspector is called every frame the pane is up: it notices the
// caret move and starts the debounce toward a request for what it moved
// to. Pinned, it notices nothing.
func (a *App) followInspector() {
	in := &a.inspector
	if !a.inspectorOpen() || in.pinned {
		return
	}
	d, st := a.inspectorTarget()
	if d == nil || st == nil || d.buf == nil {
		in.hasPending = false
		return
	}
	snap := d.buf.Snapshot()
	offset := st.Buffer.Offset(st.CursorPos())
	pos := snap.Pos(offset)
	line := snap.Line(pos.Line)
	ws, we := wordAt(line, pos.Col)
	if ws == we {
		// Whitespace does not chase the last symbol away, but a word
		// there stops being the subject.
		in.hasPending = false
		return
	}
	sub := inspectorSubject{path: d.path, doc: d, version: snap.Version(), offset: snap.LineStart(pos.Line) + ws, name: line[ws:we]}
	if in.hasSubject && sub == in.subject && !in.hasPending {
		return
	}
	if in.hasPending && in.pending == sub {
		return // already on its way
	}
	in.pending = sub
	in.hasPending = true
	if in.timer != nil {
		in.timer.Stop()
	}
	in.timer = time.AfterFunc(inspectorDebounce, func() {
		a.post.Post(func() {
			in.timer = nil
			if !in.hasPending || in.pending != sub || in.pinned {
				return
			}
			in.hasPending = false
			a.requestInspector(sub)
		})
	})
}

// inspectorTarget is the document and editor the pane follows: the
// focused group's, as Xcode's inspector follows the active editor.
func (a *App) inspectorTarget() (*doc, *editor.State) {
	g := a.focus
	if g == nil {
		return nil, nil
	}
	path := tabPath(g.active)
	d := a.docs[path]
	return d, g.eds[path]
}

// requestInspector asks the language server about sub, as the popover
// does: hover for the documentation, then, briefly, definition for the
// source row. Only the latest answer lands; a stale one is dropped, as
// is one for a buffer since edited.
func (a *App) requestInspector(sub inspectorSubject) {
	in := &a.inspector
	m := a.serverFor(sub.path)
	if m == nil || !a.cfg.IsTrusted(a.root) {
		in.subject = sub
		in.hasSubject = true
		in.err = nil
		in.hasResult = false
		in.loading = false
		return
	}
	if state, _ := m.Status(); state == langserver.Failed {
		in.subject = sub
		in.hasSubject = true
		in.err = errors.New(locales.MsgLanguageServerFailed())
		in.hasResult = false
		in.loading = false
		return
	}
	if in.cancel != nil {
		in.cancel()
	}
	in.seq++
	seq, gen := in.seq, a.gen
	snap := sub.doc.buf.Snapshot()
	in.subject = sub
	in.hasSubject = true
	in.loading = true
	ctx, cancel := context.WithTimeout(a.wsCtx, 10*time.Second)
	in.cancel = cancel
	go func() {
		defer cancel()
		client, err := m.Get(ctx)
		if err == nil {
			err = m.Sync(ctx, client, sub.doc.uri, snap)
		}
		var md string
		var declared *target
		var enc string
		if err == nil {
			enc = client.Encoding()
			pos := snap.Pos(sub.offset)
			line := snap.Line(pos.Line)
			at := lsp.Position{Line: pos.Line, Character: lsp.ColumnToLSP(line, pos.Col, enc)}
			md, _, err = client.Hover(ctx, sub.doc.uri, at)
			if err == nil && client.HasDefinition() {
				definitionCtx, cancelDefinition := context.WithTimeout(ctx, quickHelpDefinitionTimeout)
				locations, _ := client.Definition(definitionCtx, sub.doc.uri, at)
				cancelDefinition()
				targets, _ := targetsOf(locations)
				if len(targets) > 0 {
					declared = &targets[0]
				}
			}
		}
		a.post.Post(func() {
			if a.gen != gen || in.seq != seq || !in.hasSubject || in.subject != sub || a.docs[sub.path] != sub.doc || sub.doc.buf == nil || sub.doc.buf.Version() != sub.version {
				return // the caret or the text moved on
			}
			in.loading = false
			in.cancel = nil
			if err != nil {
				in.err = err
				return // the last good answer stays, under the error line
			}
			in.err = nil
			in.markdown = md
			in.declared = declared
			in.enc = enc
			in.hasResult = true
			in.scroll.ScrollTo(0, 0, ui.Animation{})
		})
	}()
}

// pinInspector holds the shown symbol against the caret's moves, or lets
// the caret lead again. Unlocking asks about where the caret is now.
func (a *App) pinInspector() {
	in := &a.inspector
	in.pinned = !in.pinned
	if !in.pinned {
		in.hasSubject = false
	}
}

// refreshInspector re-asks about the pinned or current symbol.
func (a *App) refreshInspector() {
	in := &a.inspector
	if in.hasSubject {
		sub := in.subject
		in.hasSubject = false
		a.requestInspector(sub)
	}
}

// inspectorView draws the pane: header with symbol name, pin and
// refresh, progress line while loading, then the sections the popover
// shares, the source row, and the actions that hand the symbol to
// navigation.
func (a *App) inspectorView(c *ui.Context) {
	in := &a.inspector
	ui.Column(c).Fill().Background(colors(c).Sidebar).BorderWidth(0, 0, 0, 1).BorderColor(colors(c).Separator).Children(func() {
		a.inspectorTabs(c)
		switch a.inspectorTab() {
		case "file":
			a.inspectorFilePage(c)
		case "history":
			a.inspectorHistoryPage(c)
		default:
			a.inspectorHeader(c)
			if in.loading {
				ui.Box(c).FillWidth().Height(1).Background(c.Theme().Accent).Shrink(0)
			}
			a.inspectorBody(c)
		}
	})
}

// inspectorHeader is the pane's top: the symbol's name and kind, as
// Xcode's inspector shows them over the content, and the pin and
// refresh of its following.
func (a *App) inspectorHeader(c *ui.Context) {
	in := &a.inspector
	ui.Row(c).FillWidth().Height(44).Padding(0, 6, 0, 12).Gap(8).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(colors(c).Separator).Children(func() {
		name := in.subject.name
		has := in.hasSubject && name != ""
		if !has {
			name = inspectorText("inspectorNoSymbol")
		}
		// The kind badge only means something once it colors a real
		// symbol's kind; a bare "v" next to "No Symbol" read as noise.
		if has {
			kind := inspectorSymbolKind(a)
			letter, color := symbolKind(c, kind)
			ui.Box(c).Width(20).Height(20).Radius(5).Shrink(0).Background(color.Alpha(0.16)).Center().Children(func() {
				ui.Text(c, letter).FontSize(10).Bold().TextColor(color).SingleLine()
			})
		}
		ui.Column(c).Shrink(1).Gap(1).Children(func() {
			ui.Text(c, name).FontSize(12).FontWeight(600).SingleLine().Ellipsis("…")
			if has {
				ui.Text(c, outlineKindName(inspectorSymbolKind(a))).FontSize(10).TextColor(colors(c).Muted).SingleLine()
			}
		})
		if in.hasResult && in.subject.doc != nil && in.subject.doc.buf != nil && in.subject.doc.buf.Version() != in.subject.version {
			ui.Text(c, inspectorText("inspectorStale")).FontSize(11).TextColor(colors(c).Muted).SingleLine()
		}
		ui.Box(c).Grow(1)
		if glyphButton(c, icRefresh, inspectorText("inspectorRefresh"), inspectorText("inspectorRefresh"), false, !in.hasSubject).Clicked() {
			a.refreshInspector()
		}
		pin := glyphToggle(c, icPin, inspectorText("inspectorPin"), inspectorText("inspectorPinTip"), in.pinned, !in.hasSubject)
		if pin.Clicked() {
			a.pinInspector()
		}
	})
}

// inspectorSymbolKind is the kind badge of the shown symbol, from the
// document outline when it knows the symbol's name, else the generic one.
func inspectorSymbolKind(a *App) int {
	in := &a.inspector
	if !in.hasSubject || in.subject.doc == nil {
		return 13 // variable, the badge a symbol without an outline wears
	}
	o := &a.outline
	if o.shown.doc == in.subject.doc {
		for _, s := range o.symbols {
			if s.Name == in.subject.name {
				return s.Kind
			}
		}
	}
	return 13
}

// inspectorBody is what scrolls: the documentation sections, or the
// reasons they are empty, then the source row and the actions.
func (a *App) inspectorBody(c *ui.Context) {
	in := &a.inspector
	switch {
	case !a.cfg.IsTrusted(a.root):
		// Symbol help needs the servers; the editor's banner already
		// explains what they may do, so the pane carries only the decision
		// and its button rather than repeating the detail.
		ui.Column(c).FillWidth().Gap(8).Padding(10, 12).Children(func() {
			ui.Text(c, locales.TrustBannerLead(filepath.Base(a.root))).FontSize(12).FontWeight(600).FillWidth()
			ui.Row(c).Gap(8).Children(func() {
				ui.Button(c, locales.TrustFolder()).OnClick(a.trust)
				ui.Button(c, locales.NotNow()).OnClick(func() { a.trustAsk, a.afterTrust = false, nil; a.trustDismissed = true })
			})
		})
	case !in.hasSubject:
		a.inspectorEmpty(c, inspectorText("inspectorEmpty"))
	case !in.hasResult && in.err != nil:
		a.inspectorEmpty(c, in.err.Error())
	case !in.hasResult && in.loading:
		ui.Column(c).Grow(1).FillWidth().Center().Children(func() { ui.Spinner(c) })
	case !in.hasResult:
		a.inspectorEmpty(c, inspectorText("inspectorNoDoc"))
	default:
		ui.Scroll(c).Grow(1).FillWidth().TrackScroll(&in.scroll).Label(inspectorText("inspectorPane")).Children(func() {
			a.inspectorContent(c)
		})
	}
}

// inspectorEmpty is the pane's placeholder, centered as Xcode draws its
// "No Quick Help" state: a dim icon over a muted explanation.
func (a *App) inspectorEmpty(c *ui.Context, text string) {
	ui.Column(c).FillWidth().Grow(1).Center().Padding(0, 20).Gap(8).Children(func() {
		symbolIcon(c, icDocQuestion, 32, colors(c).Dim)
		ui.Text(c, text).FontSize(12).TextColor(colors(c).Muted).TextAlign(ui.Center).FillWidth()
	})
}

// inspectorContent draws the subject's sections: declaration, summary,
// discussion, then the Declared In card and the Related actions,
// reusing the popover's split and the declaration's fenced block.
func (a *App) inspectorContent(c *ui.Context) {
	in := &a.inspector
	sections := splitQuickHelp(in.markdown)
	ui.Column(c).FillWidth().Padding(6, 6, 14, 6).Gap(20).Children(func() {
		render := func(label, body string) {
			if body == "" {
				return
			}
			ui.Column(c).FillWidth().Gap(5).Children(func() {
				ui.Text(c, quickHelpText(label)).FontSize(12).FontWeight(600).TextColor(colors(c).Muted).SingleLine()
				markdown.View(c, body, &markdown.Options{OnLink: a.openDocLink})
			})
		}
		if strings.TrimSpace(in.markdown) == "" && in.declared == nil {
			if in.err != nil {
				ui.Text(c, in.err.Error()).FontSize(12).TextColor(colors(c).Muted).FillWidth()
			} else {
				ui.Text(c, inspectorText("inspectorNoDoc")).FontSize(12).TextColor(colors(c).Muted).FillWidth()
			}
		}
		if in.err != nil && strings.TrimSpace(in.markdown) != "" {
			ui.Text(c, in.err.Error()).FontSize(12).TextColor(colors(c).Muted).FillWidth()
		}
		render("quickHelpSummary", sections.summary)
		if sections.declaration != "" {
			markdown.View(c, sections.declaration, &markdown.Options{
				CodeBlock: func(c *ui.Context, b *markdown.CodeBlockView) {
					th := markdown.DefaultTheme(c.Theme())
					ui.Column(c).FillWidth().Gap(4).Children(func() {
						ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
							ui.Text(c, quickHelpText("quickHelpDeclaration")).FontSize(12).
								FontWeight(600).TextColor(colors(c).Muted).SingleLine()
							ui.Spacer(c)
							b.CopyButton(c)
						})
						ui.Column(c).FillWidth().Background(th.CodeBlockBackground).
							Border(1, th.CodeBlockBorder).Radius(th.CodeBlockRadius).Clip().Children(func() {
							b.Body(c)
						})
					})
				},
			})
		}
		render("quickHelpDiscussion", sections.discussion)
		if in.declared != nil {
			a.inspectorSourceRow(c)
		}
		a.inspectorDocButton(c)
		a.inspectorActions(c)
	})
}

// inspectorDocButton is the pane's way into the library's full
// documentation, as Xcode's "Open in Developer Documentation" button:
// the first web address of the doc comment, when one is there.
func (a *App) inspectorDocButton(c *ui.Context) {
	u := docURL.FindString(a.inspector.markdown)
	if u == "" {
		return
	}
	inspectorSection(c, inspectorText("inspectorDocs"), func() {
		row := ui.ButtonBase(c).Label(inspectorText("inspectorOpenDocs")).Tooltip(u).FillWidth().Padding(7, 10)
		if row.Hovered() {
			row.Background(colors(c).Pill)
		}
		row.Children(func() {
			ui.Row(c).FillWidth().Gap(9).AlignItems(ui.Center).Children(func() {
				symbolIcon(c, icExternal, 13, colors(c).Muted)
				ui.Text(c, inspectorText("inspectorOpenDocs")).FontSize(12).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
				ui.Box(c).Grow(1)
				symbolIcon(c, icChevron, 9, colors(c).Dim)
			})
		})
		row.OnClick(func() { a.openDocLink(u) })
	})
}

// openDocLink opens the http and https links of a doc comment in the
// browser, as the editor's link items do; a relative target is left
// alone until there is a resolver for it.
func (a *App) openDocLink(dest string) {
	if u, err := url.Parse(dest); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		a.services.OpenURL(dest)
	}
}

// inspectorSection is a labeled group of the pane, as Xcode's
// "Summary" and "Declared In" blocks: a muted title over flat rows,
// which only hairlines divide.
func inspectorSection(c *ui.Context, label string, children func()) {
	ui.Column(c).FillWidth().Gap(3).Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(colors(c).Muted).SingleLine().Padding(0, 10)
		ui.Column(c).FillWidth().Children(children)
	})
}

// inspectorSourceRow is `path:line` where the symbol is declared, which
// opens the file there, as the popover's Declared In row; a card row
// with the file's icon and a chevron, as Xcode's link rows.
func (a *App) inspectorSourceRow(c *ui.Context) {
	in := &a.inspector
	t := in.declared
	path := t.path
	if rel, err := filepath.Rel(a.root, path); err == nil && a.root != "" {
		path = rel
	} else {
		path = filepath.Base(path)
	}
	line := fmt.Sprintf("%s:%d", filepath.ToSlash(path), t.pos.Line+1)
	inspectorSection(c, quickHelpText("quickHelpDeclaredIn"), func() {
		row := ui.ButtonBase(c).Label(line).Tooltip(t.path).FillWidth().Padding(7, 10)
		if row.Hovered() {
			row.Background(colors(c).Pill)
		}
		row.Children(func() {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				fileIcon(c, filepath.Base(t.path), 15)
				ui.Text(c, line).FontSize(12).SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0).Shrink(1)
				symbolIcon(c, icChevron, 9, colors(c).Dim)
			})
		})
		row.OnClick(func() {
			if g := a.focus; g != nil {
				from := nav.Entry{Path: in.subject.path}
				a.jump(g, from, *t, in.enc)
			}
		})
	})
}

// inspectorActions are the pane's Related section: the symbol goes to
// the views for callers, references and implementations rather than to
// a competing list of its own. Each is a card row, as Xcode's related
// items, with an icon and a chevron.
func (a *App) inspectorActions(c *ui.Context) {
	in := &a.inspector
	if in.subject.doc == nil {
		return
	}
	acts := []struct {
		icon  *ui.SVG
		label string
		kind  string
	}{
		{icCallers, inspectorText("inspectorCallers"), "callers"},
		{icChain, inspectorText("inspectorReferences"), "references"},
		{icLayers, inspectorText("inspectorImplementations"), "implementation"},
	}
	inspectorSection(c, locales.MsgRelatedItems(), func() {
		for i, act := range acts {
			act := act
			if i > 0 {
				ui.Row(c).FillWidth().Padding(0, 0, 0, 32).Children(func() {
					ui.Box(c).FillWidth().Height(1).Background(colors(c).Hairline)
				})
			}
			row := ui.ButtonBase(c).Label(act.label).Tooltip(act.label).FillWidth().Padding(7, 10)
			if row.Hovered() {
				row.Background(colors(c).Pill)
			}
			row.Children(func() {
				ui.Row(c).FillWidth().Gap(9).AlignItems(ui.Center).Children(func() {
					symbolIcon(c, act.icon, 13, colors(c).Muted)
					ui.Text(c, act.label).FontSize(12).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
					ui.Box(c).Grow(1)
					symbolIcon(c, icChevron, 9, colors(c).Dim)
				})
			})
			row.OnClick(func() { a.inspectorAction(act.kind) })
		}
	})
}

// inspectorAction asks the subject's navigation, as the jump bar's
// related menu does for the caret's symbol.
func (a *App) inspectorAction(kind string) {
	in := &a.inspector
	if !in.hasSubject || in.subject.doc == nil {
		return
	}
	g := a.focus
	if g == nil {
		return
	}
	offset := in.subject.offset
	st := g.eds[in.subject.path]
	if st != nil && in.subject.doc.buf != nil {
		pos := st.Buffer.Pos(min(offset, st.Buffer.Len()))
		st.GoTo(pos)
	}
	a.focus = g
	switch kind {
	case "callers":
		a.findUsagesAt(true, ui.Point{}, false)
	case "implementation":
		a.goToLocation(g, in.subject.doc, offset, "implementation")
	default:
		a.findUsagesAt(false, ui.Point{}, false)
	}
}

// inspectorDivider is the pane's left edge, which the pointer drags to
// resize it between its bounds, as the sidebar's does.
func (a *App) inspectorDivider(c *ui.Context) {
	edge := ui.Box(c).Absolute().Left(-3).Top(0).Bottom(0).Width(7).Cursor(ui.CursorResizeEW)
	if dx, _, ok := edge.Dragged(); ok && dx != 0 {
		w := min(max(a.inspectorW()-dx, inspectorMinWidth), inspectorMaxWidth)
		if w != a.cfg.QuickHelpWidth {
			a.cfg.QuickHelpWidth = w
			a.saveConfigSoon()
		}
	}
}

func inspectorText(key string) string {
	return locales.Catalog.Format(i18n.Locale(), key, nil)
}
