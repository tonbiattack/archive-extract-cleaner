package archive

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// The scanner runs 7-Zip without an interactive console, including from GUI builds.
func hideSevenZipConsole(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
}
