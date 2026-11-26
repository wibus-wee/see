package app

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"see/internal/workspace"
)

// treeRowStep is the indent each level adds in the project navigator and
// the changes tree. treeArrowSize is the width every row keeps for its
// disclosure arrow, whether or not it has one.
const (
	treeRowStep   = 12
	treeArrowSize = 8
)

// treeInset returns the leading inset of a tree row at depth.
func treeInset(base float32, depth int) float32 {
	return base + float32(depth)*treeRowStep
}

// treeChevron draws a tree row's disclosure arrow, turned toward the
// content when open.
func treeChevron(c *ui.Context, open bool) {
	turn := float32(0)
	if open {
		turn = 90
	}
	symbolIcon(c, icChevron, treeArrowSize, colors(c).Dim).Rotate(turn)
}

// treeArrowSpace keeps a leaf's arrow column empty, so that files and
// folders share the same icon and name columns.
func treeArrowSpace(c *ui.Context) {
	ui.Box(c).Size(treeArrowSize, treeArrowSize).Shrink(0)
}

// treeLabelOptions are what treeLabel draws: a folder's open or closed
// icon, or a file's icon, then the name.
type treeLabelOptions struct {
	Name     string
	Dir      bool
	Open     bool
	IconSize float32
	FontSize float32
	Dim      bool
}

// treeIcon draws a folder's open or closed icon, or a file's icon.
func treeIcon(c *ui.Context, name string, dir, open bool, size float32) {
	if dir {
		folderGlyph(c, open, size)
	} else {
		fileIcon(c, name, size)
	}
}

// treeLabel draws a row of a file tree and returns its name's text, for
// callers to decorate as their tree needs.
func treeLabel(c *ui.Context, o treeLabelOptions) ui.Element {
	treeIcon(c, o.Name, o.Dir, o.Open, o.IconSize)
	t := ui.Text(c, o.Name).SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0).Shrink(1)
	if o.FontSize > 0 {
		t.FontSize(o.FontSize)
	}
	if o.Dim {
		t.TextColor(colors(c).Dim)
	}
	return t
}

// pathTreeNode is an item of a path tree, with the folders leading to it.
type pathTreeNode[T any] struct {
	name     string
	path     string
	item     T
	leaf     bool
	children []*pathTreeNode[T]
}

// buildPathTree arranges items into folders by their path, folders first
// and each level in natural name order.
func buildPathTree[T any](items []T, path func(T) string) []*pathTreeNode[T] {
	root := &pathTreeNode[T]{}
	for i := range items {
		itemPath := filepath.ToSlash(path(items[i]))
		parts := strings.Split(itemPath, "/")
		node := root
		for j, part := range parts {
			if j == len(parts)-1 {
				node.children = append(node.children, &pathTreeNode[T]{name: part, path: itemPath, item: items[i], leaf: true})
				break
			}
			dirPath := strings.Join(parts[:j+1], "/")
			var dir *pathTreeNode[T]
			for _, child := range node.children {
				if !child.leaf && child.name == part {
					dir = child
					break
				}
			}
			if dir == nil {
				dir = &pathTreeNode[T]{name: part, path: dirPath}
				node.children = append(node.children, dir)
			}
			node = dir
		}
	}
	sortPathTree(root.children)
	return root.children
}

func sortPathTree[T any](nodes []*pathTreeNode[T]) {
	slices.SortStableFunc(nodes, func(a, b *pathTreeNode[T]) int {
		if a.leaf != b.leaf {
			if !a.leaf {
				return -1
			}
			return 1
		}
		return workspace.Compare(a.name, b.name)
	})
	for _, node := range nodes {
		if !node.leaf {
			sortPathTree(node.children)
		}
	}
}

// walkPathTree visits each row shown, skipping the children of the
// folders closed reports as folded.
func walkPathTree[T any](nodes []*pathTreeNode[T], closed func(path string) bool, visit func(node *pathTreeNode[T], depth int)) {
	var walk func([]*pathTreeNode[T], int)
	walk = func(nodes []*pathTreeNode[T], depth int) {
		for _, node := range nodes {
			visit(node, depth)
			if !node.leaf && !closed(node.path) {
				walk(node.children, depth+1)
			}
		}
	}
	walk(nodes, 0)
}
