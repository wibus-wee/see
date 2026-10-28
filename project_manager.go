package main

type projectWindowHost interface {
	Show()
	Focus()
	Update(func())
	IsDestroyed() bool
}

// projectWindow owns the state and native window of one open workspace.
type projectWindow struct {
	app *app
	win projectWindowHost
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
