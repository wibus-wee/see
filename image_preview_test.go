package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func writePreviewImage(t *testing.T, path string, width, height int) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{255, 30, 20, 255})
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestImagePreviewAndReload(t *testing.T) {
	root := writeTree(t, map[string]string{"broken.png": "not an image"})
	path := filepath.Join(root, "picture.png")
	writePreviewImage(t, path, 20, 10)
	h := newHarness(t, root)
	d := h.open("picture.png")
	if d.err != nil || d.image == nil || !h.tt.HasText("20 × 10") {
		t.Fatalf("preview: image=%v error=%v texts=%v", d.image != nil, d.err, h.tt.Texts())
	}
	if r, ok := h.tt.Find("Image preview"); !ok || r.W < 20 || r.H < 10 {
		t.Fatalf("image has no visible area: %+v", r)
	}
	writePreviewImage(t, path, 30, 15)
	h.a.reloadDoc(d)
	h.settle(func() bool { return !d.loading })
	if d.err != nil || !h.tt.HasText("30 × 15") {
		t.Fatalf("reload: %v %v", d.err, h.tt.Texts())
	}
	broken := h.open("broken.png")
	if broken.err == nil || !h.tt.HasText("Could not open broken.png") {
		t.Fatal("corrupt image did not show an error")
	}
}
