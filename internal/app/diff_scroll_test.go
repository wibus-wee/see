package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestDiffScrollOverHunkHeader(t *testing.T) {
	var patch strings.Builder
	patch.WriteString("--- a/x\n+++ b/x\n")
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&patch, "@@ -%d,1 +%d,1 @@ section%d\n-old%d\n+new%d\n", i*10+1, i*10+1, i, i, i)
	}
	files, err := editor.ParsePatch(patch.String())
	if err != nil {
		t.Fatal(err)
	}
	d := &editor.DiffState{FitContent: true}
	d.SetPatch(files[0])
	tt := ui.NewTester(func(c *ui.Context) { ui.Scroll(c).Fill().Children(func() { editor.DiffView(c, d).FillWidth() }) }, 700, 300)
	tt.Frame()
	header, ok := tt.Find("@@ -1,1 +1,1 @@ section0")
	if !ok {
		t.Fatalf("header missing: %v", tt.Texts())
	}
	before, ok := tt.Find("@@ -11,1 +11,1 @@ section1")
	if !ok {
		t.Fatalf("second header missing: %v", tt.Texts())
	}
	tt.Scroll(header.X+header.W/2, header.Y+header.H/2, 0, 60)
	tt.Frame()
	after, ok := tt.Find("@@ -11,1 +11,1 @@ section1")
	if !ok || after.Y >= before.Y {
		t.Fatalf("scroll over header: before=%+v after=%+v visible=%v", before, after, ok)
	}
	codeBefore, ok := tt.Find("@@ -21,1 +21,1 @@ section2")
	if !ok {
		t.Fatal("third header missing")
	}
	tt.Scroll(500, 160, 0, 35)
	tt.Frame()
	next, ok := tt.Find("@@ -21,1 +21,1 @@ section2")
	if !ok || next.Y >= codeBefore.Y {
		t.Fatalf("scroll over code: before=%+v after=%+v visible=%v", codeBefore, next, ok)
	}
}

func TestDiffHorizontalScrollMovesBothSides(t *testing.T) {
	line := strings.Repeat("abcdefghij", 30)
	d := &editor.DiffState{Old: editor.NewBuffer(line + "\nold"), New: editor.NewBuffer(line + "\nnew"), ExpandUnchanged: true}
	tt := ui.NewTester(func(c *ui.Context) { editor.DiffView(c, d).Fill() }, 700, 300)
	tt.Frame()
	before := tt.Image()
	tt.Scroll(200, 10, 100, 0)
	tt.Frame()
	after := tt.Image()
	for _, x := range []int{50, 400} {
		changed := 0
		for y := 0; y < 20; y++ {
			for dx := 0; dx < 250; dx++ {
				if before.At(x+dx, y) != after.At(x+dx, y) {
					changed++
				}
			}
		}
		if changed == 0 {
			t.Fatalf("horizontal scroll did not move the side at x=%d", x)
		}
	}
}
