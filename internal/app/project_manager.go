package app

import (
	"github.com/egoist/mygo"

	"see/internal/config"
)

type projectWindowHost interface {
	Show()
	Focus()
	Update(func())
	IsDestroyed() bool
	SetVibrancy(mygo.Vibrancy)
}

// projectWindow owns the state and native window of one open workspace.
type projectWindow struct {
	app *App
	win projectWindowHost
}

func projectWindowVibrancy(cfg *config.Config) mygo.Vibrancy {
	if !cfg.BetaEnabled(config.BetaSidebarVibrancy) {
		return mygo.VibrancyNone
	}
	return mygo.Vibrancy(cfg.BetaString(config.BetaSidebarVibrancy, "material"))
}

func applyVibrancyToProjects(projects []*projectWindow, cfg *config.Config) {
	material := projectWindowVibrancy(cfg)
	for _, project := range projects {
		if project == nil || project.win == nil || project.win.IsDestroyed() {
			continue
		}
		project.win.SetVibrancy(material)
		project.win.Update(func() {})
	}
}

func applyEditorPrefsToProjects(projects []*projectWindow) {
	for _, project := range projects {
		app := project.app
		app.post.Post(func() { app.applyEditorPrefsAll() })
	}
}

// approveProjectQuit asks each window with unsaved files before quitting.
func approveProjectQuit(projects []*projectWindow, done func()) {
	approved := make(map[*doc]uint64)
	var next func()
	next = func() {
		for _, p := range projects {
			if p.win == nil || p.win.IsDestroyed() {
				continue
			}
			var pending []*doc
			for _, d := range p.app.dirtyDocs() {
				if version, ok := approved[d]; !ok || version != d.buf.Version() {
					pending = append(pending, d)
				}
			}
			if len(pending) == 0 {
				continue
			}
			p.win.Show()
			p.win.Focus()
			p.win.Update(func() {
				p.app.showWelcome = false
				proceed := func() {
					for _, d := range pending {
						if d.buf != nil {
							approved[d] = d.buf.Version()
						}
					}
					next()
				}
				if !p.app.guardDirty(pending, proceed) {
					proceed()
				}
			})
			return
		}
		done()
	}
	next()
}
