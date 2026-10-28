package main

import (
	"cmp"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
)

// treeFilter is the file tree's filter, as Xcode's navigator filter: the
// files whose paths hold every word of the query, and the folders they are
// in, open. It filters the workspace's file list, so it finds files in
// folders the tree has not opened yet.
type treeFilter struct {
	query     string              // what the field holds
	tree      map[string][]string // a folder's matching entries, by path
	roots     []string            // the root's entries
	forQuery  string              // the query tree is built for
	matches   int                 // the files that match, not counting their folders
	field     ui.Handle           // the field, which Filter Files focuses
	focus     bool                // the field is to take the keys
	toList    bool                // the tree is to take the keys, as Down in the field asks
	savedOpen []string            // the folders open before filtering began
	saved     bool
}

// filtering reports whether the tree shows a filter's matches.
func (a *app) filtering() bool { return strings.TrimSpace(a.filter.query) != "" }

// syncFilter builds the tree of the filter's matches when the query or the
// file list changed, and puts the tree back as it was when the filter
// clears.
func (a *app) syncFilter() {
	f := &a.filter
	q := strings.TrimSpace(f.query)
	if q == "" {
		if f.saved {
			a.tree.Open.Clear()
			for _, dir := range f.savedOpen {
				a.tree.Open.Add(dir)
			}
			f.saved = false
		}
		f.forQuery, f.tree, f.roots, f.matches = "", nil, nil, 0
		return
	}
	if f.forQuery == q {
		return
	}
	if a.quick.files == nil {
		a.ensureFiles()
		f.forQuery, f.tree, f.roots, f.matches = q, nil, nil, 0
		return
	}
	f.forQuery = q
	f.tree, f.roots, f.matches = buildFilterTree(a.root, a.quick.files, q)
	if !f.saved {
		f.saved = true
		f.savedOpen = slices.Collect(a.tree.Open.All())
	}
	// Every folder a match is in opens, as a filter's outline shows its
	// matches at once; clearing the filter puts back the folders that
	// were open before.
	a.tree.Open.Clear()
	for dir := range f.tree {
		a.tree.Open.Add(dir)
	}
	if a.treeRow >= a.tree.Rows() {
		a.treeRow = -1
	}
}

// buildFilterTree builds the tree of the files whose relative paths hold
// every word of query, with the folders they are in. Paths are absolute,
// as the tree keeps them.
func buildFilterTree(root string, files []string, query string) (map[string][]string, []string, int) {
	words := strings.Fields(strings.ToLower(query))
	tree := map[string][]string{}
	seen := map[string]bool{}
	matches := 0
	for _, rel := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if !within(root, p) || !holdsWords(relPath(root, p), words) {
			continue
		}
		matches++
		for p != root {
			parent := filepath.Dir(p)
			if !seen[p] {
				seen[p] = true
				tree[parent] = append(tree[parent], p)
			}
			if !within(root, parent) {
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

// holdsWords reports whether path holds every word, ignoring case.
func holdsWords(path string, words []string) bool {
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
