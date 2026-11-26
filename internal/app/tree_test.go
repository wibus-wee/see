package app

import (
	"reflect"
	"testing"
)

func TestPathTreeNestsAndFolds(t *testing.T) {
	items := []string{"b.txt", "src/z.go", "src/a.go", "src/nested/x.go", "A.md"}
	nodes := buildPathTree(items, func(s string) string { return s })

	type row struct {
		name  string
		depth int
		leaf  bool
	}
	var rows []row
	walkPathTree(nodes, func(path string) bool { return path == "src/nested" }, func(node *pathTreeNode[string], depth int) {
		rows = append(rows, row{node.name, depth, node.leaf})
	})
	want := []row{
		{"src", 0, false},
		{"nested", 1, false},
		{"a.go", 1, true},
		{"z.go", 1, true},
		{"A.md", 0, true},
		{"b.txt", 0, true},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("path tree rows = %+v, want %+v", rows, want)
	}
}

func TestPathTreeSortsFoldersBeforeFiles(t *testing.T) {
	nodes := buildPathTree([]string{"z.txt", "a/inner.go", "m.go"}, func(s string) string { return s })
	var names []string
	for _, node := range nodes {
		names = append(names, node.name)
	}
	if want := []string{"a", "m.go", "z.txt"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("root nodes = %v, want %v", names, want)
	}
}
