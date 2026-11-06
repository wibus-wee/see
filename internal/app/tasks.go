package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"see/internal/shellpath"
	"see/internal/tasks"
	"see/locales"
)

type taskRun struct {
	kind   tasks.Kind
	output *tasks.Output
	cancel context.CancelFunc
	status string
	err    error
}

func taskMessage(key string) string { return locales.Catalog.Format(i18n.Locale(), key, nil) }

func (a *App) showTasks() {
	a.term.open, a.term.view, a.term.focus = true, panelTasks, nil
}

func (a *App) runGoTask(kind tasks.Kind) {
	if a.root == "" || kind.Args() == nil {
		return
	}
	if !a.cfg.IsTrusted(a.root) {
		a.trustAsk = true
		a.afterTrust = func() { a.runGoTask(kind) }
		return
	}
	a.showTasks()
	a.ensureFiles()
	if a.task != nil && a.task.status == "taskRunning" {
		return
	}
	ctx, cancel := context.WithCancel(a.wsCtx)
	run := &taskRun{kind: kind, output: &tasks.Output{}, cancel: cancel, status: "taskRunning"}
	a.task = run
	gen, root, post := a.gen, a.root, a.post
	var pending atomic.Bool
	run.output.Changed = func() {
		if pending.CompareAndSwap(false, true) {
			post.Post(func() { pending.Store(false) })
		}
	}
	go func() {
		defer cancel()
		err := shellpath.Wait(ctx)
		if err == nil {
			var executable string
			executable, err = exec.LookPath("go")
			if err == nil {
				err = tasks.Run(ctx, executable, root, kind, run.output)
			}
		}
		post.Post(func() {
			if a.gen != gen || a.task != run {
				return
			}
			run.err = err
			switch {
			case errors.Is(err, context.Canceled):
				run.status = "taskCancelled"
			case err != nil:
				run.status = "taskFailed"
			default:
				run.status = "taskSucceeded"
			}
		})
	}()
}

func (a *App) stopGoTask() {
	if a.task != nil && a.task.status == "taskRunning" {
		a.task.cancel()
	}
}

var goOutputLocation = regexp.MustCompile(`^\s*(.+?):([0-9]+)(?::([0-9]+))?:\s`)

func taskLocation(root, line string) (string, editor.Pos, bool) {
	m := goOutputLocation.FindStringSubmatch(line)
	if m == nil {
		return "", editor.Pos{}, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil || n < 1 {
		return "", editor.Pos{}, false
	}
	col := 1
	if m[3] != "" {
		col, err = strconv.Atoi(m[3])
		if err != nil || col < 1 {
			return "", editor.Pos{}, false
		}
	}
	path := m[1]
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	return filepath.Clean(path), editor.Pos{Line: n - 1, Col: col - 1}, true
}

func (a *App) openTaskLocation(path string, pos editor.Pos) {
	if fi, err := os.Stat(path); err != nil || fi.IsDir() {
		rel, _ := filepath.Rel(a.root, path)
		suffix := "/" + filepath.ToSlash(rel)
		matched := ""
		for _, candidate := range a.quick.files {
			if strings.HasSuffix("/"+filepath.ToSlash(candidate), suffix) {
				if matched != "" {
					a.note(taskMessage("tasksAmbiguousSource"), true)
					return
				}
				matched = filepath.Join(a.root, filepath.FromSlash(candidate))
			}
		}
		if matched == "" {
			a.note(taskMessage("tasksMissingSource"), true)
			return
		}
		path = matched
	}
	if from, ok := a.here(); ok {
		a.hist.Push(from)
	}
	a.cancelNav()
	a.openFile(path, &pos)
}

func (a *App) taskView(c *ui.Context) {
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Padding(6, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, taskMessage("tasksTitle")).Bold().Grow(1)
			ui.Button(c, taskMessage("tasksBuild")).Label(commandLabel("Go: Build Workspace")).OnClick(func() { a.runGoTask(tasks.Build) })
			ui.Button(c, taskMessage("tasksTest")).Label(commandLabel("Go: Test Workspace")).OnClick(func() { a.runGoTask(tasks.Test) })
			if a.task != nil && a.task.status == "taskRunning" {
				ui.Button(c, taskMessage("tasksStop")).Label(commandLabel("Tasks: Stop")).OnClick(a.stopGoTask)
			}
		})
		run := a.task
		ui.Text(c, taskMessage("tasksDiskHint")).FontSize(11).TextColor(colors(c).Muted).Padding(0, 12)
		if run == nil {
			ui.Text(c, taskMessage("tasksEmpty")).TextColor(colors(c).Muted).Padding(12)
			return
		}
		ui.Text(c, "go "+strings.Join(run.kind.Args(), " ")+" — "+taskMessage(run.status)).FontSize(12).Padding(4, 12)
		if run.err != nil && run.status == "taskFailed" {
			ui.Text(c, run.err.Error()).TextColor(colors(c).Err).Padding(4, 12)
		}
		text, truncated := run.output.Snapshot()
		if truncated {
			ui.Text(c, taskMessage("tasksTruncated")).TextColor(colors(c).Muted).Padding(4, 12)
		}
		lines := strings.Split(text, "\n")
		ui.List(c, &a.taskList, len(lines), func(i int) {
			line := lines[i]
			path, pos, linked := taskLocation(a.root, line)
			if linked {
				row := ui.ButtonBase(c.Key(i)).Justify(ui.Start).FillWidth().Height(24).Padding(2, 12).Radius(4).Label(fmt.Sprintf("%s:%d:%d", path, pos.Line+1, pos.Col+1)).Tooltip(line)
				if row.Hovered() {
					row.Background(colors(c).Pill)
				}
				row.OnClick(func() { a.openTaskLocation(path, pos) })
				row.Children(func() { ui.Text(c, line).FontSize(12).SingleLine().TextColor(c.Theme().Accent) })
			} else {
				ui.Text(c.Key(i), line).FontSize(12).Padding(2, 12).Selectable()
			}
		}).Grow(1).FillWidth().Label(taskMessage("tasksTitle"))
	})
}
