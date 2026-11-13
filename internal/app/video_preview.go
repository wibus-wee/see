package app

import (
	"fmt"
	"path/filepath"
	"see/locales"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/video"
)

type videoPreview struct {
	player *ui.VideoPlayer
	err    error
}

func isVideoPath(path string) bool {
	if strings.Contains(path, "://") {
		return false
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".m4v", ".mov", ".webm", ".mkv", ".avi", ".wmv":
		return true
	}
	return false
}

func (a *App) videoPreview(c *ui.Context, g *group, d *doc) {
	if g.videos == nil {
		g.videos = make(map[string]*videoPreview)
	}
	v := g.videos[d.path]
	if v == nil {
		v = &videoPreview{}
		g.videos[d.path] = v
		v.player, v.err = video.Open(d.path)
	}
	if v.err == nil && v.player != nil {
		v.err = v.player.Err()
	}
	if v.err != nil {
		if v.player != nil {
			v.player.Close()
		}
		a.message(c, locales.VideoFailed(), v.err.Error(), locales.Retry(), func() {
			if v.player != nil {
				v.player.Close()
			}
			delete(g.videos, d.path)
		})
		return
	}
	p := v.player
	ui.Column(c).Grow(1).FillWidth().Padding(16).Gap(10).Label(locales.VideoPreview()).Children(func() {
		ui.Box(c).Grow(1).FillWidth().MinHeight(0).Children(func() {
			ui.Video(c, p).Fill().Fit(ui.Contain).Label(locales.VideoPreview())
		})
		if !p.Ready() {
			ui.Text(c, locales.MsgLoading()).TextColor(colors(c).Muted)
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			label := locales.VideoPlay()
			if p.Playing() {
				label = locales.VideoPause()
			}
			if ui.Button(c.Key("video-play"), label).Clicked() {
				if p.Playing() {
					p.Pause()
				} else {
					p.Play()
				}
			}
			pos, dur := p.Position().Seconds(), p.Duration().Seconds()
			if ui.Slider(c.Key("video-seek"), &pos, 0, max(dur, 0.001)).Grow(1).Disabled(dur <= 0).Label(locales.VideoSeek()).Changed() {
				p.Seek(time.Duration(pos * float64(time.Second)))
			}
			clock := func(t time.Duration) string { s := int(t / time.Second); return fmt.Sprintf("%d:%02d", s/60, s%60) }
			ui.Text(c, clock(p.Position())+" / "+clock(p.Duration())).SingleLine()
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			label := locales.VideoMute()
			if p.Muted() {
				label = locales.VideoUnmute()
			}
			if ui.Button(c.Key("video-mute"), label).Clicked() {
				p.SetMuted(!p.Muted())
			}
			ui.Text(c, locales.VideoVolume())
			volume := p.Volume()
			if ui.Slider(c.Key("video-volume"), &volume, 0, 1).Width(140).Label(locales.VideoVolume()).Changed() {
				p.SetVolume(volume)
			}
			loop := p.Loop()
			ui.Checkbox(c.Key("video-loop"), &loop, locales.VideoLoop()).OnClick(func() { p.SetLoop(loop) })
		})
	})
}
