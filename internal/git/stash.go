package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Stash is an entry of the repository's stash list, newest first.
// Hash identifies it across stashes added or dropped since it was read,
// which shift its Index.
type Stash struct {
	Index   int
	Hash    string
	Branch  string // the branch it was taken on, "" detached
	Message string // what the user typed, or the WIP subject git wrote
	Date    time.Time
}

// Ref is the stash's reflog name, stash@{n}.
func (s Stash) Ref() string { return fmt.Sprintf("stash@{%d}", s.Index) }

// Stashes reads the stash list.
func Stashes(ctx context.Context, root string) ([]Stash, error) {
	out, err := Command(ctx, root, "stash", "list", "-z", "--format=%H%x1f%ct%x1f%gs")
	if err != nil {
		return nil, err
	}
	return parseStashList(out), nil
}

func parseStashList(out []byte) []Stash {
	var stashes []Stash
	for _, rec := range strings.Split(string(out), "\x00") {
		rec = strings.TrimLeft(rec, "\n")
		f := strings.SplitN(rec, "\x1f", 3)
		if len(f) < 3 || f[0] == "" {
			continue
		}
		s := Stash{Index: len(stashes), Hash: f[0]}
		if sec, err := strconv.ParseInt(f[1], 10, 64); err == nil {
			s.Date = time.Unix(sec, 0)
		}
		s.Branch, s.Message = parseStashSubject(f[2])
		stashes = append(stashes, s)
	}
	return stashes
}

// parseStashSubject reads the reflog subject git stash writes: "On
// <branch>: <message>" for a stash given a message, "WIP on <branch>:
// <hash> <subject>" for one without, "(no branch)" when detached.
func parseStashSubject(subject string) (branch, message string) {
	rest, wip := strings.CutPrefix(subject, "WIP on ")
	if !wip {
		var ok bool
		if rest, ok = strings.CutPrefix(subject, "On "); !ok {
			return "", subject
		}
	}
	branch, message, ok := strings.Cut(rest, ": ")
	if !ok {
		return "", subject
	}
	if branch == "(no branch)" {
		branch = ""
	}
	if wip {
		if _, subject, ok := strings.Cut(message, " "); ok {
			message = subject
		}
		message = "WIP: " + message
	}
	return branch, message
}

// StashPush stashes the working tree's and index's changes, untracked
// files among them, under message when it is not empty.
func StashPush(ctx context.Context, root, message string) error {
	args := []string{"stash", "push", "--include-untracked"}
	if message = strings.TrimSpace(message); message != "" {
		args = append(args, "-m", message)
	}
	// With literal pathspecs git stash leaves the untracked files it saved
	// in place, as its internal clean matches none of them.
	_, err := commandEnv(ctx, root, nil, []string{"GIT_LITERAL_PATHSPECS=0"}, args...)
	return err
}

// StashApply applies the stash to the working tree, restoring what was
// staged as staged; drop removes it from the list once applied cleanly.
//
// It applies the stash's commit, which a stash the list gains or loses in
// the meantime cannot change, after checking it is still listed.
func StashApply(ctx context.Context, root string, s Stash, drop bool) error {
	if _, err := stashRef(ctx, root, s); err != nil {
		return err
	}
	ref := s.Hash
	if _, err := Command(ctx, root, "stash", "apply", "--index", ref); err != nil {
		if !strings.Contains(err.Error(), "--index") {
			return err
		}
		// The index cannot take the staged changes back as they were:
		// apply them to the working tree alone, as git suggests.
		if _, err = Command(ctx, root, "stash", "apply", ref); err != nil {
			return err
		}
	}
	if drop {
		return StashDrop(ctx, root, s)
	}
	return nil
}

// StashDrop removes the stash from the list.
func StashDrop(ctx context.Context, root string, s Stash) error {
	ref, err := stashRef(ctx, root, s)
	if err != nil {
		return err
	}
	_, err = Command(ctx, root, "stash", "drop", ref)
	return err
}

// stashRef is the stash@{n} naming s now, so that an action on a stash
// read before another was added or dropped acts on it and no other. Of
// entries that store the same commit, the one still at s's index wins.
func stashRef(ctx context.Context, root string, s Stash) (string, error) {
	stashes, err := Stashes(ctx, root)
	if err != nil {
		return "", err
	}
	if s.Index < len(stashes) && stashes[s.Index].Hash == s.Hash {
		return s.Ref(), nil
	}
	for _, t := range stashes {
		if t.Hash == s.Hash {
			return t.Ref(), nil
		}
	}
	return "", fmt.Errorf("the stash is no longer in the stash list")
}

// stashUntracked is the commit holding the untracked files a stash saved,
// its third parent, or "" when hash is no such stash.
func stashUntracked(ctx context.Context, root, hash string) string {
	out, err := Command(ctx, root, "rev-parse", "--verify", "--quiet", hash+"^3")
	if err != nil {
		return ""
	}
	u := strings.TrimSpace(string(out))
	out, err = Command(ctx, root, "log", "-1", "--format=%P%x00%s", u, "--")
	if err != nil {
		return ""
	}
	parents, subject, _ := strings.Cut(strings.TrimSpace(string(out)), "\x00")
	if parents != "" || !strings.HasPrefix(subject, "untracked files on ") {
		return ""
	}
	return u
}
