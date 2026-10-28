package main

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestTreeClickAcrossRow(t *testing.T) {
	root := writeTree(t, map[string]string{"folder/child.go": "package child", "file.go": "package file"})
	for _, tc := range []struct {
		name             string
		xOffset, yOffset float32
		dir              bool
	}{
		{"folder", 2, 0, true}, {"folder", 200, 0, true}, {"folder", 2, -9, true},
		{"file.go", 2, 0, false}, {"file.go", 200, 0, false}, {"file.go", 2, -9, false},
	} {
		t.Run(tc.name+fmtOffsets(tc.xOffset, tc.yOffset), func(t *testing.T) {
			h := newHarness(t, root)
			r, ok := h.tt.Find(tc.name)
			if !ok {
				t.Fatal("missing row")
			}
			h.tt.ClickAt(r.X+tc.xOffset, r.Y+r.H/2+tc.yOffset)
			p := filepath.Join(root, tc.name)
			if tc.dir && !h.a.tree.Open.Has(p) {
				t.Fatal("folder did not open")
			}
			if !tc.dir && h.a.activePath() != p {
				t.Fatal("file did not open")
			}
		})
	}
}

func fmtOffsets(x, y float32) string { return fmt.Sprintf("_%g_%g", x, y) }
