package git

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
)

// BranchDiffFile is a file changed since the merge base; Untracked files
// are present in the working tree but not in Git's index.
type BranchDiffFile struct {
	FileStat
	Untracked bool
}

// BranchDiffResult compares a branch's merge base with the working tree.
type BranchDiffResult struct {
	BaseHash, MergeBase string
	Files               []BranchDiffFile
	Patch               []byte
}

// CompareBranch compares the working tree with the merge base of baseRef and
// HEAD. It resolves the ref before passing only commit hashes to diff.
func CompareBranch(ctx context.Context, root, baseRef string) (BranchDiffResult, error) {
	var result BranchDiffResult
	base, err := Command(ctx, root, "rev-parse", "--verify", "--end-of-options", baseRef+"^{commit}")
	if err != nil {
		return result, fmt.Errorf("resolve base %q: %w", baseRef, err)
	}
	head, err := Command(ctx, root, "rev-parse", "--verify", "--end-of-options", "HEAD^{commit}")
	if err != nil {
		return result, fmt.Errorf("resolve HEAD: %w", err)
	}
	baseHash, headHash := strings.TrimSpace(string(base)), strings.TrimSpace(string(head))
	mergeBase, err := Command(ctx, root, "merge-base", baseHash, headHash)
	if err != nil {
		return result, fmt.Errorf("find merge base for %q and HEAD: %w", baseRef, err)
	}
	mergeBaseHash := strings.TrimSpace(string(mergeBase))
	numstat, err := Command(ctx, root, "diff", "--numstat", "-z", "-M", "--no-ext-diff", "--no-textconv", "--no-color", mergeBaseHash, "--")
	if err != nil {
		return result, err
	}
	patch, err := Command(ctx, root, "diff", "--no-ext-diff", "--no-textconv", "--no-color", "-M", mergeBaseHash, "--")
	if err != nil {
		return result, err
	}
	for _, f := range ParseNumstat(numstat) {
		result.Files = append(result.Files, BranchDiffFile{FileStat: f})
	}
	result.Patch = append(result.Patch, patch...)
	untracked, err := Command(ctx, root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return result, err
	}
	for _, path := range strings.Split(string(untracked), "\x00") {
		if path == "" {
			continue
		}
		stat, err := noIndexDiff(ctx, root, path, "--numstat", "-z")
		if err != nil {
			return result, fmt.Errorf("read untracked file %q: %w", path, err)
		}
		files := ParseNumstat(stat)
		f := BranchDiffFile{FileStat: FileStat{Path: path}, Untracked: true}
		if len(files) > 0 {
			f.Added, f.Deleted = files[0].Added, files[0].Deleted
		}
		filePatch, err := noIndexDiff(ctx, root, path)
		if err != nil {
			return result, fmt.Errorf("read untracked file %q: %w", path, err)
		}
		result.Files = append(result.Files, f)
		result.Patch = append(result.Patch, filePatch...)
	}
	result.BaseHash, result.MergeBase = baseHash, mergeBaseHash
	return result, nil
}

func noIndexDiff(ctx context.Context, root, path string, options ...string) ([]byte, error) {
	args := append([]string{"diff", "--no-index", "--no-ext-diff", "--no-textconv", "--no-color"}, options...)
	args = append(args, "--", os.DevNull, path)
	// --no-index exits 1 when it has produced the requested difference.
	out, err := Command(ctx, root, args...)
	if err == nil {
		return out, nil
	}
	if bytes.HasPrefix(out, []byte("diff --git ")) {
		return out, nil
	}
	if len(ParseNumstat(out)) > 0 {
		return out, nil
	}
	return nil, err
}
