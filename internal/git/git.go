// Package git reads a repository's state with the git command: its root,
// branch and changes, and the status each changed path and folder shows.
package git

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"see/internal/proc"
)

// Change is a path git status reports, with its status in the index and
// in the working tree as porcelain's two letters ('?' both for untracked).
// OldPath is where a renamed or copied path came from.
type Change struct {
	Path, OldPath string
	Index, Work   byte
}

// Command runs git in dir, without locks, prompts, pagers or pathspec
// magic, giving up after 30 seconds. Its error holds git's output.
func Command(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return commandInput(ctx, dir, nil, args...)
}

func commandInput(ctx context.Context, dir string, input []byte, args ...string) ([]byte, error) {
	return commandEnv(ctx, dir, input, nil, args...)
}

// commandEnv is commandInput with env added after, and so overriding, the
// variables every command runs with.
func commandEnv(ctx context.Context, dir string, input []byte, env []string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	args = append([]string{"-c", "core.fsmonitor=false", "-C", dir}, args...)
	cmd := proc.CommandContext(ctx, "git", args...)
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	cmd.Env = append(cmd.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "GIT_LITERAL_PATHSPECS=1")
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func commandSeparateOutput(ctx context.Context, dir string, args ...string) ([]byte, []byte, error) {
	return commandOutputEnv(ctx, dir, nil, args...)
}

func commandOutputEnv(ctx context.Context, dir string, env []string, args ...string) ([]byte, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	args = append([]string{"-c", "core.fsmonitor=false", "-C", dir}, args...)
	cmd := proc.CommandContext(ctx, "git", args...)
	cmd.Env = append(cmd.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "GIT_LITERAL_PATHSPECS=1")
	cmd.Env = append(cmd.Env, env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// State is what a refresh of a repository's status shows.
type State struct {
	// Root is the repository's top level; Branch the name HEAD names, or
	// HEAD's short hash detached. Head is HEAD's commit, "" without one.
	Root, Branch, Head string
	Changes            []Change
	Merging            bool
	// Upstream is the branch HEAD's tracks, and Ahead and Behind how far
	// apart they are; "" without one.
	Upstream      string
	Ahead, Behind int
	Remotes       []string // the repository's remotes, origin first
	Web           string   // the web page of the remote Upstream names
	// Refs is a fingerprint of every ref and its target, for staleness:
	// a push, tag or branch switch can change history without moving
	// HEAD. RefsErr holds for-each-ref's error when it failed.
	Refs       string
	RefsErr    error
	Repository RepositoryRefs
	// Staged and Unstaged are each changed file's added and deleted
	// lines, read only when asked for.
	Staged, Unstaged map[string]FileStat
}

// Read returns the root of the repository holding dir, its branch (or
// HEAD's short hash when detached) and its changes, untracked files among
// them.
func Read(ctx context.Context, dir string) (root, branch string, changes []Change, err error) {
	s, err := Sync(ctx, dir, false)
	return s.Root, s.Branch, s.Changes, err
}

// Sync reads everything a status refresh shows, in as few git calls as it
// can: the root, then one status --porcelain=v2 --branch that names HEAD's
// commit and the branch it tracks as well as the changes, one for-each-ref
// and one remote listing; counts asks for the lines each change adds and
// deletes besides.
func Sync(ctx context.Context, dir string, counts bool) (State, error) {
	var s State
	out, err := Command(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return s, err
	}
	s.Root = filepath.Clean(strings.TrimSpace(string(out)))
	out, err = Command(ctx, s.Root, "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all")
	if err != nil {
		return s, err
	}
	s.Branch, s.Head, s.Changes = parseStatusV2(out)
	localBranch := s.Branch
	if s.Head != "" && s.Branch == "" {
		// Detached: the branch shows as the commit's short hash, as
		// rev-parse --short HEAD gave it; its length scales with the
		// repository's size, so it is not the hash's first seven.
		if b, err := Command(ctx, s.Root, "rev-parse", "--short", "HEAD"); err == nil {
			s.Branch = strings.TrimSpace(string(b))
		}
		if s.Branch == "" {
			s.Branch = shortHash(s.Head)
		}
	}
	s.Upstream, s.Ahead, s.Behind = parseBranchStatus(out)
	_, mergeErr := Command(ctx, s.Root, "rev-parse", "--verify", "--quiet", "MERGE_HEAD")
	s.Merging = mergeErr == nil
	s.Refs, s.RefsErr = Refs(ctx, s.Root)
	if counts && len(s.Changes) > 0 {
		s.Staged, s.Unstaged = LineCounts(ctx, s.Root)
	}
	s.Remotes, s.Web = remoteInfo(ctx, s.Root, s.Upstream)
	if s.RefsErr == nil {
		s.Repository = parseRepositoryRefs(s.Refs, localBranch, s.Remotes)
	}
	return s, nil
}

// parseStatusV2 reads the headers of git status --porcelain=v2 --branch
// -z for the branch and HEAD's commit, and the entries for the changes.
func parseStatusV2(out []byte) (branch, head string, changes []Change) {
	for _, p := range strings.Split(string(out), "\x00") {
		for _, line := range strings.Split(p, "\n") {
			switch {
			case strings.HasPrefix(line, "# branch.oid "):
				if oid := line[len("# branch.oid "):]; oid != "(initial)" {
					head = oid
				}
			case strings.HasPrefix(line, "# branch.head "):
				if b := line[len("# branch.head "):]; b != "(detached)" {
					branch = b
				}
			}
		}
	}
	return branch, head, parseEntriesV2(string(out))
}

// parseEntriesV2 reads the change entries of porcelain v2's -z output:
// 1 and 2 entries as v1's two letters, ? as untracked, u as a conflict.
func parseEntriesV2(out string) []Change {
	parts := strings.Split(out, "\x00")
	var changes []Change
	for i := 0; i < len(parts); i++ {
		p := parts[i]
		switch {
		case len(p) > 3 && p[0] == '1' && p[1] == ' ':
			// 1 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <path>: the path,
			// spaces included, follows the 8th field's end.
			ch := Change{Index: v1Status(p[2]), Work: v1Status(p[3])}
			ch.Path = fieldAfter(p, 8)
			changes = append(changes, ch)
		case len(p) > 3 && p[0] == 'u' && p[1] == ' ':
			// u <XY> <sub> <m1> <m2> <m3> <mW> <h1> <h2> <h3> <path>.
			ch := Change{Index: v1Status(p[2]), Work: v1Status(p[3])}
			ch.Path = fieldAfter(p, 10)
			changes = append(changes, ch)
		case len(p) > 3 && p[0] == '2' && p[1] == ' ':
			// 2 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <X><score> <path>,
			// then <origPath> in the field after.
			ch := Change{Index: v1Status(p[2]), Work: v1Status(p[3])}
			ch.Path = fieldAfter(p, 9)
			i++
			if i < len(parts) {
				ch.OldPath = parts[i]
			}
			changes = append(changes, ch)
		case len(p) > 2 && p[0] == '?':
			changes = append(changes, Change{Path: p[2:], Index: '?', Work: '?'})
		}
	}
	return changes
}

// parseBranchStatus reads the tracking headers of git status
// --porcelain=v2 --branch: the branch HEAD's tracks, and how many commits
// apart they are.
func parseBranchStatus(out []byte) (upstream string, ahead, behind int) {
	for _, p := range strings.Split(string(out), "\x00") {
		for _, line := range strings.Split(p, "\n") {
			switch {
			case strings.HasPrefix(line, "# branch.upstream "):
				upstream = line[len("# branch.upstream "):]
			case strings.HasPrefix(line, "# branch.ab "):
				fmt.Sscanf(line, "# branch.ab +%d -%d", &ahead, &behind)
			}
		}
	}
	return upstream, ahead, behind
}

// fieldAfter returns the rest of a porcelain record past its nth
// space-separated field, spaces and all: the path fields it ends with.
func fieldAfter(p string, n int) string {
	for i, j := 0, 0; i < len(p); i++ {
		if p[i] == ' ' {
			j++
			if j == n {
				return p[i+1:]
			}
		}
	}
	return ""
}

// v1Status turns porcelain v2's '.' (unchanged) into v1's ' ', which is
// what a Change's Index and Work hold for an unchanged side.
func v1Status(c byte) byte {
	if c == '.' {
		return ' '
	}
	return c
}

func shortHash(head string) string {
	if len(head) > 7 {
		return head[:7]
	}
	return head
}

// Head returns the commit HEAD names, or "" in a repository without
// commits yet.
func Head(ctx context.Context, root string) string {
	out, err := Command(ctx, root, "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ParseStatus parses the output of git status --porcelain=v1 -z.
func ParseStatus(out []byte) []Change {
	parts := strings.Split(string(out), "\x00")
	var changes []Change
	for i := 0; i < len(parts); i++ {
		p := parts[i]
		if len(p) < 4 {
			continue
		}
		ch := Change{Path: p[3:], Index: p[0], Work: p[1]}
		if p[0] == 'R' || p[0] == 'C' || p[1] == 'R' || p[1] == 'C' {
			i++
			if i < len(parts) {
				ch.OldPath = parts[i]
			}
		}
		changes = append(changes, ch)
	}
	return changes
}

// Status is the one letter a file tree shows for the change: U for
// untracked, ! for a conflict, else the working tree's letter, or the
// index's when the working tree has none.
func (ch Change) Status() byte {
	if ch.Index == '?' {
		return 'U'
	}
	if ch.Conflicted() {
		return '!'
	}
	if ch.Work != ' ' {
		return ch.Work
	}
	return ch.Index
}

func (ch Change) Conflicted() bool {
	return ch.Index == 'U' || ch.Work == 'U' || ch.Index == 'A' && ch.Work == 'A' || ch.Index == 'D' && ch.Work == 'D'
}

// Decorations maps each changed path under root, absolute, to its status,
// and each folder holding changes to the most pressing status inside it.
func Decorations(root string, changes []Change) map[string]byte {
	out := make(map[string]byte)
	if root == "" {
		return out
	}
	for _, ch := range changes {
		status := ch.Status()
		p := filepath.Join(root, filepath.FromSlash(ch.Path))
		out[p] = status
		for dir := filepath.Dir(p); dir != root && dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
			if rel, err := filepath.Rel(root, dir); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				break
			}
			if out[dir] == 0 || priority(status) > priority(out[dir]) {
				out[dir] = status
			}
		}
	}
	return out
}

// priority orders statuses for a folder: a conflict over a deletion over
// a modification over anything else.
func priority(s byte) int {
	switch s {
	case '!':
		return 4
	case 'D':
		return 3
	case 'M', 'R', 'C':
		return 2
	default:
		return 1
	}
}
