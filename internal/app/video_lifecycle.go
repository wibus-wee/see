package app

import (
	"os"

	"see/internal/workspace"
)

// Videos are streamed by the native player rather than loaded as source text.
func readDocumentFile(path string) (workspace.File, error) {
	if !isVideoPath(path) {
		return workspace.ReadFile(path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return workspace.File{}, err
	}
	if !info.Mode().IsRegular() {
		return workspace.File{}, workspace.ErrNotRegular
	}
	return workspace.File{Kind: workspace.Binary, Size: info.Size(), ModTime: info.ModTime()}, nil
}

func (g *group) closeVideo(path string) {
	if v := g.videos[path]; v != nil {
		if v.player != nil {
			v.player.Close()
		}
		delete(g.videos, path)
	}
}

func (a *App) closeDocumentVideos(path string) {
	for _, g := range a.groups() {
		g.closeVideo(path)
	}
}

func (a *App) pauseHiddenVideos() {
	for _, g := range a.groups() {
		for path, v := range g.videos {
			if v.player != nil && v.player.Playing() && (g.active != path || g.hexPreview[path]) {
				v.player.Pause()
			}
		}
	}
}
