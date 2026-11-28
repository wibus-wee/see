//go:build !unix

package lsp

import "os/exec"

// Outside Unix only the direct child is owned. Wait still closes our pipe ends.
func configureServer(*exec.Cmd) {}
func killServer(cmd *exec.Cmd)  { _ = cmd.Process.Kill() }
