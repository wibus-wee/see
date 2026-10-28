// Package search finds text in a workspace with ripgrep, lists its files
// for quick open, and ranks them with github.com/sahilm/fuzzy.
package search

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/sahilm/fuzzy"

	"nsr/internal/workspace"
)

// ErrNoRipgrep is returned when ripgrep is not installed.
var ErrNoRipgrep = errors.New("ripgrep (rg) is not installed")

// FindRipgrep finds rg: $NSR_RG, then PATH. One inside the workspace is
// never used.
func FindRipgrep(root string) (string, error) {
	var candidates []string
	if p := os.Getenv("NSR_RG"); p != "" {
		candidates = append(candidates, p)
	}
	if p, err := exec.LookPath("rg"); err == nil {
		candidates = append(candidates, p)
	}
	for _, p := range candidates {
		if !filepath.IsAbs(p) {
			continue
		}
		if rel, err := filepath.Rel(root, p); root != "" && err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return p, nil
		}
	}
	return "", ErrNoRipgrep
}

// InstallHint tells how to install ripgrep on this platform.
func InstallHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "brew install ripgrep"
	case "windows":
		return "winget install BurntSushi.ripgrep.MSVC"
	}
	return "install your distribution's ripgrep package"
}

// Options say what to find and where.
type Options struct {
	Query         string
	CaseSensitive bool
	WholeWord     bool
	Regexp        bool
	// Include and Exclude are comma-separated globs, as ripgrep's -g.
	Include, Exclude string
	// MaxMatches stops the search once this many are found; 0 is 20000.
	MaxMatches int
}

// Match is one place the query was found.
type Match struct {
	Line int // from 0
	// Col and End are bytes into the line.
	Col, End int
	// Preview is the line, or part of a long one; PreStart is where the
	// match starts in it and PreEnd where it ends.
	Preview          string
	PreStart, PreEnd int
}

// File is the matches in one file.
type File struct {
	Path    string
	Matches []Match
}

// Stats sum a search up.
type Stats struct {
	Files, Matches int
	Truncated      bool // MaxMatches was reached
}

// ignoreGlobs keep ripgrep out of the folders the tree dims and never
// scans, besides what .gitignore leaves out.
func ignoreGlobs() []string {
	var out []string
	for _, d := range workspace.IgnoredNames() {
		out = append(out, "-g", "!"+d+"/")
	}
	return out
}

// Args are ripgrep's arguments for o.
func Args(o Options) []string {
	args := []string{"--json", "--max-filesize", "4M", "--max-columns", "4000", "--no-config"}
	if !o.Regexp {
		args = append(args, "--fixed-strings")
	}
	if o.CaseSensitive {
		args = append(args, "--case-sensitive")
	} else {
		args = append(args, "--ignore-case")
	}
	if o.WholeWord {
		args = append(args, "--word-regexp")
	}
	args = append(args, ignoreGlobs()...)
	for g := range strings.SplitSeq(o.Include, ",") {
		if g = strings.TrimSpace(g); g != "" {
			args = append(args, "-g", g)
		}
	}
	for g := range strings.SplitSeq(o.Exclude, ",") {
		if g = strings.TrimSpace(g); g != "" {
			args = append(args, "-g", "!"+g)
		}
	}
	return append(args, "-e", o.Query, "--", ".")
}

// rgEvent is the part of ripgrep's JSON lines the search reads.
type rgEvent struct {
	Type string `json:"type"`
	Data struct {
		Path       rgText `json:"path"`
		Lines      rgText `json:"lines"`
		LineNumber int    `json:"line_number"`
		Submatches []struct {
			Start int `json:"start"`
			End   int `json:"end"`
		} `json:"submatches"`
	} `json:"data"`
}

type rgText struct {
	Text string `json:"text"`
}

// Run searches root with the ripgrep at rg, calling emit with each
// file's matches as ripgrep finishes it, until ctx is canceled or
// MaxMatches is reached.
func Run(ctx context.Context, rg, root string, o Options, emit func(File)) (Stats, error) {
	if o.Query == "" {
		return Stats{}, errors.New("empty query")
	}
	limit := o.MaxMatches
	if limit <= 0 {
		limit = 20000
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, rg, Args(o)...)
	cmd.Dir = root
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return Stats{}, err
	}
	if err := cmd.Start(); err != nil {
		return Stats{}, fmt.Errorf("starting ripgrep: %w", err)
	}
	var st Stats
	var cur *File
	flush := func() {
		if cur != nil && len(cur.Matches) > 0 {
			st.Files++
			emit(*cur)
		}
		cur = nil
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for !st.Truncated && sc.Scan() {
		var ev rgEvent
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "begin":
			flush()
			cur = &File{Path: filepath.Join(root, filepath.FromSlash(ev.Data.Path.Text))}
		case "end":
			flush()
		case "match":
			// A line that is not UTF-8 comes as bytes, and is not shown.
			if cur == nil || ev.Data.Lines.Text == "" {
				continue
			}
			line := strings.TrimRight(ev.Data.Lines.Text, "\r\n")
			for _, sm := range ev.Data.Submatches {
				if sm.End > len(line) || sm.Start >= sm.End {
					continue
				}
				cur.Matches = append(cur.Matches, preview(ev.Data.LineNumber-1, line, sm.Start, sm.End))
				st.Matches++
				if st.Matches >= limit {
					st.Truncated = true
					break
				}
			}
		}
	}
	flush()
	if st.Truncated {
		cancel()
	}
	werr := cmd.Wait()
	switch {
	case st.Truncated:
		return st, nil
	case ctx.Err() != nil:
		return st, ctx.Err()
	case werr != nil:
		var ee *exec.ExitError
		if errors.As(werr, &ee) && ee.ExitCode() == 1 {
			return st, nil // ripgrep's "nothing found"
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			msg, _, _ = strings.Cut(msg, "\n")
			return st, errors.New(msg)
		}
		return st, werr
	}
	return st, nil
}

// preview cuts a long line around a match, at character boundaries.
func preview(line int, text string, start, end int) Match {
	m := Match{Line: line, Col: start, End: end}
	from, to := 0, len(text)
	const before, after = 40, 160
	if start > before {
		from = start - before
		for from < start && !utf8.RuneStart(text[from]) {
			from++
		}
	}
	if to-end > after {
		to = end + after
		for to > end && to < len(text) && !utf8.RuneStart(text[to]) {
			to--
		}
	}
	pre := text[from:to]
	lead := min(len(pre)-len(strings.TrimLeft(pre, " \t")), start-from)
	m.Preview = pre[lead:]
	m.PreStart = start - from - lead
	m.PreEnd = min(end-from-lead, len(m.Preview))
	if from > 0 {
		m.Preview = "…" + m.Preview
		m.PreStart += len("…")
		m.PreEnd += len("…")
	}
	return m
}

// ListFiles lists the files under root for quick open, relative to it
// and separated by slashes: with `rg --files` when rg is given, which
// leaves out what .gitignore does, else by walking the tree. It stops
// at limit files.
func ListFiles(ctx context.Context, rg, root string, limit int) ([]string, bool, error) {
	if rg == "" {
		return walkFiles(ctx, root, limit)
	}
	args := append([]string{"--files", "--no-config"}, ignoreGlobs()...)
	cmd := exec.CommandContext(ctx, rg, args...)
	cmd.Dir = root
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, err
	}
	if err := cmd.Start(); err != nil {
		return nil, false, err
	}
	var files []string
	truncated := false
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		if len(files) >= limit {
			truncated = true
			break
		}
		files = append(files, filepath.ToSlash(sc.Text()))
	}
	if truncated {
		_ = cmd.Process.Kill()
	}
	_ = cmd.Wait()
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	return files, truncated, nil
}

// walkFiles lists files without ripgrep.
func walkFiles(ctx context.Context, root string, limit int) ([]string, bool, error) {
	var files []string
	truncated := false
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if d != nil && d.IsDir() && p != root {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if p != root && workspace.Ignored(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if len(files) >= limit {
			truncated = true
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(root, p)
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	return files, truncated, err
}

// Scored is a path quick open matched, best first.
type Scored struct {
	Path string
	// Hits are the bytes of Path the query's characters matched.
	Hits []int
}

// Fuzzy ranks paths against query with sahilm/fuzzy, the file's name
// counting before its folders, and returns at most limit of them. An
// empty query lists the first paths.
func Fuzzy(paths []string, query string, limit int) []Scored {
	query = strings.ReplaceAll(query, " ", "")
	if query == "" {
		out := make([]Scored, 0, min(limit, len(paths)))
		for _, p := range paths[:min(limit, len(paths))] {
			out = append(out, Scored{Path: p})
		}
		return out
	}
	ms := fuzzy.FindFrom(query, nameFirst(paths))
	out := make([]Scored, 0, min(limit, len(ms)))
	for _, m := range ms {
		if len(out) == limit {
			break
		}
		out = append(out, Scored{Path: paths[m.Index], Hits: unflip(paths[m.Index], m.MatchedIndexes)})
	}
	return out
}

// nameFirst presents paths as "name dir/", so that the library's
// preference for early and word-start matches favors the file's name.
type nameFirst []string

func (n nameFirst) Len() int { return len(n) }

func (n nameFirst) String(i int) string {
	dir, name := splitPath(n[i])
	if dir == "" {
		return name
	}
	return name + " " + dir
}

func splitPath(p string) (dir, name string) {
	i := strings.LastIndexByte(p, '/')
	return p[:i+1], p[i+1:]
}

// unflip maps the matched indexes of "name dir/" back into the path.
func unflip(p string, idx []int) []int {
	dir, name := splitPath(p)
	out := make([]int, 0, len(idx))
	for _, i := range idx {
		switch {
		case i < len(name):
			out = append(out, len(dir)+i)
		case i > len(name):
			out = append(out, i-len(name)-1)
		}
	}
	return out
}
