//go:build !windows

package archive

import "os/exec"

func hideSevenZipConsole(cmd *exec.Cmd) {}
