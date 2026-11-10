//go:build windows

package removal

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

const (
	fofSilent           = 0x0004
	fofNoConfirmation   = 0x0010
	fofAllowUndo        = 0x0040
	fofNoErrorUI        = 0x0400
	fofxRecycleOnDelete = 0x00080000
	fofxEarlyFailure    = 0x00100000
	coinitApartment     = 0x2
	clsctxAll           = 0x17
	rpcEChangedMode     = 0x80010106
	driveRemovable      = 2
	driveFixed          = 3
	driveRemote         = 4
)

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	clsidFileOperation = guid{0x3AD05575, 0x8857, 0x4850, [8]byte{0x92, 0x77, 0x11, 0xB8, 0x5B, 0xDB, 0x8E, 0x09}}
	iidFileOperation   = guid{0x947AAB5F, 0x0A5C, 0x4C13, [8]byte{0xB4, 0xD6, 0x4B, 0xF7, 0x83, 0x6F, 0xC9, 0xF8}}
	iidShellItem       = guid{0x43826D1E, 0xE718, 0x42EE, [8]byte{0xBC, 0x55, 0xA1, 0xE2, 0x61, 0xC3, 0x7B, 0xFE}}

	ole32    = syscall.NewLazyDLL("ole32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procCoInitializeEx              = ole32.NewProc("CoInitializeEx")
	procCoUninitialize              = ole32.NewProc("CoUninitialize")
	procCoCreateInstance            = ole32.NewProc("CoCreateInstance")
	procSHCreateItemFromParsingName = shell32.NewProc("SHCreateItemFromParsingName")
	procGetDriveTypeW               = kernel32.NewProc("GetDriveTypeW")
)

func MoveToRecycleBin(folder string) error {
	abs, err := filepath.Abs(folder)
	if err != nil {
		return err
	}
	if err := checkRecycleDrive(abs); err != nil {
		return err
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr, _, _ := procCoInitializeEx.Call(0, coinitApartment)
	if uint32(hr) == rpcEChangedMode {
		return errors.New("COMをシングルスレッドアパートメントで初期化できません")
	}
	if int32(hr) < 0 {
		return hresultError("CoInitializeEx", hr)
	}
	defer procCoUninitialize.Call()

	var operation unsafe.Pointer
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidFileOperation)),
		0,
		clsctxAll,
		uintptr(unsafe.Pointer(&iidFileOperation)),
		uintptr(unsafe.Pointer(&operation)),
	)
	if int32(hr) < 0 || operation == nil {
		return hresultError("IFileOperationの作成", hr)
	}
	defer releaseCOM(operation)

	flags := uintptr(fofSilent | fofNoConfirmation | fofAllowUndo | fofNoErrorUI | fofxRecycleOnDelete | fofxEarlyFailure)
	if result := vcall(operation, 5, flags); result < 0 {
		return hresultError("SetOperationFlags", uintptr(result))
	}

	pathPtr, err := syscall.UTF16PtrFromString(abs)
	if err != nil {
		return err
	}
	var item unsafe.Pointer
	hr, _, _ = procSHCreateItemFromParsingName.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		uintptr(unsafe.Pointer(&iidShellItem)),
		uintptr(unsafe.Pointer(&item)),
	)
	if int32(hr) < 0 || item == nil {
		return hresultError("SHCreateItemFromParsingName", hr)
	}
	defer releaseCOM(item)

	if result := vcall(operation, 18, uintptr(item), 0); result < 0 {
		return hresultError("DeleteItem", uintptr(result))
	}
	if result := vcall(operation, 21); result < 0 {
		return hresultError("PerformOperations", uintptr(result))
	}
	var aborted int32
	if result := vcall(operation, 22, uintptr(unsafe.Pointer(&aborted))); result < 0 {
		return hresultError("GetAnyOperationsAborted", uintptr(result))
	}
	if aborted != 0 {
		return errors.New("ごみ箱への移動が中止されました")
	}
	return nil
}

func checkRecycleDrive(path string) error {
	if strings.HasPrefix(path, `\\`) {
		return errors.New("ネットワークパス(UNC)はWindowsごみ箱に移動できません")
	}
	volume := filepath.VolumeName(path)
	if volume == "" {
		return errors.New("ドライブを特定できません")
	}
	root, err := syscall.UTF16PtrFromString(volume + `\`)
	if err != nil {
		return err
	}
	driveType, _, _ := procGetDriveTypeW.Call(uintptr(unsafe.Pointer(root)))
	switch uint32(driveType) {
	case driveFixed, driveRemovable:
		return nil
	case driveRemote:
		return errors.New("ネットワークドライブはWindowsごみ箱に移動できません")
	default:
		return fmt.Errorf("このドライブはWindowsごみ箱に対応していません (drive type %d)", driveType)
	}
}

func hresultError(operation string, hr uintptr) error {
	return fmt.Errorf("%sに失敗しました (HRESULT 0x%08X)", operation, uint32(hr))
}

func vcall(object unsafe.Pointer, index uintptr, args ...uintptr) int32 {
	vtable := *(*unsafe.Pointer)(object)
	method := *(*uintptr)(unsafe.Add(vtable, index*unsafe.Sizeof(uintptr(0))))
	callArgs := make([]uintptr, 0, len(args)+1)
	callArgs = append(callArgs, uintptr(object))
	callArgs = append(callArgs, args...)
	result, _, _ := syscall.SyscallN(method, callArgs...)
	return int32(result)
}

func releaseCOM(object unsafe.Pointer) {
	if object != nil {
		vcall(object, 2)
	}
}
