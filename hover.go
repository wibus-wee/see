package main

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

	"nsr/internal/lsp"
)

// hoverState follows the hovers asked of gopls: only the latest one
// shows, and while Cmd is held a hover is a link to the definition,
// underlaid in the text, with the definition's code in the card.
type hoverState struct {
	seq    uint64
	cancel context.CancelFunc
	cmd    bool          // Cmd held, as the last frame saw
	link   *editor.State // the editor showing a link
}

// hoverAt answers an editor's hover request: with the symbol's
// documentation from gopls, or with Cmd held its definition. It asks
// nothing of a workspace not yet trusted, nor of files but Go's.
func (a *app) hoverAt(d *doc, st *editor.State, r editor.HoverRequest) {
	if a.lsp == nil || d.buf == nil || !strings.EqualFold(filepath.Ext(d.path), ".go") || !a.cfg.trusted(a.root) {
		return
	}
	if s, _ := a.lsp.status(); s == lspFailed {
		return
	}
	if a.hover.cancel != nil {
		a.hover.cancel()
	}
	a.hover.seq++
	seq, gen, cmd := a.hover.seq, a.gen, a.hover.cmd
	snap := d.buf.Snapshot()
	pos := snap.Pos(r.Offset)
	line := snap.Line(pos.Line)
	ws, we := wordAt(line, pos.Col)
	if ws == we {
		return // not on a word
	}
	lineStart := snap.LineStart(pos.Line)
	ctx, cancel := context.WithTimeout(a.wsCtx, 10*time.Second)
	a.hover.cancel = cancel
	m, root := a.lsp, a.root
	go func() {
		defer cancel()
		c, err := m.get(ctx)
		if err != nil {
			return
		}
		if m.sync(ctx, c, d.uri, snap) != nil {
			return
		}
		enc := c.Encoding()
		at := lsp.Position{Line: pos.Line, Character: lsp.ColumnToLSP(line, pos.Col, enc)}
		start, end := lineStart+ws, lineStart+we
		var md string
		if cmd {
			locs, err := c.Definition(ctx, d.uri, at)
			targets, _ := targetsOf(locs)
			if err != nil || len(targets) == 0 {
				return
			}
			md = definitionPreview(root, targets[0])
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
			if a.gen != gen || a.hover.seq != seq {
				return // the pointer has moved on
			}
			if cmd {
				if !a.hover.cmd {
					return // Cmd let go meanwhile
				}
				a.hideLink()
				st.TextBackgrounds = []editor.TextBackground{{Start: start, End: end, Color: colLink}}
				a.hover.link = st
			}
			st.ShowHover(start, end, "", func(c *ui.Context) { hoverContent(c, md) })
		})
	}()
}

// hideLink takes the Cmd link out of the text.
func (a *app) hideLink() {
	if a.hover.link != nil {
		a.hover.link.TextBackgrounds = nil
		a.hover.link = nil
	}
}

// renderHover styles the editor's hover card as a popover: the editor's
// own card, rounder, with room for Markdown.
func (a *app) renderHover(c *ui.Context, panel ui.Element, h *editor.HoverCard) {
	editor.DefaultHover(c, panel, h)
	panel.Radius(10).MaxWidth(620).Background(colPopover).Border(1, colPopoverEdge).
		Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.45))
}

func hoverContent(c *ui.Context, md string) {
	ui.Column(c).MaxWidth(600).MaxHeight(420).Children(func() {
		ui.Scroll(c).Children(func() {
			markdown.View(c, md, &markdown.Options{WrapCode: true})
		})
	})
}

// definitionPreview is Markdown showing a definition's first lines,
// read from its file, under where it is.
func definitionPreview(root string, t target) string {
	data, err := os.ReadFile(t.path)
	if err != nil || len(data) > 4<<20 {
		return fmt.Sprintf("*%s:%d*", relPath(root, t.path), t.pos.Line+1)
	}
	lines := strings.Split(string(data), "\n")
	from := max(0, t.pos.Line-docLinesAbove(lines, t.pos.Line))
	to := min(len(lines), t.pos.Line+12)
	snippet := strings.TrimRight(strings.ReplaceAll(strings.Join(lines[from:to], "\n"), "\r", ""), "\n")
	lang := strings.TrimPrefix(filepath.Ext(t.path), ".")
	return fmt.Sprintf("*%s:%d*\n\n```%s\n%s\n```", relPath(root, t.path), t.pos.Line+1, lang, snippet)
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
