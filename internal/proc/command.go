// Package proc starts child commands without opening a console on Windows.
package proc

import (
	"context"
	"os/exec"
)

func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	configure(cmd)
	return cmd
}

func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	configure(cmd)
	return cmd
}
