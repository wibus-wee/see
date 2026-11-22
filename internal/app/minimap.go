package app

import (
	"image"
	"image/color"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/locales"
)

// The minimap draws each line of the file minimapRow DIPs high and each
// character minimapCol DIPs wide, as Xcode's does, from a bitmap of the
// lines around the view that a goroutine renders.
const (
	minimapWidth     = 116
	minimapPadX      = 10
	minimapRow       = 3
	minimapInk       = 2
	minimapCol       = 1
	minimapDensity   = 2
	minimapMaxLines  = 1200
	minimapMaxMarks  = 20000
	minimapMinEditor = 480
)

type minimapFind struct {
	query                      string
	caseSens, wholeWord, regex bool
}

type minimap struct {
	bmp      *ui.Bitmap
	from, to int
	version  uint64
	themeGen int // the theme generation the bitmap was drawn in
	busy     bool

	find       minimapFind
	matches    []int
	matchKey   minimapFind
	matchVer   uint64
	matchBusy  bool
	diagLines  []minimapMark
	diagKey    uint64
	diagSource []editor.Diagnostic

	h          float32
	room       float32
	drag       bool
	grab       float32
	hover      bool
	lastLayout minimapLayout
}

type minimapMark struct {
	line int
	c    ui.Color
}

// minimapLayout places the file in a minimap h DIPs high: the minimap
// shows the file from line off/minimapRow down, and the editor's view is
// the box from boxY, boxH high. As the editor scrolls from top to bottom,
// the box and the file slide together, as Xcode's minimap does.
type minimapLayout struct {
	off, boxY, boxH, track float32
	maxScroll              float64
}

func layoutMinimap(lines int, h float32, scrollY, viewH, total float64) minimapLayout {
	content := float32(lines) * minimapRow
	l := minimapLayout{track: min(h, content), maxScroll: max(0, total-viewH)}
	l.boxH = l.track
	if total > 0 {
		l.boxH = min(l.track, float32(viewH/total)*content)
	}
	l.boxH = max(l.boxH, min(l.track, 2*minimapRow))
	ratio := float32(0)
	if l.maxScroll > 0 {
		ratio = float32(min(max(scrollY/l.maxScroll, 0), 1))
	}
	l.boxY = ratio * (l.track - l.boxH)
	l.off = ratio * max(0, content-h)
	return l
}

// scrollFor is the editor's scroll that puts the box at boxY.
func (l minimapLayout) scrollFor(boxY float32) float64 {
	if l.track <= l.boxH {
		return 0
	}
	return float64(min(max(boxY/(l.track-l.boxH), 0), 1)) * l.maxScroll
}

// showsMinimap reports whether the editor of d in g shows the minimap:
// unless the settings hide it, while the editor and the minimap had room
// side by side the last frame.
func (a *App) showsMinimap(g *group, d *doc) bool {
	m := g.minimaps[d.path]
	return !a.cfg.HideMinimap && (m == nil || m.room == 0 || m.room >= minimapMinEditor+minimapWidth)
}

func (g *group) minimapOf(path string) *minimap {
	if g.minimaps == nil {
		g.minimaps = map[string]*minimap{}
	}
	m := g.minimaps[path]
	if m == nil {
		m = &minimap{}
		g.minimaps[path] = m
	}
	return m
}

// minimap builds the minimap beside an editor: what it shows comes from
// the bitmap and marks the last frames made ready, and a goroutine renders
// the lines the view moves to.
func (a *App) minimap(c *ui.Context, g *group, d *doc, st *editor.State) {
	m := g.minimapOf(d.path)
	st.RenderFindBar = func(c *ui.Context, bar ui.Element, f *editor.FindBar) {
		m.find = minimapFind{*f.Query(), *f.CaseSensitive(), *f.WholeWord(), *f.Regexp()}
		editor.DefaultFindBar(c, bar, f)
	}
	lines := d.buf.LineCount()
	version := d.buf.Version()
	l := layoutMinimap(lines, m.h, st.ScrollY(), st.ViewportHeight(), st.LineTop(lines))
	m.lastLayout = l
	first := int(l.off / minimapRow)
	last := min(lines, first+int(m.h/minimapRow)+1)
	if m.h > 0 && !m.busy && (m.bmp == nil || m.version != version || m.themeGen != a.themeGen || first < m.from || last > m.to) {
		a.renderMinimap(m, d, first, last)
	}
	if count, _ := st.FindMatches(); count == 0 {
		m.find = minimapFind{}
	}
	if !m.matchBusy && (m.find != m.matchKey || m.find.query != "" && m.matchVer != version) {
		a.findMinimapMatches(m, d)
	}
	a.minimapDiagnostics(m, d)

	t := a.edTheme
	box := ui.Box(c.Key(struct {
		minimap string
	}{d.path})).Width(minimapWidth).Shrink(0).FillHeight().Background(t.Background).
		Label(locales.MinimapLabel()).Role(ui.RoleSlider)
	_, _, m.hover = box.PointerPosition()
	box.HandleInput(func(ev ui.InputEvent) bool {
		l := m.lastLayout
		switch ev.Kind {
		case ui.InputPointerDown:
			if ev.Button != 0 {
				return false
			}
			m.drag = true
			if ev.Y >= l.boxY && ev.Y < l.boxY+l.boxH {
				m.grab = ev.Y - l.boxY
			} else {
				m.grab = l.boxH / 2
				st.SetScrollY(l.scrollFor(ev.Y - m.grab))
			}
			return true
		case ui.InputPointerMove:
			if !m.drag {
				return false
			}
			st.SetScrollY(l.scrollFor(ev.Y - m.grab))
			return true
		case ui.InputPointerUp:
			if !m.drag {
				return false
			}
			m.drag = false
			return true
		case ui.InputScroll:
			st.SetScrollY(min(max(st.ScrollY()+float64(ev.DY), 0), l.maxScroll))
			return true
		}
		return false
	})
	var hunks []editor.DiffHunk
	if d.gitChanges != nil {
		hunks = d.gitChanges.Hunks()
	}
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		m.h = r.H
		p.Fill(ui.Rect{X: r.X, Y: r.Y, W: 1, H: r.H}, t.Text.Alpha(0.06), 0)
		p.Clip(r, 0, func() { a.paintMinimap(p, r, m, l, lines, hunks) })
	})
}

func (a *App) paintMinimap(p *ui.Painter, r ui.Rect, m *minimap, l minimapLayout, lines int, hunks []editor.DiffHunk) {
	t := a.edTheme
	y0 := r.Y - l.off
	from := int(l.off / minimapRow)
	to := min(lines, from+int(r.H/minimapRow)+2)
	rowRect := func(line int, x, w float32) ui.Rect {
		return ui.Rect{X: r.X + x, Y: y0 + float32(line)*minimapRow, W: w, H: minimapRow}
	}
	if m.bmp != nil && m.themeGen == a.themeGen {
		bw, bh := m.bmp.Size()
		p.ImageRegion(m.bmp, ui.Rect{W: float32(bw), H: float32(bh)},
			ui.Rect{X: r.X, Y: y0 + float32(m.from)*minimapRow, W: float32(bw) / minimapDensity, H: float32(bh) / minimapDensity}, ui.ImageOptions{})
	}
	for _, mk := range m.diagLines {
		if mk.line >= from && mk.line < to {
			p.Fill(rowRect(mk.line, 1, r.W-1), mk.c.Alpha(0.28), 0)
		}
	}
	if m.matchKey == m.find && m.find.query != "" {
		i, _ := slices.BinarySearch(m.matches, from)
		for ; i < len(m.matches) && m.matches[i] < to; i++ {
			p.Fill(rowRect(m.matches[i], 1, r.W-1), t.CurrentMatch.Alpha(0.45), 0)
		}
	}
	for i, h := range hunks {
		if i == minimapMaxMarks {
			break
		}
		c := t.Modified
		end := h.NewEnd
		switch {
		case h.OldStart == h.OldEnd:
			c = t.Added
		case h.NewStart == h.NewEnd:
			c, end = t.Deleted, h.NewStart+1
		}
		if end <= from || h.NewStart >= to {
			continue
		}
		top := y0 + float32(h.NewStart)*minimapRow
		p.Fill(ui.Rect{X: r.X + 2, Y: top, W: 2, H: max(float32(end-h.NewStart)*minimapRow, 2)}, c, 0)
	}
	alpha := float32(0.08)
	if m.hover || m.drag {
		alpha = 0.14
	}
	p.Fill(ui.Rect{X: r.X + 1, Y: r.Y + l.boxY, W: r.W - 1, H: l.boxH}, t.Text.Alpha(alpha), 0)
}

func (a *App) minimapDiagnostics(m *minimap, d *doc) {
	key := d.diagnosticVersion
	if key == m.diagKey && len(d.diagnostics) == len(m.diagSource) && (len(d.diagnostics) == 0 || &d.diagnostics[0] == &m.diagSource[0]) {
		return
	}
	m.diagKey, m.diagSource, m.diagLines = key, d.diagnostics, m.diagLines[:0]
	if d.buf.Version() != d.diagnosticVersion {
		return
	}
	snap := d.buf.Snapshot()
	t := a.edTheme
	for i, dg := range d.diagnostics {
		if i == minimapMaxMarks {
			break
		}
		c := t.Hint
		switch dg.Severity {
		case editor.SeverityError:
			c = t.Error
		case editor.SeverityWarning:
			c = t.Warning
		case editor.SeverityInfo:
			c = t.Info
		}
		m.diagLines = append(m.diagLines, minimapMark{snap.LineOf(min(max(dg.Start, 0), snap.Len())), c})
	}
}

func (a *App) renderMinimap(m *minimap, d *doc, first, last int) {
	from := max(0, first-minimapMaxLines/4)
	to := min(d.buf.LineCount(), max(last, from+minimapMaxLines))
	from = max(0, min(from, to-minimapMaxLines))
	snap := d.buf.Snapshot()
	// The theme is drawn from a copy: applyAppearance replaces the
	// shared object's contents while the bitmap renders off thread.
	t, gen := *a.edTheme, a.themeGen
	var lang *editor.Language
	if !d.plain {
		lang = d.buf.Language()
	}
	m.busy = true
	go func() {
		img := drawMinimap(snap, lang, &t, from, to)
		a.post.Post(func() {
			m.busy = false
			m.bmp, m.from, m.to, m.version, m.themeGen = ui.NewBitmap(img), from, to, snap.Version(), gen
		})
	}()
}

// drawMinimap draws lines from to to of text as blocks of their tokens'
// colors, minimapDensity pixels to the DIP.
func drawMinimap(text editor.Snapshot, lang *editor.Language, t *editor.Theme, from, to int) *image.RGBA {
	const cols = (minimapWidth - minimapPadX - 4) / minimapCol
	img := image.NewRGBA(image.Rect(0, 0, minimapWidth*minimapDensity, max(1, to-from)*minimapRow*minimapDensity))
	var hl editor.Highlighter
	switch {
	case lang == nil:
	case lang.Tokenizer != nil:
		hl = editor.NewTokenHighlighter(lang.Tokenizer, text)
	case lang.Highlighter != nil:
		hl = lang.Highlighter(text, func() {})
	}
	if hl != nil && !hl.Highlight(to-1, 2*time.Second) {
		hl = nil
	}
	tab := 4
	if lang != nil && lang.TabSize > 0 {
		tab = lang.TabSize
	}
	plain := t.Text
	var toks []editor.Token
	for i := from; i < to; i++ {
		line := text.Line(i)
		toks = toks[:0]
		if hl != nil {
			toks, _ = hl.Tokens(i, line, toks)
		}
		y := (i - from) * minimapRow * minimapDensity
		col, tok := 0, 0
		for off := 0; off < len(line) && col < cols; {
			r, n := utf8.DecodeRuneInString(line[off:])
			for tok < len(toks) && toks[tok].End <= off {
				tok++
			}
			switch r {
			case ' ':
				col++
			case '\t':
				col += tab - col%tab
			default:
				c := plain
				if tok < len(toks) {
					c = t.TokenColor(toks[tok].Kind)
				}
				fillMinimap(img, minimapPadX+col*minimapCol, y, c)
				col++
			}
			off += n
		}
	}
	return img
}

func fillMinimap(img *image.RGBA, x, y int, c ui.Color) {
	const a = 0.6
	px := color.RGBA{uint8(float32(c.R) * a), uint8(float32(c.G) * a), uint8(float32(c.B) * a), uint8(255 * a)}
	for dy := range minimapInk * minimapDensity {
		for dx := range minimapCol * minimapDensity {
			img.SetRGBA(x*minimapDensity+dx, y+dy, px)
		}
	}
}

func (a *App) findMinimapMatches(m *minimap, d *doc) {
	key, snap := m.find, d.buf.Snapshot()
	if key.query == "" {
		m.matchKey, m.matches = key, nil
		return
	}
	m.matchBusy = true
	go func() {
		lines := minimapMatches(snap, key)
		a.post.Post(func() {
			m.matchBusy = false
			m.matchKey, m.matchVer, m.matches = key, snap.Version(), lines
		})
	}()
}

// minimapMatches returns the lines that the find bar's query matches in,
// for queries of one line.
func minimapMatches(text editor.Snapshot, f minimapFind) []int {
	pat := f.query
	if !f.regex {
		if strings.Contains(pat, "\n") {
			return nil
		}
		pat = regexp.QuoteMeta(pat)
	}
	if f.wholeWord {
		pat = `\b(?:` + pat + `)\b`
	}
	if !f.caseSens {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil
	}
	var plain, lower string
	fast := !f.regex && !f.wholeWord
	if fast {
		plain = f.query
		if !f.caseSens {
			lower = strings.ToLower(plain)
		}
	}
	var out []int
	for i := range text.LineCount() {
		line := text.Line(i)
		var hit bool
		switch {
		case fast && f.caseSens:
			hit = strings.Contains(line, plain)
		case fast:
			hit = strings.Contains(strings.ToLower(line), lower)
		default:
			hit = re.MatchString(line)
		}
		if hit {
			out = append(out, i)
			if len(out) == minimapMaxMarks {
				break
			}
		}
	}
	return out
}
