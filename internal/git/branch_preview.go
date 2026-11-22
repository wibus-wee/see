package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const branchPreviewCommitCount = 3

// BranchPreview is the committed history and file changes a branch switch
// would bring, compared with the current commit.
type BranchPreview struct {
	TargetRef  string
	TargetHash string
	Commits    []Commit
	Changes    []BranchFileChange
}

// BranchFileChange is a path changed between two committed trees.
type BranchFileChange struct {
	Status        byte
	Path, OldPath string
}

// PreviewBranch reads the commit and files of the branch Switch would check
// out. It does not change HEAD or the working tree.
func PreviewBranch(ctx context.Context, root string, b Branch, branches []Branch) (BranchPreview, error) {
	ref := branchRef(b)
	if b.Remote {
		local := b.LocalName()
		for _, candidate := range branches {
			if !candidate.Remote && candidate.Name == local {
				ref = branchRef(candidate)
				break
			}
		}
	}
	target, err := resolveCommit(ctx, root, ref)
	if err != nil {
		return BranchPreview{}, fmt.Errorf("resolve branch %q: %w", b.Name, err)
	}
	head, err := resolveCommit(ctx, root, "HEAD")
	if err != nil {
		return BranchPreview{}, fmt.Errorf("resolve HEAD: %w", err)
	}
	commits, err := branchPreviewCommits(ctx, root, target)
	if err != nil {
		return BranchPreview{}, err
	}
	changes, err := branchPreviewChanges(ctx, root, head, target)
	if err != nil {
		return BranchPreview{}, err
	}
	return BranchPreview{TargetRef: ref, TargetHash: target, Commits: commits, Changes: changes}, nil
}

func branchRef(b Branch) string {
	if b.Remote {
		return "refs/remotes/" + b.Name
	}
	return "refs/heads/" + b.Name
}

func resolveCommit(ctx context.Context, root, ref string) (string, error) {
	out, err := Command(ctx, root, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	hash := strings.TrimSpace(string(out))
	if hash == "" {
		return "", errors.New("no commit")
	}
	return hash, nil
}

func branchPreviewCommits(ctx context.Context, root, target string) ([]Commit, error) {
	out, err := Command(ctx, root, "log", "--no-color", "--no-decorate", "-n", fmt.Sprint(branchPreviewCommitCount), logFormat, target, "--")
	if err != nil {
		return nil, err
	}
	return ParseLog(out), nil
}

func branchPreviewChanges(ctx context.Context, root, head, target string) ([]BranchFileChange, error) {
	out, err := Command(ctx, root, "diff", "--name-status", "-z", "--no-ext-diff", "--no-textconv", "--find-renames", head, target, "--")
	if err != nil {
		return nil, err
	}
	return parseBranchFileChanges(out)
}

func parseBranchFileChanges(out []byte) ([]BranchFileChange, error) {
	if len(out) == 0 {
		return nil, nil
	}
	fields := strings.Split(string(out), "\x00")
	if fields[len(fields)-1] != "" {
		return nil, errors.New("malformed git name-status output")
	}
	fields = fields[:len(fields)-1]
	var changes []BranchFileChange
	for i := 0; i < len(fields); {
		status := fields[i]
		i++
		if status == "" || i >= len(fields) {
			return nil, errors.New("malformed git name-status output")
		}
		change := BranchFileChange{Status: status[0], Path: fields[i]}
		i++
		if status[0] == 'R' || status[0] == 'C' {
			if i >= len(fields) {
				return nil, errors.New("malformed git rename output")
			}
			change.OldPath, change.Path = change.Path, fields[i]
			i++
		}
		if change.Status != 'A' && change.Status != 'D' && change.Status != 'R' {
			change.Status = 'M'
		}
		changes = append(changes, change)
	}
	return changes, nil
}
