package archive

import (
	"os/exec"
	"runtime"
)

// lookPath は 7z 実行ファイルを PATH から探す。
func lookPath(name string) (string, error) {
	return exec.LookPath(name)
}

// runtimeIsWindows は OS 判定を包む（テストで差し替えやすいように分離）。
func runtimeIsWindows() bool {
	return runtime.GOOS == "windows"
}
