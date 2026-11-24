package app

import (
	"see/locales"
	"strings"
	"unicode"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type whitespaceDiffLine struct {
	text  string
	noEOL bool
}

func (l whitespaceDiffLine) appendTo(lines []string, prefix string) []string {
	lines = append(lines, prefix+l.text)
	if l.noEOL {
		lines = append(lines, "\\ No newline at end of file")
	}
	return lines
}

func (l whitespaceDiffLine) normalized() string {
	return " " + strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, l.text)
}

// whitespaceDiffLines uses the usual line comparison on normalized copies.
// Matching lines become context on both sides with the new side's spelling.
func whitespaceDiffLines(lines []string, old, new []whitespaceDiffLine) []string {
	normalized := func(src []whitespaceDiffLine) *editor.Buffer {
		var b strings.Builder
		for _, l := range src {
			b.WriteString(l.normalized())
			if !l.noEOL {
				b.WriteByte('\n')
			}
		}
		return editor.NewBuffer(b.String())
	}
	d := &editor.DiffState{Old: normalized(old), New: normalized(new)}
	n := 0
	for _, h := range d.Hunks() {
		for _, l := range new[n:h.NewStart] {
			lines = l.appendTo(lines, " ")
		}
		for _, l := range old[h.OldStart:h.OldEnd] {
			lines = l.appendTo(lines, "-")
		}
		for _, l := range new[h.NewStart:h.NewEnd] {
			lines = l.appendTo(lines, "+")
		}
		n = h.NewEnd
	}
	for _, l := range new[n:] {
		lines = l.appendTo(lines, " ")
	}
	return lines
}

// withoutWhitespaceChanges keeps the patch's file coordinates and context;
// only changed runs need to be compared again.
func withoutWhitespaceChanges(p editor.FilePatch) editor.FilePatch {
	out := p
	out.Hunks = make([]editor.PatchHunk, 0, len(p.Hunks))
	for _, h := range p.Hunks {
		lines := make([]string, 0, len(h.Lines))
		for i := 0; i < len(h.Lines); {
			l := h.Lines[i]
			if l == "" || (l[0] != '-' && l[0] != '+') {
				lines = append(lines, l)
				i++
				continue
			}
			var old, new []whitespaceDiffLine
			for i < len(h.Lines) {
				l = h.Lines[i]
				if l == "" || (l[0] != '-' && l[0] != '+') {
					break
				}
				i++
				line := whitespaceDiffLine{text: strings.TrimSuffix(l[1:], "\r")}
				if i < len(h.Lines) && strings.HasPrefix(h.Lines[i], "\\") {
					line.noEOL = true
					i++
				}
				if l[0] == '-' {
					old = append(old, line)
				} else {
					new = append(new, line)
				}
			}
			lines = whitespaceDiffLines(lines, old, new)
		}
		h.Lines = lines
		out.Hunks = append(out.Hunks, h)
	}
	return out
}

func setBufferDiffWhitespace(d *editor.DiffState, old, new *editor.Buffer, hide bool) {
	if !hide {
		d.Old, d.New = old, new
		return
	}
	lines := func(b *editor.Buffer) []whitespaceDiffLine {
		s := b.Snapshot()
		var out []whitespaceDiffLine
		for line := range strings.SplitAfterSeq(s.String(), "\n") {
			if line != "" {
				text, hasEOL := strings.CutSuffix(line, "\n")
				out = append(out, whitespaceDiffLine{text: text, noEOL: !hasEOL})
			}
		}
		return out
	}
	from, to := lines(old), lines(new)
	h := editor.PatchHunk{
		OldLines: len(from), NewLines: len(to),
		Lines: whitespaceDiffLines(nil, from, to),
	}
	if len(from) > 0 {
		h.OldStart = 1
	}
	if len(to) > 0 {
		h.NewStart = 1
	}
	p := editor.FilePatch{Hunks: []editor.PatchHunk{h}}
	d.SetPatch(p)
	d.LineSelection = false
	d.Old.SetLanguage(old.Language())
	d.New.SetLanguage(new.Language())
}

func (a *App) setDiffPatch(d *editor.DiffState, p editor.FilePatch, full bool) {
	if a.cfg.HideWhitespaceChanges {
		p = withoutWhitespaceChanges(p)
	}
	if full {
		setPatch(d, p)
	} else {
		d.SetPatch(p)
	}
}

func (a *App) updateDiffWhitespace() {
	if a.diffWhitespaceHidden == a.cfg.HideWhitespaceChanges {
		return
	}
	a.diffWhitespaceHidden = a.cfg.HideWhitespaceChanges
	for _, g := range a.groups() {
		for _, v := range g.diffs {
			switch {
			case v.localHistory != nil && v.state != nil:
				a.selectLocalHistorySnapshot(v, v.localHistory.selected)
			case v.oldBuffer != nil:
				if a.cfg.HideWhitespaceChanges {
					setBufferDiffWhitespace(v.state, v.oldBuffer, v.newBuffer, true)
				} else {
					d := &editor.DiffState{Old: v.oldBuffer, New: v.newBuffer, Theme: a.edTheme}
					a.applyDiffPrefs(d)
					v.state = d
				}
			case v.state != nil:
				a.setDiffPatch(v.state, v.patch, true)
			case v.log != nil && v.log.detail != nil:
				for _, f := range v.log.detail.files {
					if f.diff != nil {
						a.setDiffPatch(f.diff, f.patch, false)
					}
				}
			case v.branch != nil:
				for _, f := range v.branch.files {
					if f.diff != nil {
						a.setDiffPatch(f.diff, f.patch, true)
					}
				}
			}
		}
	}
}

func (a *App) toggleDiffWhitespace() {
	a.cfg.HideWhitespaceChanges = !a.cfg.HideWhitespaceChanges
	a.updateDiffWhitespace()
	a.settingsChanged()
}

func (a *App) diffChangesHidden(d *editor.DiffState) bool {
	return a.cfg.HideWhitespaceChanges && len(d.Hunks()) == 0
}

func (a *App) hiddenWhitespaceDiff(c *ui.Context) {
	ui.Row(c).Grow(1).FillWidth().Center().Padding(8).Children(func() {
		ui.Text(c, diffWhitespaceText("msgNoChangesWithWhitespaceHidden")).FontSize(12).TextColor(colors(c).Muted)
	})
}

func diffWhitespaceText(key string) string {
	return locales.Catalog.Format(i18n.Locale(), key, nil)
}

func (a *App) diffStats(d *editor.DiffState, added, deleted int) (int, int) {
	if a.cfg.HideWhitespaceChanges && d != nil {
		return d.Stats()
	}
	return added, deleted
}

func (a *App) gitPeekDiff(peek *gitPeek, st *editor.State) (*editor.DiffState, bool) {
	if !a.cfg.HideWhitespaceChanges {
		if peek.filtered != nil {
			peek.filtered = nil
			peek.scroll.Y = diffHunkOffset(st, peek.diff, gitHunkAtLine(peek.diff, peek.line))
		}
		return peek.diff, true
	}
	version := peek.doc.buf.Version()
	if peek.filtered == nil || peek.filteredVersion != version {
		d := &editor.DiffState{Theme: a.edTheme, FitContent: true}
		setBufferDiffWhitespace(d, peek.diff.Old, peek.doc.buf, true)
		a.applyDiffPrefs(d)
		peek.filtered, peek.filteredVersion = d, version
		peek.filteredHunk = -1
		if i := gitHunkAtLine(peek.diff, peek.line); i >= 0 {
			raw := peek.diff.Hunks()[i]
			for j, h := range d.Hunks() {
				if h.NewStart < max(raw.NewEnd, raw.NewStart+1) && raw.NewStart < max(h.NewEnd, h.NewStart+1) {
					d.GoToHunk(j)
					peek.scroll.Y = diffHunkOffset(st, d, j)
					peek.filteredHunk = j
					break
				}
			}
		}
	}
	return peek.filtered, peek.filteredHunk >= 0
}
