package git

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// DiscoverRepositories finds the opened repository and Git repositories in
// its immediate child folders. Child scans stop at one level, as a folder
// opened in See should not silently turn into a recursive workspace scan.
func DiscoverRepositories(ctx context.Context, workspaceRoot string) ([]string, error) {
	root, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var repositories []string
	add := func(dir string) {
		out, err := Command(ctx, dir, "rev-parse", "--show-toplevel")
		if err == nil {
			repository := filepath.Clean(strings.TrimSpace(string(out)))
			if repository != "." && !slices.Contains(repositories, repository) {
				repositories = append(repositories, repository)
			}
		}
	}
	add(root)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			add(dir)
		}
	}
	slices.Sort(repositories)
	return repositories, nil
}
