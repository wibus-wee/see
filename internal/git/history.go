package git

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Branch is a local or remote-tracking branch.
type Branch struct {
	Name    string // short: main, origin/main
	Remote  bool
	Current bool
	// Upstream is the branch a local one tracks, and Track how far apart
	// they are, as git writes it: "ahead 2, behind 1", "gone" or "".
	Upstream, Track string
	Date            time.Time // of its last commit
	Subject         string
}

// Branches lists the repository's branches, as Xcode's branch picker:
// the current one, the other local ones, then remote ones, each kind
// newest first. Remote HEADs are left out.
func Branches(ctx context.Context, root string) ([]Branch, error) {
	out, err := Command(ctx, root, "for-each-ref", "--sort=-committerdate",
		"--format=%(refname)%00%(HEAD)%00%(upstream:short)%00%(upstream:track,nobracket)%00%(committerdate:unix)%00%(subject)",
		"refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	var local, remote []Branch
	for line := range strings.SplitSeq(string(out), "\n") {
		f := strings.Split(line, "\x00")
		if len(f) < 6 {
			continue
		}
		b := Branch{Current: f[1] == "*", Upstream: f[2], Track: f[3], Subject: f[5]}
		if sec, err := strconv.ParseInt(f[4], 10, 64); err == nil {
			b.Date = time.Unix(sec, 0)
		}
		switch {
		case strings.HasPrefix(f[0], "refs/heads/"):
			b.Name = strings.TrimPrefix(f[0], "refs/heads/")
			local = append(local, b)
		case strings.HasPrefix(f[0], "refs/remotes/"):
			b.Name, b.Remote = strings.TrimPrefix(f[0], "refs/remotes/"), true
			if strings.HasSuffix(b.Name, "/HEAD") {
				continue
			}
			remote = append(remote, b)
		}
	}
	slices.SortStableFunc(local, func(a, b Branch) int {
		switch {
		case a.Current == b.Current:
			return 0
		case a.Current:
			return -1
		}
		return 1
	})
	return append(local, remote...), nil
}

// LocalName is the local branch a remote one checks out as: origin/feat/x
// is feat/x.
func (b Branch) LocalName() string {
	if !b.Remote {
		return b.Name
	}
	if _, rest, ok := strings.Cut(b.Name, "/"); ok {
		return rest
	}
	return b.Name
}

// Switch checks out branch b: a remote one through the local branch of
// its name, made to track it when there is none yet.
func Switch(ctx context.Context, root string, b Branch, branches []Branch) error {
	name := b.LocalName()
	if b.Remote && !slices.ContainsFunc(branches, func(o Branch) bool { return !o.Remote && o.Name == name }) {
		_, err := Command(ctx, root, "switch", "--track", "-c", name, b.Name)
		return err
	}
	_, err := Command(ctx, root, "switch", name)
	return err
}

// CreateBranch makes a branch at HEAD and checks it out.
func CreateBranch(ctx context.Context, root, name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("a branch needs a name")
	}
	if _, err := Command(ctx, root, "check-ref-format", "--branch", name); err != nil {
		return errors.New("“" + name + "” is not a valid branch name")
	}
	_, err := Command(ctx, root, "switch", "-c", name)
	return err
}

// Commit is a commit of the history, and where the graph draws it.
type Commit struct {
	Hash, Short    string
	Parents        []string
	Author, Email  string
	Date           time.Time
	Refs           []string // branch and tag names pointing at it, HEAD's first
	Subject        string
	CoAuthors      []Person // from its Co-authored-by trailers, each once
	Lane           int      // the column of its dot
	Before, After  []string // the lanes' commits above and below its dot, "" for none
	MergeFromLanes []int    // the lanes that end in it, besides its own
}

// logFormat is the log line Log and ReadCommit read: the fields of a
// commit after a record separator, its co-authors last, apart.
const logFormat = "--format=%x1e%H%x00%h%x00%P%x00%an%x00%ae%x00%at%x00%D%x00%s%x00%(trailers:key=Co-authored-by,valueonly,separator=%x1f)"

// Person is an author or a co-author of a commit.
type Person struct{ Name, Email string }

// emptyTree is git's tree with nothing in it, the parent a first commit
// is compared with.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// Log reads up to n commits of HEAD and its upstream, or of every branch with
// all, skipping the first skip, newest first and each before its parents,
// with the lanes of the graph laid out.
func Log(ctx context.Context, root string, all bool, skip, n int) ([]Commit, error) {
	args := []string{"log", "--topo-order", "--decorate=short", "--no-color",
		logFormat,
		"--skip=" + strconv.Itoa(skip), "-n", strconv.Itoa(n)}
	if all {
		args = append(args, "--branches", "--remotes", "--tags", "HEAD")
	} else {
		args = append(args, "HEAD")
		if upstream, err := Command(ctx, root, "rev-parse", "--verify", "--quiet", "@{upstream}"); err == nil {
			args = append(args, strings.TrimSpace(string(upstream)))
		}
	}
	out, err := Command(ctx, root, args...)
	if err != nil {
		return nil, err
	}
	return ParseLog(out), nil
}

// ReadCommit reads one commit's log line, without the graph.
func ReadCommit(ctx context.Context, root, hash string) (Commit, error) {
	out, err := Command(ctx, root, "log", "-1", "--decorate=short", "--no-color",
		logFormat, hash, "--")
	if err != nil {
		return Commit{}, err
	}
	if cs := ParseLog(out); len(cs) == 1 {
		return cs[0], nil
	}
	return Commit{}, errors.New("no commit " + hash)
}

// ParseLog parses Log's output, without the graph.
func ParseLog(out []byte) []Commit {
	var commits []Commit
	for rec := range strings.SplitSeq(string(out), "\x1e") {
		f := strings.Split(strings.TrimRight(rec, "\n"), "\x00")
		if len(f) < 8 {
			continue
		}
		c := Commit{Hash: f[0], Short: f[1], Author: f[3], Email: f[4], Subject: f[7]}
		if f[2] != "" {
			c.Parents = strings.Fields(f[2])
		}
		if sec, err := strconv.ParseInt(f[5], 10, 64); err == nil {
			c.Date = time.Unix(sec, 0)
		}
		for ref := range strings.SplitSeq(f[6], ", ") {
			switch {
			case ref == "":
			case strings.HasPrefix(ref, "HEAD -> "):
				c.Refs = append([]string{"HEAD", strings.TrimPrefix(ref, "HEAD -> ")}, c.Refs...)
			case ref == "HEAD":
				c.Refs = append([]string{"HEAD"}, c.Refs...)
			case strings.HasSuffix(ref, "/HEAD"):
			default:
				c.Refs = append(c.Refs, strings.TrimPrefix(ref, "tag: "))
			}
		}
		if len(f) > 8 {
			c.CoAuthors = ParseCoAuthors(strings.Split(strings.TrimSpace(f[8]), "\x1f"), c.Email)
		}
		commits = append(commits, c)
	}
	return commits
}

// ParseCoAuthors parses Co-authored-by trailers' values, as
// "Name <email>", each address once and the author's not at all.
func ParseCoAuthors(values []string, author string) []Person {
	var out []Person
	seen := map[string]bool{strings.ToLower(author): true}
	for _, v := range values {
		v = strings.TrimSpace(v)
		lt, gt := strings.LastIndex(v, "<"), strings.LastIndex(v, ">")
		if lt < 0 || gt < lt {
			continue
		}
		p := Person{Name: strings.TrimSpace(v[:lt]), Email: strings.TrimSpace(v[lt+1 : gt])}
		if p.Email == "" || seen[strings.ToLower(p.Email)] {
			continue
		}
		if p.Name == "" {
			p.Name = p.Email
		}
		seen[strings.ToLower(p.Email)] = true
		out = append(out, p)
	}
	return out
}

// Graph lays out the lanes of commits, newest first and each before its
// parents, as git log --graph does: a commit takes the lane that waits
// for it, its first parent goes on in that lane, and its other parents in
// lanes of their own. lanes are those the commits before them left, nil
// at the start, and the result is what they leave for the commits after.
func Graph(commits []Commit, lanes []string) []string {
	lanes = slices.Clone(lanes)
	for i := range commits {
		c := &commits[i]
		c.Before = slices.Clone(lanes)
		c.Lane = slices.Index(lanes, c.Hash)
		if c.Lane < 0 {
			c.Lane = slices.Index(lanes, "")
			if c.Lane < 0 {
				c.Lane = len(lanes)
				lanes = append(lanes, "")
			}
		}
		c.MergeFromLanes = nil
		for j, h := range lanes {
			if h == c.Hash && j != c.Lane {
				c.MergeFromLanes = append(c.MergeFromLanes, j)
				lanes[j] = ""
			}
		}
		lanes[c.Lane] = ""
		if len(c.Parents) > 0 {
			lanes[c.Lane] = c.Parents[0]
		}
		for _, p := range c.Parents[min(1, len(c.Parents)):] {
			if slices.Contains(lanes, p) {
				continue
			}
			if j := slices.Index(lanes, ""); j >= 0 {
				lanes[j] = p
			} else {
				lanes = append(lanes, p)
			}
		}
		for len(lanes) > 0 && lanes[len(lanes)-1] == "" {
			lanes = lanes[:len(lanes)-1]
		}
		c.After = slices.Clone(lanes)
	}
	return lanes
}

// Unpushed are the commits of HEAD the branch it tracks does not have,
// nil without one.
func Unpushed(ctx context.Context, root string) map[string]bool {
	return commitRange(ctx, root, "@{upstream}..HEAD")
}

// Incoming are the upstream's commits HEAD does not have, nil without one.
func Incoming(ctx context.Context, root string) map[string]bool {
	return commitRange(ctx, root, "HEAD..@{upstream}")
}

func commitRange(ctx context.Context, root, revision string) map[string]bool {
	out, err := Command(ctx, root, "rev-list", revision)
	if err != nil {
		return nil
	}
	m := map[string]bool{}
	for _, h := range strings.Fields(string(out)) {
		m[h] = true
	}
	return m
}

// FileStat is a file a commit changed, with the lines added and deleted,
// -1 each for a binary file.
type FileStat struct {
	Path, OldPath  string
	Added, Deleted int
}

// CommitDetails is what Show tells of a commit beyond its log line.
type CommitDetails struct {
	Body                     string
	Committer, CommitterMail string
	CommitDate               time.Time
	Files                    []FileStat
}

// Show reads a commit's message and the files it changed against its
// first parent.
func Show(ctx context.Context, root, hash string) (CommitDetails, error) {
	var d CommitDetails
	out, err := Command(ctx, root, "show", "-s", "--format=%cn%x00%ce%x00%ct%x00%B", hash)
	if err != nil {
		return d, err
	}
	f := strings.SplitN(string(out), "\x00", 4)
	if len(f) == 4 {
		d.Committer, d.CommitterMail, d.Body = f[0], f[1], strings.TrimSpace(f[3])
		if sec, err := strconv.ParseInt(f[2], 10, 64); err == nil {
			d.CommitDate = time.Unix(sec, 0)
		}
	}
	out, err = Command(ctx, root, "diff", "--numstat", "-z", "-M", "--no-color", "--no-ext-diff", Parent(ctx, root, hash), hash)
	if err != nil {
		return d, err
	}
	d.Files = ParseNumstat(out)
	return d, nil
}

// Parent is the commit hash is compared with: its first parent, or the
// empty tree for a first commit.
func Parent(ctx context.Context, root, hash string) string {
	out, err := Command(ctx, root, "rev-parse", "--verify", "--quiet", hash+"^1")
	if err != nil {
		return emptyTree
	}
	return strings.TrimSpace(string(out))
}

// ParseNumstat parses git diff --numstat -z.
func ParseNumstat(out []byte) []FileStat {
	parts := strings.Split(string(out), "\x00")
	var files []FileStat
	for i := 0; i < len(parts); i++ {
		f := strings.SplitN(parts[i], "\t", 3)
		if len(f) < 3 {
			continue
		}
		fs := FileStat{Path: f[2], Added: -1, Deleted: -1}
		if n, err := strconv.Atoi(f[0]); err == nil {
			fs.Added = n
		}
		if n, err := strconv.Atoi(f[1]); err == nil {
			fs.Deleted = n
		}
		if fs.Path == "" && i+2 < len(parts) {
			// A rename: the old path, then the new one.
			fs.OldPath, fs.Path = parts[i+1], parts[i+2]
			i += 2
		}
		files = append(files, fs)
	}
	return files
}

// CommitFileDiff is the patch of one file of a commit against its first
// parent, whole, for a diff view.
func CommitFileDiff(ctx context.Context, root, hash string, f FileStat) ([]byte, error) {
	args := []string{"diff", "--no-ext-diff", "--no-color", "-M", "--unified=2147483647", Parent(ctx, root, hash), hash, "--", f.Path}
	if f.OldPath != "" {
		args = append(args, f.OldPath)
	}
	return Command(ctx, root, args...)
}

// CommitPatch is the patch of every file of a commit against its first
// parent, with git's few lines of context, for the log to show at once.
func CommitPatch(ctx context.Context, root, hash string) ([]byte, error) {
	return Command(ctx, root, "diff", "--no-ext-diff", "--no-color", "-M", Parent(ctx, root, hash), hash)
}

// LineCounts are the lines added and deleted of each changed file, by
// its path: those staged, and those of the working tree not staged.
// Untracked files have none.
func LineCounts(ctx context.Context, root string) (staged, unstaged map[string]FileStat) {
	read := func(args ...string) map[string]FileStat {
		out, err := Command(ctx, root, append([]string{"diff", "--numstat", "-z", "-M", "--no-color", "--no-ext-diff"}, args...)...)
		if err != nil {
			return nil
		}
		m := map[string]FileStat{}
		for _, f := range ParseNumstat(out) {
			m[f.Path] = f
		}
		return m
	}
	return read("--cached"), read()
}
