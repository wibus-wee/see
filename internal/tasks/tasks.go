// Package tasks runs fixed Go development tasks and retains bounded output.
package tasks

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const MaxOutput = 256 << 10

type Kind string

const (
	Build Kind = "build"
	Test  Kind = "test"
)

func (k Kind) Args() []string {
	switch k {
	case Build:
		return []string{"build", "./..."}
	case Test:
		return []string{"test", "./..."}
	}
	return nil
}

// Output retains the newest bytes and can be read while a task writes.
type Output struct {
	mu        sync.Mutex
	text      string
	truncated bool
	Changed   func()
}

func (o *Output) Write(p []byte) (int, error) {
	o.mu.Lock()
	if len(p) >= MaxOutput {
		o.text = string(p[len(p)-MaxOutput:])
		o.truncated = true
	} else {
		o.text += string(p)
		if len(o.text) > MaxOutput {
			o.text = o.text[len(o.text)-MaxOutput:]
			o.truncated = true
		}
	}
	o.mu.Unlock()
	if o.Changed != nil {
		o.Changed()
	}
	return len(p), nil
}

func (o *Output) Snapshot() (text string, truncated bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return strings.ToValidUTF8(o.text, "�"), o.truncated
}

// Run invokes Go directly. Cancellation terminates the task's process tree.
func Run(ctx context.Context, executable, root string, kind Kind, output *Output) error {
	args := kind.Args()
	if args == nil {
		return fmt.Errorf("unknown Go task %q", kind)
	}
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = root
	cmd.Stdout, cmd.Stderr = output, output
	cmd.WaitDelay = 2 * time.Second
	configureProcess(cmd)
	err := cmd.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
