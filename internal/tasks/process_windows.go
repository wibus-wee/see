package tasks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"see/internal/proc"
)

func configureProcess(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		killer := filepath.Join(os.Getenv("SystemRoot"), "System32", "taskkill.exe")
		if err := proc.Command(killer, "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run(); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
