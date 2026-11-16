package archive

import (
	"os/exec"
)

// commandFor はテスト用に 7-Zip コマンドを組み立てる。
func commandFor(tool string, args ...string) *exec.Cmd {
	return exec.Command(tool, args...)
}
