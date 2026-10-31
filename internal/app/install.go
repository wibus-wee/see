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

// installManagedTypeScript installs tsrs into See's own data folder, as
// any other tool: one job at a time, its error on the row that asked.
func (a *App) installManagedTypeScript(services ui.Services) {
	a.runInstallJob("typescript", services, func(ctx context.Context) error {
		return languages.InstallTypeScript(ctx, languages.ManagedTypeScriptDir())
	}, func() {
		if a.serverJobErr == "" {
			a.retryTypeScriptLSP()
		}
	})
}

// removeManagedTypeScript deletes See's installation, stopping the server
// that runs from it first.
func (a *App) removeManagedTypeScript(services ui.Services) {
	if a.serverJob {
		return
	}
	a.serverJob, a.serverJobName, a.serverJobErr = true, "typescript", ""
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
		a.post.Post(func() {
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
