package tasks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

func configureProcess(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		killer := filepath.Join(os.Getenv("SystemRoot"), "System32", "taskkill.exe")
		if err := exec.Command(killer, "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run(); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
