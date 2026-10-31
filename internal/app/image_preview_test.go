package app

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
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

func TestCopyTreeFileAndImagePreview(t *testing.T) {
	root := writeTree(t, map[string]string{"file.txt": "hello"})
	path := filepath.Join(root, "picture.png")
	writePreviewImage(t, path, 20, 10)
	h := newHarness(t, root)
	var copiedFile string
	var copiedImage []byte
	h.host.copy = func(paths ...string) error { copiedFile = paths[0]; return nil }
	h.host.copyPNG = func(data []byte) error { copiedImage = data; return nil }
	h.click("file.txt")
	h.tt.Key(ui.Cmd, ui.KeyC)
	if copiedFile != filepath.Join(root, "file.txt") {
		t.Fatalf("copied file = %q", copiedFile)
	}
	h.open("picture.png")
	h.tt.Key(ui.Cmd, ui.KeyC)
	h.settle(func() bool { return copiedImage != nil })
	img, err := png.Decode(bytes.NewReader(copiedImage))
	if err != nil || img.Bounds().Dx() != 20 || img.Bounds().Dy() != 10 {
		t.Fatalf("copied image: %v", err)
	}
	if copiedFile != filepath.Join(root, "file.txt") {
		t.Fatal("image copy also copied the tree selection")
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
