package proc

import (
	"os"
	"os/exec"
	"testing"
)

func TestCommandsHideConsole(t *testing.T) {
	for name, cmd := range map[string]*exec.Cmd{
		"Command":        Command(os.Args[0]),
		"CommandContext": CommandContext(t.Context(), os.Args[0]),
	} {
		t.Run(name, func(t *testing.T) {
			const createNoWindow = 0x08000000 // Win32 CREATE_NO_WINDOW
			if attr := cmd.SysProcAttr; attr == nil || !attr.HideWindow || attr.CreationFlags&createNoWindow == 0 {
				t.Fatalf("want HideWindow and CREATE_NO_WINDOW, got %+v", attr)
			}
		})
	}
}
