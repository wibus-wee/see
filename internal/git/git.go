// Package git reads a repository's state with the git command: its root,
// branch and changes, and the status each changed path and folder shows.
package git

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
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
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	args = append([]string{"-c", "core.fsmonitor=false", "-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(cmd.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "GIT_LITERAL_PATHSPECS=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// Read returns the root of the repository holding dir, its branch (or
// HEAD's short hash when detached) and its changes, untracked files among
// them.
func Read(ctx context.Context, dir string) (root, branch string, changes []Change, err error) {
	out, err := Command(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", "", nil, err
	}
	root = strings.TrimSpace(string(out))
	b, err := Command(ctx, root, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		b, err = Command(ctx, root, "rev-parse", "--short", "HEAD")
	}
	if err != nil {
		return "", "", nil, err
	}
	out, err = Command(ctx, root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return "", "", nil, err
	}
	return root, strings.TrimSpace(string(b)), ParseStatus(out), nil
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
	if ch.Index == 'U' || ch.Work == 'U' || (ch.Index == 'A' && ch.Work == 'A') || (ch.Index == 'D' && ch.Work == 'D') {
		return '!'
	}
	if ch.Work != ' ' {
		return ch.Work
	}
	return ch.Index
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
