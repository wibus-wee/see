//go:build !windows

package proc

import "os/exec"

func configure(*exec.Cmd) {}
