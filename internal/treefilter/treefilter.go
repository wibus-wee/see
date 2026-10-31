// Package treefilter builds the file tree's filter, as Xcode's navigator
// filter: the files whose paths hold every word of a query, with the
// folders they are in, so that the tree can show a match in a folder it
// has not opened.
package treefilter

import (
	"cmp"
	"path/filepath"
	"slices"
	"strings"

	"see/internal/workspace"
)

// BuildTree returns, for the files under root, the matching entries of
// each folder, the root's entries, and how many files matched. Paths are
// absolute, as the tree keeps them.
func BuildTree(root string, files []string, query string) (map[string][]string, []string, int) {
	words := strings.Fields(strings.ToLower(query))
	tree := map[string][]string{}
	seen := map[string]bool{}
	matches := 0
	for _, rel := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if !workspace.Within(root, p) || !HoldsWords(workspace.RelPath(root, p), words) {
			continue
		}
		matches++
		for p != root {
			parent := filepath.Dir(p)
			if !seen[p] {
				seen[p] = true
				tree[parent] = append(tree[parent], p)
			}
			if !workspace.Within(root, parent) {
				break
			}
			p = parent
		}
	}
	for _, entries := range tree {
		sortEntries(tree, entries)
	}
	return tree, tree[root], matches
}

// HoldsWords reports whether path holds every word, ignoring case.
func HoldsWords(path string, words []string) bool {
	path = strings.ToLower(filepath.ToSlash(path))
	for _, w := range words {
		if !strings.Contains(path, w) {
			return false
		}
	}
	return true
}

// sortEntries puts a folder's directories before its files, each by name
// ignoring case, as the navigator shows them.
func sortEntries(tree map[string][]string, entries []string) {
	slices.SortFunc(entries, func(a, b string) int {
		if da, db := tree[a] != nil, tree[b] != nil; da != db {
			if da {
				return -1
			}
			return 1
		}
		return cmp.Compare(strings.ToLower(filepath.Base(a)), strings.ToLower(filepath.Base(b)))
	})
}
