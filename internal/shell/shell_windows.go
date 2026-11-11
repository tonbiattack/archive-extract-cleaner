//go:build windows

package shell

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	shell32                        = syscall.NewLazyDLL("shell32.dll")
	ole32                          = syscall.NewLazyDLL("ole32.dll")
	procSHParseDisplayName         = shell32.NewProc("SHParseDisplayName")
	procSHOpenFolderAndSelectItems = shell32.NewProc("SHOpenFolderAndSelectItems")
	procShellExecuteW              = shell32.NewProc("ShellExecuteW")
	procCoInitializeEx             = ole32.NewProc("CoInitializeEx")
	procCoUninitialize             = ole32.NewProc("CoUninitialize")
	procCoTaskMemFree              = ole32.NewProc("CoTaskMemFree")
)

const (
	coinitApartmentThreaded = 0x2
	rpcEChangedMode         = 0x80010106
)

func OpenFolder(path string) error {
	target, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	verb, _ := syscall.UTF16PtrFromString("open")
	result, _, _ := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(target)),
		0,
		0,
		1,
	)
	if result <= 32 {
		return fmt.Errorf("エクスプローラーでフォルダを開けませんでした (code %d)", result)
	}
	return nil
}

func RevealFile(path string) error {
	target, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	result, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded)
	hr := uint32(result)
	initialized := hr == 0 || hr == 1
	if int32(hr) < 0 && hr != rpcEChangedMode {
		return fmt.Errorf("COMを初期化できません (HRESULT 0x%08X)", hr)
	}
	if initialized {
		defer procCoUninitialize.Call()
	}
	var pidl unsafe.Pointer
	parseResult, _, _ := procSHParseDisplayName.Call(
		uintptr(unsafe.Pointer(target)),
		0,
		uintptr(unsafe.Pointer(&pidl)),
		0,
		0,
	)
	hr = uint32(parseResult)
	if int32(hr) < 0 || pidl == nil {
		return fmt.Errorf("ファイルの場所を特定できません (HRESULT 0x%08X)", hr)
	}
	defer procCoTaskMemFree.Call(uintptr(pidl))
	openResult, _, _ := procSHOpenFolderAndSelectItems.Call(uintptr(pidl), 0, 0, 0)
	hr = uint32(openResult)
	if int32(hr) < 0 {
		return fmt.Errorf("エクスプローラーで選択表示できません (HRESULT 0x%08X)", hr)
	}
	return nil
}
