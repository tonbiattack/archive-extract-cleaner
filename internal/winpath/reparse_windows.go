//go:build windows

package winpath

import "syscall"

const fileAttributeReparsePoint = 0x400

func IsReparsePoint(path string) (bool, error) {
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false, err
	}
	attrs, err := syscall.GetFileAttributes(ptr)
	if err != nil {
		return false, err
	}
	return attrs&fileAttributeReparsePoint != 0, nil
}
