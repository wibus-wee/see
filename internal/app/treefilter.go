package app

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"see/internal/treefilter"
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
func (a *App) filtering() bool { return strings.TrimSpace(a.filter.query) != "" }

// syncFilter builds the tree of the filter's matches when the query or the
// file list changed, and puts the tree back as it was when the filter
// clears.
func (a *App) syncFilter() {
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
	f.tree, f.roots, f.matches = treefilter.BuildTree(a.root, a.quick.files, q)
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
