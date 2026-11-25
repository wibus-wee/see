package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"see/locales"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"

	"see/internal/lsp"
	"see/internal/nav"

	"see/internal/langserver"
)

// hoverState follows language server hover requests: only the latest
// one shows. The link Cmd+click goes from is the editor's own, drawn as
// the modifier is held; with it held, a hover is the definition's code
// in the card.
type hoverState struct {
	seq    uint64
	cancel context.CancelFunc
	cmd    bool // Cmd held, as the last frame saw
	cache  map[hoverKey]hoverResult
	epoch  uint64
}

type hoverKey struct {
	path    string
	version uint64
	offset  int
	cmd     bool
}

type hoverResult struct {
	start, end int
	markdown   string
	location   *hoverLocation
	at         time.Time
}

type hoverLocation struct {
	path string
	line int
}

const hoverCacheTTL = 10 * time.Second
const hoverCacheLimit = 128
const quickHelpDefinitionTimeout = 300 * time.Millisecond

func (a *App) invalidateHover() {
	a.hover.epoch++
	a.hover.seq++
	if a.hover.cancel != nil {
		a.hover.cancel()
		a.hover.cancel = nil
	}
	a.hover.cache = nil
	if st := a.quickHelp; st != nil {
		a.dismissQuickHelp(st)
	}
	a.quickHelp = nil
	a.quickHelpBounds = ui.Rect{}
	a.quickHelpRequest = nil
	a.quickHelpSeq = 0
}

func (a *App) showHover(st *editor.State, cmd bool, r hoverResult) {
	if cmd && !a.hover.cmd {
		return // the modifier was let go as the answer came back
	}
	st.ShowHover(r.start, r.end, "", func(c *ui.Context) { hoverContent(c, r.markdown, r.location) })
}

// hoverAt answers an editor's hover request: with the symbol's
// documentation from a language server, or with Cmd held its definition.
// It does not query servers in an untrusted workspace.
func (a *App) hoverAt(d *doc, st *editor.State, r editor.HoverRequest) {
	if a.quickHelp != nil || a.quickHelpRequest == st {
		return
	}
	if a.quickHelpRequest != nil {
		a.cancelQuickHelpRequest()
	}
	if d.buf == nil {
		return
	}
	snap := d.buf.Snapshot()
	pos := snap.Pos(r.Offset)
	line := snap.Line(pos.Line)
	// Past the line's end the pointer is on its trailing decoration,
	// which is blame: answer with the commit, whatever a server says.
	if pos.Col >= len(line) {
		if a.cfg.HideInlineBlame {
			return
		}
		if b, ok := a.lineBlameAt(d, pos.Line); ok {
			end := snap.LineStart(pos.Line) + len(line)
			st.ShowHover(end, end, "", func(c *ui.Context) { a.blameHoverContent(c, b) })
		}
		return
	}
	m := a.serverFor(d.path)
	if m == nil || !a.cfg.IsTrusted(a.root) {
		return
	}
	if s, _ := m.Status(); s == langserver.Failed {
		return
	}
	if a.hover.cancel != nil {
		a.hover.cancel()
	}
	a.hover.seq++
	seq, gen, cmd, epoch := a.hover.seq, a.gen, a.hover.cmd, a.hover.epoch
	ws, we := wordAt(line, pos.Col)
	if ws == we {
		return // not on a word
	}
	lineStart := snap.LineStart(pos.Line)
	key := hoverKey{path: d.path, version: snap.Version(), offset: lineStart + ws, cmd: cmd}
	if cached, ok := a.hover.cache[key]; ok && r.Offset >= cached.start && r.Offset < cached.end && time.Since(cached.at) < hoverCacheTTL {
		a.showHover(st, cmd, cached)
		return
	}
	ctx, cancel := context.WithTimeout(a.wsCtx, 10*time.Second)
	a.hover.cancel = cancel
	go func() {
		defer cancel()
		c, err := m.Get(ctx)
		if err != nil {
			return
		}
		if m.Sync(ctx, c, d.uri, snap) != nil {
			return
		}
		enc := c.Encoding()
		at := lsp.Position{Line: pos.Line, Character: lsp.ColumnToLSP(line, pos.Col, enc)}
		start, end := lineStart+ws, lineStart+we
		var md string
		var location *hoverLocation
		if cmd {
			locs, err := c.Definition(ctx, d.uri, at)
			targets, _ := targetsOf(locs)
			if err != nil || len(targets) == 0 {
				return
			}
			location = &hoverLocation{path: targets[0].path, line: targets[0].pos.Line + 1}
			md = definitionPreview(targets[0])
		} else {
			text, rng, err := c.Hover(ctx, d.uri, at)
			if err != nil || strings.TrimSpace(text) == "" {
				return
			}
			md = text
			if rng != nil && rng.Start.Line == pos.Line && rng.End.Line == pos.Line {
				start = lineStart + lsp.ColumnFromLSP(line, rng.Start.Character, enc)
				end = lineStart + lsp.ColumnFromLSP(line, rng.End.Character, enc)
			}
		}
		a.post.Post(func() {
			if a.gen != gen || a.hover.seq != seq || a.hover.epoch != epoch || a.docs[d.path] != d || d.buf == nil || d.buf.Version() != key.version {
				return // the pointer has moved on
			}
			visible := false
			for _, g := range a.groups() {
				if g.active == d.path && g.eds[d.path] == st {
					visible = true
					break
				}
			}
			if !visible {
				return
			}
			if a.hover.cache == nil || len(a.hover.cache) >= hoverCacheLimit {
				a.hover.cache = make(map[hoverKey]hoverResult)
			}
			result := hoverResult{start: start, end: end, markdown: md, location: location, at: time.Now()}
			a.hover.cache[key] = result
			a.showHover(st, cmd, result)
		})
	}()
}

// renderHover keeps the editor's selectable hover and gives it the app's
// popover colors and a subtle elevation.
func (a *App) renderHover(st *editor.State, c *ui.Context, panel ui.Element, h *editor.HoverCard) {
	editor.DefaultHover(c, panel, h)
	if a.quickHelp == st {
		a.quickHelpRendered = true
		a.quickHelpBounds = panel.LayoutBounds()
		panel.Label(quickHelpText("quickHelp"))
		panel.Width(500).MaxWidth(500).MaxWidthPercent(100).
			Shadow(0, 2, 8, 0, ui.RGBA(0, 0, 0, 0.1))
	} else {
		panel.MaxWidth(620).Padding(10).
			Shadow(0, 1, 4, 0, ui.RGBA(0, 0, 0, 0.08))
	}
	panel.Radius(8).Background(colors(c).Popover).Border(1, colors(c).PopoverEdge)
}

type quickHelpSections struct {
	declaration string
	summary     string
	discussion  string
}

func splitQuickHelp(md string) quickHelpSections {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	lines := strings.Split(md, "\n")
	var section quickHelpSections
	docs := make([]string, 0, len(lines))
	fence, fenceSize, fenceStart, fenceEnd := byte(0), 0, -1, -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if fenceStart < 0 {
			if marker, size := markdownFence(trimmed); size > 0 {
				fence, fenceSize, fenceStart = marker, size, i
				continue
			}
			docs = append(docs, line)
			continue
		}
		if fenceEnd < 0 && markdownFenceClose(trimmed, fence, fenceSize) {
			fenceEnd = i
			continue
		}
	}
	if fenceStart >= 0 {
		if fenceEnd < 0 {
			fenceEnd = len(lines) - 1
		}
		section.declaration = strings.TrimSpace(strings.Join(lines[fenceStart:fenceEnd+1], "\n"))
		docs = docs[:0]
		for i, line := range lines {
			if i < fenceStart || i > fenceEnd {
				docs = append(docs, line)
			}
		}
	}
	doc := strings.TrimSpace(strings.Join(docs, "\n"))
	if doc == "" {
		return section
	}
	docLines := strings.Split(doc, "\n")
	start := 0
	for start < len(docLines) && (strings.TrimSpace(docLines[start]) == "" || markdownSeparator(docLines[start])) {
		start++
	}
	if start == len(docLines) {
		return section
	}
	end := start
	for end < len(docLines) && strings.TrimSpace(docLines[end]) != "" {
		end++
	}
	section.summary = strings.TrimSpace(strings.Join(docLines[start:end], "\n"))
	if end < len(docLines) {
		section.discussion = strings.TrimSpace(strings.Join(docLines[end:], "\n"))
	}
	return section
}

func markdownFence(line string) (byte, int) {
	if len(line) < 3 || line[0] != '`' && line[0] != '~' {
		return 0, 0
	}
	n := 0
	for n < len(line) && line[n] == line[0] {
		n++
	}
	if n < 3 {
		return 0, 0
	}
	return line[0], n
}

func markdownFenceClose(line string, fence byte, size int) bool {
	marker, n := markdownFence(line)
	return marker == fence && n >= size && strings.TrimSpace(line[n:]) == ""
}

func markdownSeparator(line string) bool {
	marker := byte(0)
	count := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '-', '*', '_':
			if marker != 0 && line[i] != marker {
				return false
			}
			marker = line[i]
			count++
		case ' ', '\t':
		default:
			return false
		}
	}
	return count >= 3
}

func quickHelpText(key string) string {
	return locales.Catalog.Format(i18n.Locale(), key, nil)
}

func (a *App) showQuickHelpAtCaret() {
	g := a.focus
	if g == nil || g.active == "" {
		return
	}
	d, st := a.docs[g.active], a.activeEditor()
	if d != nil && st != nil {
		a.focusEditor = st
		a.quickHelpAt(g, d, st, st.Cursor())
	}
}

func (a *App) quickHelpAt(g *group, d *doc, st *editor.State, offset int) {
	if a.quickHelp != nil {
		a.dismissQuickHelp(a.quickHelp)
	}
	if a.quickHelpRequest != nil {
		a.cancelQuickHelpRequest()
	}
	m := a.serverFor(d.path)
	if m == nil || d.buf == nil || !a.cfg.IsTrusted(a.root) {
		return
	}
	if state, _ := m.Status(); state == langserver.Failed {
		return
	}
	snap := d.buf.Snapshot()
	pos := snap.Pos(offset)
	line := snap.Line(pos.Line)
	wordStart, wordEnd := wordAt(line, pos.Col)
	if wordStart == wordEnd {
		return
	}
	lineStart := snap.LineStart(pos.Line)
	start, end := lineStart+wordStart, lineStart+wordEnd
	if a.hover.cancel != nil {
		a.hover.cancel()
	}
	a.hover.seq++
	seq, gen, epoch := a.hover.seq, a.gen, a.hover.epoch
	a.quickHelpRequest, a.quickHelpSeq = st, seq
	ctx, cancel := context.WithTimeout(a.wsCtx, 10*time.Second)
	a.hover.cancel = cancel
	go func() {
		defer cancel()
		posted := false
		defer func() {
			if !posted {
				a.post.Post(func() {
					if a.quickHelpSeq == seq {
						a.quickHelpRequest = nil
						a.quickHelpSeq = 0
					}
				})
			}
		}()
		client, err := m.Get(ctx)
		if err != nil || m.Sync(ctx, client, d.uri, snap) != nil {
			return
		}
		enc := client.Encoding()
		at := lsp.Position{Line: pos.Line, Character: lsp.ColumnToLSP(line, pos.Col, enc)}
		md, rng, err := client.Hover(ctx, d.uri, at)
		if err != nil || strings.TrimSpace(md) == "" {
			return
		}
		if rng != nil && rng.Start.Line == pos.Line && rng.End.Line == pos.Line {
			start = lineStart + lsp.ColumnFromLSP(line, rng.Start.Character, enc)
			end = lineStart + lsp.ColumnFromLSP(line, rng.End.Character, enc)
		}
		var declared *target
		if client.HasDefinition() {
			definitionCtx, cancelDefinition := context.WithTimeout(ctx, quickHelpDefinitionTimeout)
			locations, _ := client.Definition(definitionCtx, d.uri, at)
			cancelDefinition()
			targets, _ := targetsOf(locations)
			if len(targets) > 0 {
				declared = &targets[0]
			}
		}
		from := nav.Entry{Path: d.path, Line: pos.Line, ByteColumn: pos.Col}
		posted = true
		a.post.Post(func() {
			if a.quickHelpSeq == seq {
				a.quickHelpRequest = nil
				a.quickHelpSeq = 0
			}
			if a.gen != gen || a.hover.seq != seq || a.hover.epoch != epoch || a.docs[d.path] != d || d.buf == nil || d.buf.Version() != snap.Version() {
				return
			}
			if g.active != d.path || g.eds[d.path] != st {
				return
			}
			a.quickHelp = st
			st.ShowHover(start, end, "", func(c *ui.Context) {
				quickHelpContent(c, md, declared, func() {
					if declared != nil {
						a.jump(g, from, *declared, enc)
					}
				}, a.root, a.openDocLink)
			})
		})
	}()
}

func quickHelpContent(c *ui.Context, md string, declared *target, open func(), root string, onLink func(string)) {
	sections := splitQuickHelp(md)
	ui.Scroll(c).Width(480).MaxWidthPercent(100).MaxHeight(400).Shrink(1).Children(func() {
		ui.Column(c).FillWidth().Padding(4).Gap(12).Children(func() {
			render := func(label, body string) {
				if body == "" {
					return
				}
				ui.Column(c).FillWidth().Gap(5).Children(func() {
					ui.Text(c, quickHelpText(label)).FontSize(12).FontWeight(600).TextColor(colors(c).Muted).SingleLine()
					markdown.View(c, body, &markdown.Options{OnLink: onLink})
				})
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
			// onLink is nil until set, as a doc link the pane should open
			// rather than the popover's default.
			if declared != nil {
				path := declared.path
				if rel, err := filepath.Rel(root, path); err == nil && root != "" {
					path = rel
				} else {
					path = filepath.Base(path)
				}
				line := fmt.Sprintf("%s:%d", filepath.ToSlash(path), declared.pos.Line+1)
				ui.Column(c).FillWidth().Gap(5).Children(func() {
					ui.Text(c, quickHelpText("quickHelpDeclaredIn")).FontSize(12).FontWeight(600).
						TextColor(colors(c).Muted).SingleLine()
					ui.Row(c).FillWidth().Cursor(ui.CursorPointer).Tooltip(declared.path).OnClick(open).Children(func() {
						ui.Text(c, line).FontSize(12).TextColor(c.Theme().Accent).SingleLine()
					})
				})
			}
		})
	})
}

func (a *App) observeQuickHelp(ev ui.InputEvent) {
	st := a.quickHelp
	switch ev.Kind {
	case ui.InputPointerDown:
		if st != nil && a.quickHelpBounds.Contains(ev.X, ev.Y) {
			return
		}
	case ui.InputKeyDown, ui.InputText, ui.InputCommand:
	default:
		return
	}
	if st != nil {
		a.dismissQuickHelp(st)
	} else {
		a.cancelQuickHelpRequest()
	}
}

func (a *App) quickHelpEditorInput(g *group, d *doc, st *editor.State, ev ui.InputEvent) bool {
	if a.quickHelp == st && (ev.Kind == ui.InputKeyDown || ev.Kind == ui.InputText || ev.Kind == ui.InputCommand) {
		a.dismissQuickHelp(st)
	} else if a.quickHelpRequest == st && (ev.Kind == ui.InputKeyDown || ev.Kind == ui.InputText || ev.Kind == ui.InputCommand) {
		a.cancelQuickHelpRequest()
	}
	if ev.Kind == ui.InputPointerDown && ev.Button == 0 && ev.Mods == ui.Alt {
		a.post.Post(func() {
			if a.docs[d.path] == d && g.eds[d.path] == st && g.active == d.path {
				a.quickHelpAt(g, d, st, st.Cursor())
			}
		})
	}
	return false
}

func (a *App) dismissQuickHelp(st *editor.State) {
	if a.quickHelp == st {
		a.quickHelp = nil
		a.quickHelpBounds = ui.Rect{}
		st.Run(editor.Cancel)
	}
	if a.quickHelpRequest == st {
		a.cancelQuickHelpRequest()
	}
}

func (a *App) cancelQuickHelpRequest() {
	if a.quickHelpRequest == nil {
		return
	}
	a.quickHelpRequest = nil
	a.quickHelpSeq = 0
	a.hover.seq++
	if a.hover.cancel != nil {
		a.hover.cancel()
		a.hover.cancel = nil
	}
}

func hoverContent(c *ui.Context, md string, location *hoverLocation) {
	ui.Column(c).MaxWidth(600).MaxHeight(420).Gap(8).Children(func() {
		if location != nil {
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Tooltip(fmt.Sprintf("%s:%d", location.path, location.line)).Children(func() {
				fileIcon(c, filepath.Base(location.path), 13)
				ui.Row(c).Gap(0).Shrink(1).Children(func() {
					ui.Text(c, filepath.Base(location.path)).FontSize(12).TextColor(colors(c).Text).SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0).Shrink(1)
					ui.Text(c, fmt.Sprintf(":%d", location.line)).FontSize(12).TextColor(colors(c).Text).SingleLine().Shrink(0)
				})
				parent := filepath.Base(filepath.Dir(location.path))
				if parent != "." && parent != string(filepath.Separator) {
					ui.Text(c, parent).FontSize(11).TextColor(colors(c).Muted).SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0).Shrink(1)
				}
			})
		}
		if md != "" {
			ui.Scroll(c).MaxHeight(380).Shrink(1).Label(locales.MsgHoverDocumentation()).Children(func() {
				markdown.View(c, md, nil)
			})
		}
	})
}

// definitionPreview reads the declaration and its surrounding lines as Markdown.
func definitionPreview(t target) string {
	data, err := os.ReadFile(t.path)
	if err != nil || len(data) > 4<<20 {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	from := max(0, t.pos.Line-docLinesAbove(lines, t.pos.Line))
	to := min(len(lines), t.pos.Line+12)
	snippet := strings.TrimRight(strings.ReplaceAll(strings.Join(lines[from:to], "\n"), "\r", ""), "\n")
	lang := strings.TrimPrefix(filepath.Ext(t.path), ".")
	return fmt.Sprintf("```%s\n%s\n```", lang, snippet)
}

// docLinesAbove counts the comment lines right above line, up to 6, so
// the preview shows a declaration's doc comment.
func docLinesAbove(lines []string, line int) int {
	n := 0
	for i := line - 1; i >= 0 && n < 6; i-- {
		if !strings.HasPrefix(strings.TrimSpace(lines[i]), "//") {
			break
		}
		n++
	}
	return n
}

// wordAt finds the identifier around byte col of line.
func wordAt(line string, col int) (start, end int) {
	isWord := func(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }
	col = min(max(col, 0), len(line))
	start, end = col, col
	for start > 0 {
		r, n := utf8.DecodeLastRuneInString(line[:start])
		if !isWord(r) {
			break
		}
		start -= n
	}
	for end < len(line) {
		r, n := utf8.DecodeRuneInString(line[end:])
		if !isWord(r) {
			break
		}
		end += n
	}
	return start, end
}
