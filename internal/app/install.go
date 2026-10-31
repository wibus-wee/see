package app

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/egoist/mygo/ui"

	"see/internal/langserver"
	"see/internal/languages"
)

func (a *App) installManagedTypeScript(services ui.Services) {
	if a.serverJob {
		return
	}
	a.serverJob, a.serverJobErr = true, ""
	dest := languages.ManagedTypeScriptDir()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		err := languages.InstallTypeScript(ctx, dest)
		a.post(func() {
			a.serverJob = false
			if err != nil {
				a.serverJobErr = err.Error()
			} else {
				a.retryTypeScriptLSP()
			}
			services.Invalidate()
		})
	}()
}

func (a *App) removeManagedTypeScript(services ui.Services) {
	if a.serverJob {
		return
	}
	a.serverJob, a.serverJobErr = true, ""
	old := a.tsLSP
	a.tsEpoch++
	a.tsLSP = nil
	a.tsState, a.tsErr = langserver.Off, nil
	gen := a.gen
	dest := languages.ManagedTypeScriptDir()
	go func() {
		if old != nil {
			old.StopWait(2 * time.Second)
		}
		var err error
		if dest == "" {
			err = errors.New("cannot locate the application data folder")
		} else {
			err = os.RemoveAll(dest)
		}
		a.post(func() {
			a.serverJob = false
			if a.gen == gen {
				a.resetTypeScriptServer()
			}
			if err != nil {
				a.serverJobErr = err.Error()
			}
			services.Invalidate()
		})
	}()
}
