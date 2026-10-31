package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"

	"see/internal/lsp"

	"see/internal/langserver"
)

// hoverState follows language server hover requests: only the latest one
// shows, and while Cmd is held a hover is a link to the definition,
// underlaid in the text, with the definition's code in the card.
type hoverState struct {
	seq    uint64
	cancel context.CancelFunc
	cmd    bool          // Cmd held, as the last frame saw
	link   *editor.State // the editor showing a link
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

func (a *App) invalidateHover() {
	a.hover.epoch++
	a.hover.seq++
	if a.hover.cancel != nil {
		a.hover.cancel()
		a.hover.cancel = nil
	}
	a.hover.cache = nil
	a.hideLink()
}

func (a *App) showHover(st *editor.State, cmd bool, r hoverResult) {
	if cmd {
		if !a.hover.cmd {
			return
		}
		a.hideLink()
		st.TextBackgrounds = []editor.TextBackground{{Start: r.start, End: r.end, Color: a.colors().Link}}
		a.hover.link = st
	}
	st.ShowHover(r.start, r.end, "", func(c *ui.Context) { hoverContent(c, r.markdown, r.location) })
}

// hoverAt answers an editor's hover request: with the symbol's
// documentation from a language server, or with Cmd held its definition.
// It does not query servers in an untrusted workspace.
func (a *App) hoverAt(d *doc, st *editor.State, r editor.HoverRequest) {
	m := a.serverFor(d.path)
	if m == nil || d.buf == nil || !a.cfg.IsTrusted(a.root) {
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
	snap := d.buf.Snapshot()
	pos := snap.Pos(r.Offset)
	line := snap.Line(pos.Line)
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
		a.post(func() {
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

// hideLink takes the Cmd link out of the text.
func (a *App) hideLink() {
	if a.hover.link != nil {
		a.hover.link.TextBackgrounds = nil
		a.hover.link = nil
	}
}

// renderHover keeps the editor's selectable hover and gives it the app's
// popover colors and a subtle elevation.
func (a *App) renderHover(c *ui.Context, panel ui.Element, h *editor.HoverCard) {
	editor.DefaultHover(c, panel, h)
	panel.Radius(8).MaxWidth(620).Background(colors(c).Popover).Border(1, colors(c).PopoverEdge).
		Shadow(0, 3, 12, 0, ui.RGBA(0, 0, 0, 0.14))
}

func hoverContent(c *ui.Context, md string, location *hoverLocation) {
	ui.Column(c).MaxWidth(600).MaxHeight(420).Gap(8).Children(func() {
		if location != nil {
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Tooltip(fmt.Sprintf("%s:%d", location.path, location.line)).Children(func() {
				fileIcon(c, filepath.Base(location.path), 13)
				ui.Row(c).Gap(0).Shrink(1).Children(func() {
					ui.Text(c, filepath.Base(location.path)).FontSize(12).TextColor(colors(c).Text).SingleLine().Shrink(1)
					ui.Text(c, fmt.Sprintf(":%d", location.line)).FontSize(12).TextColor(colors(c).Text).SingleLine().Shrink(0)
				})
				parent := filepath.Base(filepath.Dir(location.path))
				if parent != "." && parent != string(filepath.Separator) {
					ui.Text(c, parent).FontSize(11).TextColor(colors(c).Muted).SingleLine().Shrink(1)
				}
			})
		}
		if md != "" {
			ui.Scroll(c).MaxHeight(380).Shrink(1).Label("Hover documentation").Children(func() {
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
