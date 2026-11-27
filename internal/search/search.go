// Package search finds text in a workspace with ripgrep, lists its files
// for quick open, and ranks them with github.com/sahilm/fuzzy.
package search

import (
	"bufio"
	"context"
	"crypto/sha256"
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

	"see/internal/proc"
	"see/internal/workspace"
)

// ErrNoRipgrep is returned when ripgrep is not installed.
var ErrNoRipgrep = errors.New("ripgrep (rg) is not installed")

// FindRipgrep finds rg: $SEE_RG, then the candidates an installation added
// (in the order given), then PATH. One inside the workspace is never used —
// inside means what the candidate resolves to in the disk's own spelling,
// the identity check the launch boundary applies, not the lexical shape of
// the path: a case variant of a workspace path on a case-insensitive volume,
// or a symlink anywhere pointing into the workspace, names the workspace's
// bytes. A copy discovered on PATH that resolves inside the workspace is
// skipped silently — the user never pointed at it; one the user or an
// installation pointed at is the typed denial proc.ErrUntrustedExecutable,
// as the language-server finders report theirs.
func FindRipgrep(root string, installed ...string) (string, error) {
	type candidate struct {
		path     string
		explicit bool
	}
	var candidates []candidate
	if p := os.Getenv("SEE_RG"); p != "" {
		candidates = append(candidates, candidate{p, true})
	}
	for _, p := range installed {
		candidates = append(candidates, candidate{p, true})
	}
	if p, err := exec.LookPath("rg"); err == nil {
		candidates = append(candidates, candidate{p, false})
	}
	for _, c := range candidates {
		if !filepath.IsAbs(c.path) {
			continue
		}
		if err := proc.ValidateExecutable(root, c.path); err != nil {
			if errors.Is(err, proc.ErrUntrustedExecutable) && c.explicit {
				return "", err
			}
			continue
		}
		if fi, err := os.Stat(c.path); err == nil && fi.Mode().IsRegular() {
			return c.path, nil
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
type IgnoreFileOptions struct {
	All, Global, Parent bool
}

type Options struct {
	Query         string
	CaseSensitive bool
	WholeWord     bool
	Regexp        bool
	// Include and Exclude are comma-separated globs, as ripgrep's -g.
	Include, Exclude string
	// MaxMatches stops the search once this many are found; 0 is 20000.
	MaxMatches int
	// NoIgnoreFiles disables ignore files as rg's --no-ignore* flags.
	NoIgnoreFiles IgnoreFileOptions
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
	lineHash         [sha256.Size]byte
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
	if o.NoIgnoreFiles.All {
		args = append(args, "--no-ignore")
	} else {
		if o.NoIgnoreFiles.Global {
			args = append(args, "--no-ignore-global")
		}
		if o.NoIgnoreFiles.Parent {
			args = append(args, "--no-ignore-parent")
		}
	}
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
func Run(ctx context.Context, rg, root string, o Options, extraExcludes []string, emit func(File)) (Stats, error) {
	if o.Query == "" {
		return Stats{}, errors.New("empty query")
	}
	limit := o.MaxMatches
	if limit <= 0 {
		limit = 20000
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	args := Args(o)
	if len(extraExcludes) > 0 {
		tail := append([]string(nil), args[len(args)-4:]...)
		args = args[:len(args)-4]
		for _, glob := range extraExcludes {
			args = append(args, "-g", "!"+glob)
		}
		args = append(args, tail...)
	}
	cmd := proc.CommandContext(ctx, rg, args...)
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
			if ev.Data.Path.Text != "" {
				cur = &File{Path: filepath.Join(root, filepath.FromSlash(ev.Data.Path.Text))}
			}
		case "end":
			flush()
		case "match":
			// A line that is not UTF-8 comes as bytes, and is not shown.
			if cur == nil || ev.Data.Lines.Text == "" {
				continue
			}
			line := trimLineEnding(ev.Data.Lines.Text)
			lineHash := sha256.Sum256([]byte(line))
			for _, sm := range ev.Data.Submatches {
				if sm.Start < 0 || sm.End > len(line) || sm.Start >= sm.End {
					continue
				}
				m := preview(ev.Data.LineNumber-1, line, sm.Start, sm.End)
				m.lineHash = lineHash
				cur.Matches = append(cur.Matches, m)
				st.Matches++
				if st.Matches >= limit {
					st.Truncated = true
					break
				}
			}
		}
	}
	flush()
	scanErr := sc.Err()
	if st.Truncated || scanErr != nil {
		cancel()
	}
	werr := cmd.Wait()
	switch {
	case st.Truncated:
		return st, nil
	case scanErr != nil:
		return st, scanErr
	case ctx.Err() != nil:
		return st, ctx.Err()
	case werr != nil:
		var ee *exec.ExitError
		if errors.As(werr, &ee) && ee.ExitCode() == 1 {
			return st, nil // ripgrep's "nothing found"
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return st, errors.New(msg)
		}
		return st, werr
	}
	return st, nil
}

func trimLineEnding(s string) string {
	if line, ok := strings.CutSuffix(s, "\n"); ok {
		return strings.TrimSuffix(line, "\r")
	}
	return s
}

// previewWidth is how much of a long line a preview shows.
const previewWidth = 200

// preview cuts a long line around a match, at character boundaries. The
// coordinates are ripgrep's, and are clamped rather than trusted: they
// arrive through a parser of another process's output, and a slice out
// of range here would crash the editor from a background goroutine.
func preview(line int, text string, start, end int) Match {
	start = min(max(start, 0), len(text))
	end = min(max(end, 0), len(text))
	if start > end {
		start = end
	}
	m := Match{Line: line, Col: start, End: end}
	from, to := 0, len(text)
	if to-from > previewWidth {
		// The window centers the match, shifted back inside the line
		// and then far enough that the whole match stays inside it:
		// everything below slices by that invariant. Rune alignment
		// walks at most three bytes, the width of the longest
		// continuation run inside a valid character.
		from = max(0, min(start-previewWidth/2, len(text)-previewWidth))
		to = from + previewWidth
		if end > to {
			to = end
		}
		// The walk forward never crosses the match itself: slicing from
		// past it would leave nothing to highlight, and subtraction
		// below assumes from <= start.
		for i := 0; i < 3 && from < start && !utf8.RuneStart(text[from]); i++ {
			from++
		}
		for i := 0; i < 3 && to < len(text) && !utf8.RuneStart(text[to]); i++ {
			to++
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
// at limit matching files. The walk also leaves out the folders skipDir names,
// by their slash-separated path relative to root, when it is given.
//
// Its contract with callers:
//
//   - Ordering is unspecified and must not be depended on; ranking is
//     the caller's job. Paths are byte-wise distinct within one call:
//     neither `rg --files` nor the walk can visit a file twice, so a
//     duplicate in the result would itself be a bug to fix, not input
//     to deduplicate away.
//   - truncated means the scan stopped at limit and there may be more
//     files than reported; the files returned are the first limit the
//     scan happened to reach. Without truncation the list is complete
//     for its scan moment, modulo concurrent filesystem change, which
//     no scan can observe atomically.
//   - An error means the list is not trustworthy at all: nothing is
//     returned, not even a partial prefix, so a failed scan can never
//     pass for an empty workspace. Cancellation is reported as the
//     context's error, not as truncation.
//   - Memory stays bounded by limit paths plus one scanner line,
//     regardless of how large the workspace is.
func ListFiles(ctx context.Context, rg, root string, limit int, skipDir func(rel string) bool, skipFile func(rel string) bool, noIgnoreFiles IgnoreFileOptions) ([]string, bool, error) {
	if rg == "" {
		return walkFiles(ctx, root, limit, skipDir, skipFile)
	}
	args := append([]string{"--files", "--no-config"}, ignoreGlobs()...)
	if noIgnoreFiles.All {
		args = append(args, "--no-ignore")
	} else {
		if noIgnoreFiles.Global {
			args = append(args, "--no-ignore-global")
		}
		if noIgnoreFiles.Parent {
			args = append(args, "--no-ignore-parent")
		}
	}
	cmd := proc.CommandContext(ctx, rg, args...)
	cmd.Dir = root
	var stderr strings.Builder
	cmd.Stderr = &stderr
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
		rel := filepath.ToSlash(sc.Text())
		if skipFile != nil && skipFile(rel) {
			continue
		}
		if len(files) >= limit {
			truncated = true
			break
		}
		// What rg prints is a tool's claim about a relative path; keep
		// only well-formed confined ones, as a file named oddly enough
		// must not let a join reach outside the root.
		if workspace.ValidRel(rel) {
			files = append(files, rel)
		}
	}
	if truncated || sc.Err() != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if err := sc.Err(); err != nil {
		return nil, false, err
	}
	if waitErr != nil && !truncated {
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) || exitErr.ExitCode() != 1 {
			return nil, false, fmt.Errorf("listing workspace files: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
		}
	}
	return files, truncated, nil
}

// walkFiles lists files without ripgrep.
func walkFiles(ctx context.Context, root string, limit int, skipDir func(rel string) bool, skipFile func(rel string) bool) ([]string, bool, error) {
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
			if rel, err := filepath.Rel(root, p); p != root && err == nil && skipDir != nil && skipDir(filepath.ToSlash(rel)) {
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
		if skipFile != nil && skipFile(filepath.ToSlash(rel)) {
			return nil
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		// Like the ripgrep scan, a failed walk returns nothing: partial
		// results of an interrupted walk are not a list anyone can use.
		return nil, false, err
	}
	return files, truncated, nil
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
	return appendUnflipped(make([]int, 0, len(idx)), p, idx)
}

func appendUnflipped(out []int, p string, idx []int) []int {
	dir, name := splitPath(p)
	flipped := len(name) + 1 + len(dir) // "name dir" is one byte longer than the path
	for _, i := range idx {
		if i < 0 || i >= flipped {
			// The matcher never produces these, but a hit outside the
			// flipped string would unflip to a byte outside the path,
			// where the UI highlights by index without checking again.
			continue
		}
		switch {
		case i < len(name):
			out = append(out, len(dir)+i)
		case i > len(name):
			out = append(out, i-len(name)-1)
		}
	}
	return out
}
