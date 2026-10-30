package main

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
	"see/internal/nav"
	"see/internal/search"
	uriutil "see/internal/uri"

	"see/internal/langserver"
)

// ----- The find navigator: text in the workspace, with ripgrep -----

// findState is the find navigator's: its query and options, and the
// results streaming in from ripgrep.
type findState struct {
	opts        search.Options
	field       ui.Handle
	focus       bool
	showFilters bool
	running     bool
	seq         int
	cancel      context.CancelFunc
	timer       *time.Timer
	ran         search.Options // what the results are of
	results     []search.File  // by path
	stats       search.Stats
	err         error
	rg          string // ripgrep's path, found once per workspace
	rgErr       error
	tree        ui.OutlineState[findKey]
	row         int
}

// findKey is a row of the results: a file, or one of its matches.
type findKey struct {
	path  string
	match int // -1 for the file's row
}

func (f *findState) init() {
	f.row = -1
	f.tree.List.Selected = &f.row
}

func (f *findState) reset() {
	if f.cancel != nil {
		f.cancel()
	}
	if f.timer != nil {
		f.timer.Stop()
	}
	opts := f.opts
	*f = findState{opts: opts}
	f.init()
}

// findSoon searches once typing stops for a moment.
func (a *app) findSoon() {
	if a.find.timer != nil {
		a.find.timer.Stop()
	}
	a.find.timer = time.AfterFunc(250*time.Millisecond, func() { a.post(a.runFind) })
}

// runFind searches the workspace for the query, dropping the search on
// its way; results come in batches as ripgrep finds them.
func (a *app) runFind() {
	f := &a.find
	if f.timer != nil {
		f.timer.Stop()
		f.timer = nil
	}
	if f.cancel != nil {
		f.cancel()
		f.cancel = nil
	}
	f.seq++
	f.results, f.stats, f.err = nil, search.Stats{}, nil
	f.tree.Open = ui.Selection[findKey]{}
	f.row = -1
	f.ran = f.opts
	if strings.TrimSpace(f.opts.Query) == "" || a.root == "" {
		f.running = false
		return
	}
	if f.rg == "" && f.rgErr == nil {
		f.rg, f.rgErr = findRipgrep(a.root)
	}
	if f.rgErr != nil {
		f.err = f.rgErr
		f.running = false
		return
	}
	seq, gen, opts, root, rg := f.seq, a.gen, f.opts, a.root, f.rg
	ctx, cancel := context.WithCancel(a.wsCtx)
	f.cancel = cancel
	f.running = true
	go func() {
		defer cancel()
		var mu sync.Mutex
		var batch []search.File
		last := time.Now()
		flush := func() {
			mu.Lock()
			b := batch
			batch = nil
			mu.Unlock()
			if len(b) == 0 {
				return
			}
			a.post(func() {
				if a.gen == gen && a.find.seq == seq {
					a.addFindResults(b)
				}
			})
		}
		st, err := search.Run(ctx, rg, root, opts, func(file search.File) {
			mu.Lock()
			batch = append(batch, file)
			mu.Unlock()
			if time.Since(last) > 80*time.Millisecond {
				last = time.Now()
				flush()
			}
		})
		flush()
		a.post(func() {
			if a.gen != gen || a.find.seq != seq {
				return
			}
			a.find.running, a.find.cancel = false, nil
			a.find.stats = st
			if err != nil && !errors.Is(err, context.Canceled) {
				a.find.err = err
			}
		})
	}()
}

// addFindResults puts files of results in path order, open.
func (a *app) addFindResults(files []search.File) {
	f := &a.find
	for _, file := range files {
		i, _ := slices.BinarySearchFunc(f.results, file.Path, func(r search.File, p string) int { return strings.Compare(r.Path, p) })
		f.results = slices.Insert(f.results, i, file)
		f.tree.Open.Add(findKey{path: file.Path, match: -1})
		f.stats.Files++
		f.stats.Matches += len(file.Matches)
	}
}

func (a *app) findFile(path string) *search.File {
	i, ok := slices.BinarySearchFunc(a.find.results, path, func(r search.File, p string) int { return strings.Compare(r.Path, p) })
	if !ok {
		return nil
	}
	return &a.find.results[i]
}

// findChildren lists a result row's children: a file's matches.
func (a *app) findChildren(k findKey) []findKey {
	if k.match >= 0 {
		return nil
	}
	file := a.findFile(k.path)
	if file == nil {
		return nil
	}
	out := make([]findKey, len(file.Matches))
	for i := range file.Matches {
		out[i] = findKey{path: k.path, match: i}
	}
	return out
}

// openMatch shows a match in the focused group, selected.
func (a *app) openMatch(k findKey) {
	file := a.findFile(k.path)
	if file == nil || k.match < 0 || k.match >= len(file.Matches) {
		return
	}
	m := file.Matches[k.match]
	a.cancelNav()
	pos := editor.Pos{Line: m.Line, Col: m.Col}
	a.openIn(a.focus, k.path, goTo{pos: &pos, selLen: m.End - m.Col})
}

// ----- Open Quickly: files by name, ranked by sahilm/fuzzy -----

type quickOpen struct {
	open          bool
	query         string
	ranked        string // the query results are of
	files         []string
	listing       bool
	truncated     bool
	err           error
	results       []search.Scored
	list          ui.ListState
	sel           int
	gen           int
	symbols       []quickSymbol
	symbolCancel  context.CancelFunc
	symbolLoading bool
	symbolErr     error
}

type quickSymbol struct {
	target target
	enc    string
	name   string
}

// maxQuickFiles bounds the files Open Quickly lists.
const maxQuickFiles = 200000

func (a *app) openQuickly() {
	a.openQuickPanel("")
}

func (a *app) openQuickPanel(query string) {
	q := &a.quick
	q.gen++
	if q.symbolCancel != nil {
		q.symbolCancel()
		q.symbolCancel = nil
	}
	q.open, q.query, q.ranked, q.sel = true, query, "\x00", 0
	q.list.Selected = &q.sel
	a.ensureFiles()
}

// ensureFiles lists the workspace's files for Open Quickly and the tree's
// filter, once: the list lasts until the workspace changes or a refresh
// forgets it.
func (a *app) ensureFiles() {
	q := &a.quick
	if q.files != nil || q.listing || a.root == "" {
		return
	}
	q.listing = true
	gen, root := a.gen, a.root
	rg, _ := findRipgrep(root) // "" lists by walking
	ctx := a.wsCtx
	go func() {
		files, truncated, err := search.ListFiles(ctx, rg, root, maxQuickFiles)
		a.post(func() {
			if a.gen != gen {
				return
			}
			q.listing, q.files, q.truncated, q.err = false, files, truncated, err
			if q.files == nil {
				q.files = []string{}
			}
			if !strings.HasPrefix(q.query, ">") && !strings.HasPrefix(q.query, "#") {
				q.ranked = "\x00" // rank files again
			}
			a.filter.forQuery = "" // filter again over the new list
		})
	}()
}

// rankQuick ranks the files for the query when it changed.
func (a *app) rankQuick() {
	q := &a.quick
	if q.ranked == q.query {
		return
	}
	q.ranked = q.query
	q.gen++
	if q.symbolCancel != nil {
		q.symbolCancel()
		q.symbolCancel = nil
	}
	q.symbolLoading, q.symbolErr = false, nil
	q.symbols = nil
	if strings.HasPrefix(q.query, ">") {
		all := commands()
		names := make([]string, len(all))
		for i, cmd := range all {
			names[i] = cmd.name
		}
		q.results = search.Fuzzy(names, strings.TrimSpace(q.query[1:]), len(names))
	} else if strings.HasPrefix(q.query, "#") {
		q.results = nil
		if a.root != "" && a.cfg.trusted(a.root) && strings.TrimSpace(q.query[1:]) != "" {
			a.searchSymbols(strings.TrimSpace(q.query[1:]))
		}
	} else {
		q.results = search.Fuzzy(q.files, q.query, 60)
	}
	q.sel = 0
}

func (a *app) searchSymbols(query string) {
	q := &a.quick
	ctx, cancel := context.WithTimeout(a.wsCtx, 20*time.Second)
	q.symbolCancel, q.symbolLoading = cancel, true
	gen, ranked, request := a.gen, q.ranked, q.gen
	servers := []*langserver.Server{a.lsp, a.tsLSP}
	go func() {
		defer cancel()
		select {
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			return
		}
		type answer struct {
			symbols []quickSymbol
			err     error
		}
		answers := make(chan answer, len(servers))
		count := 0
		for _, m := range servers {
			if m == nil {
				continue
			}
			count++
			go func(m *langserver.Server) {
				c, err := m.Get(ctx)
				var out []quickSymbol
				if err == nil {
					if client, ok := c.(interface {
						WorkspaceSymbols(context.Context, string) ([]lsp.WorkspaceSymbol, error)
					}); ok {
						var symbols []lsp.WorkspaceSymbol
						symbols, err = client.WorkspaceSymbols(ctx, query)
						for _, s := range symbols {
							if path, e := uriutil.ToPath(s.Location.URI); e == nil {
								out = append(out, quickSymbol{target: target{path: path, pos: s.Location.Range.Start}, enc: c.Encoding(), name: s.Name})
							}
						}
					}
				}
				answers <- answer{out, err}
			}(m)
		}
		var symbols []quickSymbol
		var firstErr error
		for i := 0; i < count; i++ {
			var result answer
			select {
			case result = <-answers:
			case <-ctx.Done():
				firstErr = ctx.Err()
				i = count
				continue
			}
			symbols = append(symbols, result.symbols...)
			if firstErr == nil {
				firstErr = result.err
			}
			if len(result.symbols) > 0 {
				partial := append([]quickSymbol(nil), symbols...)
				a.post(func() {
					if a.gen != gen || !q.open || q.query != ranked || q.ranked != ranked || q.gen != request {
						return
					}
					q.symbols = partial
					q.results = make([]search.Scored, len(partial))
				})
			}
		}
		a.post(func() {
			if a.gen != gen || !q.open || q.query != ranked || q.ranked != ranked || q.gen != request {
				return
			}
			q.symbolLoading, q.symbolCancel = false, nil
			q.symbols = symbols
			if len(symbols) == 0 {
				q.symbolErr = firstErr
			}
			q.results = make([]search.Scored, len(symbols))
			q.sel = 0
		})
	}()
}

func (a *app) chooseQuick(i int) {
	q := &a.quick
	if i < 0 || i >= len(q.results) {
		return
	}
	p := q.results[i].Path
	q.open = false
	if strings.HasPrefix(q.query, "#") {
		s := q.symbols[i]
		if a.focus != nil {
			if st := a.activeEditor(); st != nil {
				pos := st.CursorPos()
				a.hist.Push(nav.Entry{Path: a.activePath(), Line: pos.Line, ByteColumn: pos.Col})
			}
			a.cancelNav()
			pos := s.target.pos
			a.openIn(a.focus, s.target.path, goTo{lsp: &pos, enc: s.enc})
		}
		return
	}
	if strings.HasPrefix(q.query, ">") {
		for _, cmd := range commands() {
			if cmd.name == p {
				cmd.run(a)
				return
			}
		}
		return
	}
	a.previewOpen(joinRoot(a.root, p))
}

func joinRoot(root, rel string) string { return filepath.Join(root, filepath.FromSlash(rel)) }
