package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"see/internal/workspace"
)

func isImagePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return true
	}
	return false
}

func readImage(path string) (*ui.Bitmap, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, workspace.MaxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > workspace.MaxSize {
		return nil, fmt.Errorf("image is too large to preview")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("cannot decode image: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 64_000_000 {
		return nil, fmt.Errorf("image dimensions are too large to preview")
	}
	bitmap, err := ui.DecodeBitmap(data)
	if err != nil {
		return nil, fmt.Errorf("cannot decode image: %w", err)
	}
	return bitmap, nil
}

func (a *app) imagePreview(c *ui.Context, g *group, d *doc) {
	w, h := d.image.Size()
	preview := ui.Column(c).Grow(1).FillWidth().Padding(20).Gap(10).AlignItems(ui.Center).
		Focusable().FocusRing(false).Label("Image preview area").OnShortcut(ui.Cmd, ui.KeyC, func() { a.copyPreviewImage(d.path) })
	if a.focusImage == d.path && a.focus == g {
		preview.Focus()
		a.focusImage = ""
	}
	preview.Children(func() {
		ui.Box(c).Grow(1).FillWidth().MinHeight(0).Center().Children(func() {
			ui.Image(c, d.image).Fill().Fit(ui.ScaleDown).Label("Image preview")
		})
		ui.Text(c, fmt.Sprintf("%d × %d · %s", w, h, size(d.size))).FontSize(11).TextColor(colors(c).muted).SingleLine()
	})
}

func (a *app) copyFile(path string) {
	if err := a.host.copyFiles(path); err != nil {
		a.note("Could not copy file: "+describeErr(err), true)
	}
}

func (a *app) copyPreviewImage(path string) {
	go func() {
		bitmap, err := imagePNG(path)
		a.post(func() {
			if err == nil {
				err = a.host.copyImage(bitmap)
			}
			if err != nil {
				a.note("Could not copy image: "+describeErr(err), true)
			}
		})
	}()
}

func imagePNG(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, workspace.MaxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > workspace.MaxSize {
		return nil, fmt.Errorf("image is too large to copy")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 64_000_000 {
		return nil, fmt.Errorf("image dimensions are too large to copy")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
