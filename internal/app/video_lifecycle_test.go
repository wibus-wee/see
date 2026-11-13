package app

import (
	"errors"
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"see/internal/platform"
	"see/internal/workspace"
)

func TestVideoFileStreamsBeyondTextLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clip.mp4")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	err = f.Truncate(workspace.MaxSize + 1)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	got, err := readDocumentFile(path)
	if err != nil || got.Kind != workspace.Binary || got.Size != workspace.MaxSize+1 || got.Text != "" {
		t.Fatalf("video metadata = %+v, %v", got, err)
	}
	if _, err := readDocumentFile(path + ".missing.mp4"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing video: %v", err)
	}
	dir := filepath.Join(t.TempDir(), "folder.mp4")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := readDocumentFile(dir); !errors.Is(err, workspace.ErrNotRegular) {
		t.Fatalf("directory video: %v", err)
	}
}

type videoTestDecoder struct{ closed chan struct{} }

func (d *videoTestDecoder) Next() (ui.VideoFrame, error) {
	return ui.VideoFrame{Image: image.NewRGBA(image.Rect(0, 0, 1, 1)), Duration: time.Second}, nil
}
func (d *videoTestDecoder) Rewind() error { return nil }
func (d *videoTestDecoder) Close() error  { close(d.closed); return nil }

func testVideoPlayer(t *testing.T) (*ui.VideoPlayer, <-chan struct{}) {
	t.Helper()
	d := &videoTestDecoder{closed: make(chan struct{})}
	p, err := ui.NewVideoPlayer(d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p, d.closed
}

func TestVideoVisibilityAndRelease(t *testing.T) {
	p, closed := testVideoPlayer(t)
	other, _ := testVideoPlayer(t)
	g := &group{active: "clip.mp4", videos: map[string]*videoPreview{"clip.mp4": {player: p}}}
	split := &group{active: "clip.mp4", videos: map[string]*videoPreview{"clip.mp4": {player: other}}}
	a := &App{layout: &layout{a: &layout{g: g}, b: &layout{g: split}}}
	a.pauseHiddenVideos()
	if !p.Playing() || !other.Playing() {
		t.Fatal("visible split videos paused")
	}
	g.active = "source.go"
	a.pauseHiddenVideos()
	if p.Playing() || !other.Playing() {
		t.Fatal("hidden player must pause independently")
	}
	g.active = "clip.mp4"
	a.pauseHiddenVideos()
	if p.Playing() {
		t.Fatal("returning to a tab unexpectedly resumed audio")
	}
	g.hexPreview = map[string]bool{"clip.mp4": true}
	p.Play()
	a.pauseHiddenVideos()
	if p.Playing() {
		t.Fatal("hex view kept playing audio")
	}
	g.closeVideo("clip.mp4")
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("decoder not released")
	}
	if len(g.videos) != 0 || !other.Playing() {
		t.Fatal("closing one split affected the other")
	}
	a.closeDocumentVideos("clip.mp4")
	if len(split.videos) != 0 {
		t.Fatal("document invalidation kept a player")
	}
}

func TestVideoRefreshPreservesUnchangedPlayer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clip.mp4")
	if err := os.WriteFile(path, []byte("video fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := readDocumentFile(path)
	if err != nil {
		t.Fatal(err)
	}
	player, _ := testVideoPlayer(t)
	d := &doc{path: path, name: "clip.mp4", video: true, stamp: fileStamp{f.Size, f.ModTime}}
	g := &group{active: path, videos: map[string]*videoPreview{path: {player: player}}}
	callbacks := make(chan func(), 1)
	a := &App{docs: map[string]*doc{path: d}, layout: &layout{g: g}, post: platform.SchedulerFunc(func(fn func()) { callbacks <- fn })}
	a.loadDoc(d)
	select {
	case fn := <-callbacks:
		fn()
	case <-time.After(time.Second):
		t.Fatal("refresh did not complete")
	}
	if d.loading || d.err != nil || g.videos[path] == nil || g.videos[path].player != player || !player.Playing() {
		t.Fatal("unchanged video refresh discarded playback state")
	}
}

func TestVideoReloadPreservesUnsavedText(t *testing.T) {
	a := &App{}
	buf := editor.NewBuffer("original")
	buf.Set("unsaved edits")
	d := &doc{path: "renamed.mp4", buf: buf, diskText: "original"}
	a.loaded(d, workspace.File{Kind: workspace.Binary}, nil, false, nil)
	if d.video || d.buf.Snapshot().String() != "unsaved edits" || !d.buf.Modified() || !d.conflict {
		t.Fatal("media reload replaced unsaved text")
	}
}
