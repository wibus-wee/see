package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Stash is an entry of the repository's stash list, newest first.
// Hash identifies the saved commit, not the reflog entry that points to it.
type Stash struct {
	Index             int
	Hash              string
	Branch            string // the branch it was taken on, "" detached
	Message           string // what the user typed, or the WIP subject git wrote
	Date              time.Time
	selector, subject string
	ambiguous         bool
}

// Ref is the stash's reflog name, stash@{n}.
func (s Stash) Ref() string { return fmt.Sprintf("stash@{%d}", s.Index) }

func (s Stash) EntryKey() string {
	key := s.entryIdentity()
	if s.ambiguous {
		key += "\x00" + s.Ref()
	}
	return key
}

func (s Stash) entryIdentity() string {
	return s.Hash + "\x00" + s.selector + "\x00" + s.subject
}

// Stashes reads the stash list.
func Stashes(ctx context.Context, root string) ([]Stash, error) {
	out, err := Command(ctx, root, "stash", "list", "-z", "--date=raw", "--format=%H%x1f%ct%x1f%gd%x1f%gs")
	if err != nil {
		return nil, err
	}
	return parseStashList(out), nil
}

func parseStashList(out []byte) []Stash {
	var stashes []Stash
	counts := map[string]int{}
	for _, rec := range strings.Split(string(out), "\x00") {
		rec = strings.TrimLeft(rec, "\n")
		f := strings.SplitN(rec, "\x1f", 4)
		if len(f) < 4 || f[0] == "" {
			continue
		}
		s := Stash{Index: len(stashes), Hash: f[0], selector: f[2], subject: f[3]}
		if sec, err := strconv.ParseInt(f[1], 10, 64); err == nil {
			s.Date = time.Unix(sec, 0)
		}
		s.Branch, s.Message = parseStashSubject(s.subject)
		counts[s.entryIdentity()]++
		stashes = append(stashes, s)
	}
	for i := range stashes {
		stashes[i].ambiguous = counts[stashes[i].entryIdentity()] > 1
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
	if _, err := stashRef(ctx, root, s, drop); err != nil {
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
	ref, err := stashRef(ctx, root, s, true)
	if err != nil {
		return err
	}
	// Git has no compare-and-drop: an external writer can still move the index here.
	_, err = Command(ctx, root, "stash", "drop", ref)
	return err
}

// stashRef translates a reflog entry's identity back to its current index.
func stashRef(ctx context.Context, root string, s Stash, dropping bool) (string, error) {
	stashes, err := Stashes(ctx, root)
	if err != nil {
		return "", err
	}
	for _, t := range stashes {
		if t.entryIdentity() != s.entryIdentity() {
			continue
		}
		if dropping && (s.ambiguous || t.ambiguous) {
			return "", fmt.Errorf("the stash entry is indistinguishable from another entry")
		}
		return t.Ref(), nil
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
