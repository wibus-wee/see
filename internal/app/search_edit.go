package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/search"
	"see/locales"
)

// searchEditTab is the tab of the find navigator's results as one
// editable document, as Zed's multibuffer: each file's matching lines,
// with a little context, edited in place and saved back to each file.
const searchEditTab = diffTabPrefix + "search"

// searchEditContext is how many lines show around a match.
const searchEditContext = 2

type searchEditView struct {
	opts    search.Options
	files   []*searchEditFile
	loading bool
	err     error
	note    string
	noteErr bool
	saving  bool
	scroll  ui.ScrollState
	matcher *regexp.Regexp
}

type searchEditFile struct {
	path     string
	excerpts []*searchExcerpt
	matches  int
	err      error
}

// searchExcerpt is lines first.. of a file as read, and the editor that
// edits them.
type searchExcerpt struct {
	first    int
	orig     []string
	st       *editor.State
	focus    ui.Handle
	bgsVer   uint64
	bgsValid bool
}

func searchEditText(key string) string { return locales.Catalog.Format(i18n.Locale(), key, nil) }

func searchEditCount(key string, n ...int) string {
	s := searchEditText(key)
	for _, v := range n {
		s = strings.Replace(s, "%d", fmt.Sprint(v), 1)
	}
	return s
}

// searchExcerptRanges merges the lines around matches into ranges of
// lines [start, end) of a file of lineCount lines.
func searchExcerptRanges(lines []int, lineCount, context int) [][2]int {
	lines = slices.Clone(lines)
	slices.Sort(lines)
	var out [][2]int
	for _, l := range lines {
		if l < 0 || l >= lineCount {
			continue
		}
		start, end := max(0, l-context), min(lineCount, l+context+1)
		if n := len(out); n > 0 && start <= out[n-1][1] {
			out[n-1][1] = max(out[n-1][1], end)
			continue
		}
		out = append(out, [2]int{start, end})
	}
	return out
}

// textLines splits a file's text into lines without their endings, and
// tells the ending it uses and whether its last line ends with one.
func textLines(text string) (lines []string, eol string, trailing bool) {
	eol = "\n"
	if strings.Contains(text, "\r\n") {
		eol = "\r\n"
		text = strings.ReplaceAll(text, "\r\n", "\n")
	}
	if text == "" {
		return nil, eol, false
	}
	trailing = strings.HasSuffix(text, "\n")
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n"), eol, trailing
}

// excerptLines are the lines an excerpt's editor holds: none when it
// was emptied.
func excerptLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

// lineEdit replaces lines first..first+len(old) of a file, which must
// still read old, with new.
type lineEdit struct {
	first    int
	old, new []string
}

// spliceLines applies edits, of disjoint ranges, to text, keeping its
// line endings; search.ErrStaleMatches when its lines are not the ones
// an edit was made over.
func spliceLines(text string, edits []lineEdit) (string, error) {
	lines, eol, trailing := textLines(text)
	edits = slices.Clone(edits)
	slices.SortFunc(edits, func(a, b lineEdit) int { return b.first - a.first })
	for _, e := range edits {
		end := e.first + len(e.old)
		if e.first < 0 || end > len(lines) || !slices.Equal(lines[e.first:end], e.old) {
			return "", search.ErrStaleMatches
		}
		lines = slices.Concat(lines[:e.first], e.new, lines[end:])
	}
	out := strings.Join(lines, eol)
	if trailing && len(lines) > 0 {
		out += eol
	}
	return out, nil
}

// searchMatcher finds the query in edited text as ripgrep did, for its
// highlights; nil when Go's regular expressions cannot.
func searchMatcher(o search.Options) *regexp.Regexp {
	q := o.Query
	if !o.Regexp {
		q = regexp.QuoteMeta(q)
	}
	if o.WholeWord {
		q = `\b(?:` + q + `)\b`
	}
	if !o.CaseSensitive {
		q = `(?i)` + q
	}
	re, err := regexp.Compile(q)
	if err != nil || o.Query == "" {
		return nil
	}
	return re
}

func (v *searchEditView) dirty() bool {
	for _, f := range v.files {
		if f.dirty() {
			return true
		}
	}
	return false
}

func (f *searchEditFile) dirty() bool {
	for _, x := range f.excerpts {
		if x.st.Buffer.Modified() {
			return true
		}
	}
	return false
}

// openSearchEdit shows the find navigator's results as an editable
// document, in the tab it has when one is open.
func (a *App) openSearchEdit() {
	f := &a.find
	if f.running || len(f.results) == 0 {
		return
	}
	for _, g := range a.groups() {
		if v := g.diffs[searchEditTab]; v != nil && g.has(searchEditTab) {
			if !v.search.dirty() && !v.search.saving {
				a.loadSearchEdit(v.search)
			}
			a.activateIn(g, searchEditTab)
			return
		}
	}
	g := a.focus
	if g.diffs == nil {
		g.diffs = map[string]*gitDiffTab{}
	}
	v := &searchEditView{}
	g.diffs[searchEditTab] = &gitDiffTab{name: searchEditText("searchEditTab"), search: v}
	g.addTab(searchEditTab, len(g.order))
	a.activateIn(g, searchEditTab)
	a.term.maximized = false
	a.loadSearchEdit(v)
}

// loadSearchEdit reads the files of the displayed results for their
// excerpts.
func (a *App) loadSearchEdit(v *searchEditView) {
	results := slices.Clone(a.find.results)
	v.opts, v.files, v.loading, v.err, v.note, v.noteErr = a.find.ran, nil, true, nil, "", false
	v.matcher = searchMatcher(v.opts)
	gen := a.gen
	go func() {
		type read struct {
			text string
			err  error
		}
		texts := make([]read, len(results))
		for i, r := range results {
			b, err := os.ReadFile(r.Path)
			texts[i] = read{string(b), err}
		}
		a.post.Post(func() {
			if a.gen != gen {
				return
			}
			v.loading = false
			for i, r := range results {
				file := &searchEditFile{path: r.Path, matches: len(r.Matches), err: texts[i].err}
				v.files = append(v.files, file)
				if file.err != nil {
					continue
				}
				lines, _, _ := textLines(texts[i].text)
				var at []int
				for _, m := range r.Matches {
					at = append(at, m.Line)
				}
				for _, rg := range searchExcerptRanges(at, len(lines), searchEditContext) {
					file.excerpts = append(file.excerpts, a.newSearchExcerpt(file, rg[0], lines[rg[0]:rg[1]]))
				}
			}
		})
	}()
}

func (a *App) newSearchExcerpt(f *searchEditFile, first int, lines []string) *searchExcerpt {
	x := &searchExcerpt{first: first, orig: slices.Clone(lines)}
	buf := editor.NewBuffer(strings.Join(lines, "\n"))
	buf.SetLanguage(languageFor(filepath.Base(f.path), ""))
	x.st = &editor.State{
		Buffer:          buf,
		Label:           fmt.Sprintf("%s:%d", a.workspacePath(f.path), first+1),
		Bindings:        editorBindings(),
		Theme:           a.edTheme,
		FitContent:      true,
		NoScrollbars:    true,
		NoFolding:       true,
		NoScrollPastEnd: true,
		NoIndentGuides:  true,
	}
	a.applyEditorPrefs(x.st)
	x.st.SoftWrap = false
	x.st.LineNumbers = editor.LineNumbersOff
	return x
}

// excerptBackgrounds highlights the query in an excerpt as it reads now.
func (v *searchEditView) excerptBackgrounds(x *searchExcerpt, color ui.Color) {
	ver := x.st.Buffer.Version()
	if x.bgsValid && x.bgsVer == ver {
		return
	}
	x.bgsValid, x.bgsVer = true, ver
	x.st.TextBackgrounds = x.st.TextBackgrounds[:0]
	if v.matcher == nil {
		return
	}
	text := x.st.Buffer.String()
	for _, m := range v.matcher.FindAllStringIndex(text, -1) {
		if m[1] > m[0] {
			x.st.TextBackgrounds = append(x.st.TextBackgrounds, editor.TextBackground{Start: m[0], End: m[1], Color: color})
		}
	}
}

// saveSearchEdit writes each edited file's excerpts back: into its
// open document, saved, or into the file on disk.
func (a *App) saveSearchEdit(v *searchEditView) {
	if v.saving || !v.dirty() {
		return
	}
	saved, failed := 0, 0
	var firstIssue string
	issue := func(path string, err error) {
		failed++
		if firstIssue == "" {
			firstIssue = replaceIssue(path, err)
		}
	}
	for _, f := range v.files {
		if !f.dirty() {
			continue
		}
		var edits []lineEdit
		for _, x := range f.excerpts {
			if x.st.Buffer.Modified() {
				edits = append(edits, lineEdit{first: x.first, old: x.orig, new: excerptLines(x.st.Buffer.String())})
			}
		}
		if d := a.docs[f.path]; d != nil && d.buf != nil {
			if a.replaceProtected(f.path) {
				failed++
				if firstIssue == "" {
					firstIssue = locales.MsgReplaceProtected(filepath.Base(f.path))
				}
				continue
			}
			text, err := spliceLines(d.buf.String(), edits)
			if err != nil {
				issue(f.path, err)
				continue
			}
			d.buf.Set(text)
			if !a.saveDoc(d, false) {
				d.buf.Undo()
				issue(f.path, errors.New(searchEditText("searchEditSaveFailed")))
				continue
			}
		} else if err := a.writeSearchEdits(f.path, edits); err != nil {
			issue(f.path, err)
			continue
		}
		f.settle()
		saved++
	}
	v.noteErr = failed > 0
	v.note = searchEditCount("searchEditSaved", saved)
	if failed > 0 {
		v.note += " " + locales.MsgWorkspaceReplaceSkipped(failed) + " " + firstIssue
	}
	if saved > 0 {
		a.invalidateHover()
		a.refreshGit()
		if a.find.ran == v.opts {
			a.runFind()
		}
	}
}

// settle makes a file's excerpts, as saved, the lines they edit.
func (f *searchEditFile) settle() {
	shift := 0
	for _, x := range f.excerpts {
		x.first += shift
		lines := excerptLines(x.st.Buffer.String())
		shift += len(lines) - len(x.orig)
		x.orig = lines
		x.st.Buffer.MarkSaved()
	}
}

// writeSearchEdits writes edits to a file no document has open, once
// its lines are still those read.
func (a *App) writeSearchEdits(path string, edits []lineEdit) error {
	before, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	bom := strings.HasPrefix(string(before), "\ufeff")
	text := strings.TrimPrefix(string(before), "\ufeff")
	after, err := spliceLines(text, edits)
	if err != nil {
		return err
	}
	if bom {
		after = "\ufeff" + after
	}
	return writeFileAtomically(path, func(w io.Writer) error {
		_, err := io.WriteString(w, after)
		return err
	})
}

// revertSearchEdit takes back every unsaved edit of the results.
func (v *searchEditView) revert() {
	for _, f := range v.files {
		for _, x := range f.excerpts {
			if x.st.Buffer.Modified() {
				x.st.Buffer.Set(strings.Join(x.orig, "\n"))
				x.st.Buffer.MarkSaved()
			}
		}
	}
	v.note, v.noteErr = "", false
}

// excerptAfter is the excerpt following x in the results, or before it
// with dir -1.
func (v *searchEditView) excerptAfter(x *searchExcerpt, dir int) *searchExcerpt {
	var all []*searchExcerpt
	for _, f := range v.files {
		all = append(all, f.excerpts...)
	}
	i := slices.Index(all, x) + dir
	if i < 0 || i >= len(all) {
		return nil
	}
	return all[i]
}

func (a *App) searchEditContent(c *ui.Context, g *group, v *searchEditView) {
	switch {
	case v.loading:
		ui.Row(c).Grow(1).FillWidth().Center().Gap(8).Children(func() {
			ui.Spinner(c)
			ui.Text(c, searchEditText("searchEditReading")).FontSize(13).TextColor(colors(c).Muted)
		})
		return
	case len(v.files) == 0:
		ui.Column(c).Grow(1).Fill().Center().Children(func() {
			ui.Text(c, searchEditText("searchEditEmpty")).FontSize(14).TextColor(colors(c).Muted)
		})
		return
	}
	match := colors(c).Match
	ui.Scroll(c.Key(v)).Grow(1).FillWidth().TrackScroll(&v.scroll).Label(searchEditText("searchEditTab")).Children(func() {
		ui.Column(c).FillWidth().Children(func() {
			for _, f := range v.files {
				ui.Column(c.Key(f)).FillWidth().Children(func() {
					a.searchEditHeader(c, g, v, f)
					if f.err != nil {
						ui.Text(c, describeErr(f.err)).FontSize(12).TextColor(colors(c).Err).Padding(4, 10)
						return
					}
					for i, x := range f.excerpts {
						if i > 0 {
							ui.Row(c).FillWidth().Height(14).Padding(0, 10).AlignItems(ui.Center).Background(colors(c).TabBar).Children(func() {
								ui.Text(c, "⋯").FontSize(10).TextColor(colors(c).Dim).SingleLine()
							})
						}
						v.excerptBackgrounds(x, match)
						a.searchExcerptView(c, g, v, f, x)
					}
				})
			}
		})
	})
}

func (a *App) searchEditHeader(c *ui.Context, g *group, v *searchEditView, f *searchEditFile) {
	header := ui.Row(c).FillWidth().Height(26).Padding(0, 10).Gap(6).AlignItems(ui.Center).
		Background(colors(c).TabBar).BorderWidth(1, 0, 1, 0).BorderColor(colors(c).Hairline).
		Cursor(ui.CursorPointer).Label(f.path).Tooltip(searchEditText("searchEditOpenFile"))
	header.OnClick(func() {
		a.observeFocusedGroup(g)
		var pos *editor.Pos
		if len(f.excerpts) > 0 {
			pos = &editor.Pos{Line: f.excerpts[0].first}
		}
		a.openIn(g, f.path, goTo{pos: pos})
	})
	header.Children(func() {
		fileIcon(c, filepath.Base(f.path), 14)
		ui.Text(c, filepath.Base(f.path)).FontSize(12).Bold().SingleLine().Shrink(0)
		if dir := filepath.Dir(a.workspacePath(f.path)); dir != "." {
			ui.Text(c, filepath.ToSlash(dir)).FontSize(11).TextColor(colors(c).Dim).SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0).Shrink(1)
		}
		if f.dirty() {
			ui.Text(c, "●").FontSize(10).TextColor(c.Theme().Accent).Tooltip(searchEditText("searchEditEdited"))
		}
		ui.Box(c).Grow(1)
		if n := v.fileMatches(f); n > 0 {
			ui.Text(c, fmt.Sprint(n)).FontSize(11).TextColor(colors(c).Muted).Padding(0, 6).Radius(8).Background(colors(c).Pill)
		}
	})
}

// searchExcerptView is an excerpt's editor, numbered with the file's
// lines in a gutter of its own.
func (a *App) searchExcerptView(c *ui.Context, g *group, v *searchEditView, f *searchEditFile, x *searchExcerpt) {
	st := x.st
	lines := st.Buffer.LineCount()
	rowH := float32(st.LineTop(1) - st.LineTop(0))
	if rowH <= 0 {
		rowH = float32(a.cfg.EditorFontSize()) * 1.5
	}
	last := f.excerpts[len(f.excerpts)-1]
	digits := len(fmt.Sprint(last.first + last.st.Buffer.LineCount()))
	st.OnInput = func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputKeyDown || ev.Mods != 0 {
			return false
		}
		pos := st.CursorPos()
		var next *searchExcerpt
		switch {
		case ev.Key == ui.KeyDown && pos.Line == st.Buffer.LineCount()-1:
			next = v.excerptAfter(x, 1)
			if next != nil {
				next.st.GoTo(editor.Pos{Line: 0, Col: pos.Col})
			}
		case ev.Key == ui.KeyUp && pos.Line == 0:
			next = v.excerptAfter(x, -1)
			if next != nil {
				next.st.GoTo(editor.Pos{Line: next.st.Buffer.LineCount() - 1, Col: pos.Col})
			}
		}
		if next == nil {
			return false
		}
		next.focus.Focus(c)
		return true
	}
	ui.Row(c.Key(x)).FillWidth().AlignItems(ui.Start).Children(func() {
		gutter := ui.Column(c).Width(float32(digits)*7.5+16).Shrink(0).Padding(0, 8, 0, 4)
		gutter.Children(func() {
			for i := range lines {
				line := x.first + i
				n := ui.Row(c).Height(rowH).FillWidth().Justify(ui.End).AlignItems(ui.Center).Cursor(ui.CursorPointer).
					Tooltip(searchEditText("searchEditOpenLine"))
				n.OnClick(func() {
					a.observeFocusedGroup(g)
					a.openIn(g, f.path, goTo{pos: &editor.Pos{Line: line}})
				})
				n.Children(func() {
					ui.Text(c, fmt.Sprint(line+1)).FontSize(11).TextColor(colors(c).Dim).SingleLine()
				})
			}
		})
		view := editor.View(c, st).Grow(1).Bind(&x.focus)
		if view.FocusWithin() {
			a.observeFocusedGroup(g)
		}
	})
}

func (a *App) searchEditStatusBar(c *ui.Context, g *group, v *searchEditView) {
	ui.Row(c).FillWidth().Height(26).Padding(0, 8).Gap(7).AlignItems(ui.Center).
		BorderWidth(1, 0, 0, 0).BorderColor(colors(c).Separator).Background(colors(c).Editor).Children(func() {
		edited := 0
		for _, f := range v.files {
			if f.dirty() {
				edited++
			}
		}
		switch {
		case v.note != "" && edited == 0:
			color := colors(c).Muted
			if v.noteErr {
				color = colors(c).Err
			}
			ui.Text(c, v.note).FontSize(11).TextColor(color).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1).Role(ui.RoleStatus)
		case edited > 0:
			ui.Text(c, searchEditCount("searchEditEditedFiles", edited)).FontSize(11).TextColor(c.Theme().Accent).SingleLine()
		default:
			ui.Text(c, searchEditText("searchEditHint")).FontSize(11).TextColor(colors(c).Muted).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
		}
		ui.Box(c).Grow(1)
		ui.Button(c, searchEditText("searchEditRevert")).FontSize(11).Height(20).Padding(1, 7).Disabled(edited == 0).KeepFocus().
			OnClick(func() { v.revert() })
		ui.PrimaryButton(c, searchEditText("searchEditSave")).FontSize(11).Height(20).Padding(1, 7).Disabled(edited == 0).KeepFocus().
			Tooltip(keyLabel("Cmd+", "S")).OnClick(func() { a.saveSearchEdit(v) })
	})
}

func (v *searchEditView) summary() string {
	matches, files := v.matchCount()
	return searchEditCount("searchEditSummary", matches, files)
}

// fileMatches counts the query in a file's excerpts as they read now.
func (v *searchEditView) fileMatches(f *searchEditFile) int {
	if v.matcher == nil {
		return f.matches
	}
	n := 0
	for _, x := range f.excerpts {
		n += len(v.matcher.FindAllStringIndex(x.st.Buffer.String(), -1))
	}
	return n
}

// matchCount counts the matches, and the files with any, as the
// excerpts read now.
func (v *searchEditView) matchCount() (matches, files int) {
	for _, f := range v.files {
		if n := v.fileMatches(f); n > 0 {
			matches += n
			files++
		}
	}
	return matches, files
}
