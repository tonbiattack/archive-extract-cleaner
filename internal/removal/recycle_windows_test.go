//go:build windows

package removal

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

type recycleBinInfo struct {
	cbSize   uint32
	_        uint32
	size     int64
	numItems int64
}

var procSHQueryRecycleBinW = shell32.NewProc("SHQueryRecycleBinW")

func queryRecycleBinItems(volumeRoot string) (int64, error) {
	root, err := syscall.UTF16PtrFromString(volumeRoot)
	if err != nil {
		return 0, err
	}
	info := recycleBinInfo{cbSize: uint32(unsafe.Sizeof(recycleBinInfo{}))}
	hr, _, _ := procSHQueryRecycleBinW.Call(uintptr(unsafe.Pointer(root)), uintptr(unsafe.Pointer(&info)))
	if int32(hr) < 0 {
		return 0, hresultError("SHQueryRecycleBin", hr)
	}
	return info.numItems, nil
}

func TestCheckRecycleDriveRejectsUNC(t *testing.T) {
	if err := checkRecycleDrive(`\\server\share\folder`); err == nil {
		t.Fatal("UNC path should be rejected")
	}
}

func TestMoveToRecycleBinIntegration(t *testing.T) {
	if os.Getenv("ARCHIVE_CLEANER_RECYCLE_TEST") != "1" {
		t.Skip("set ARCHIVE_CLEANER_RECYCLE_TEST=1 to exercise the real Windows recycle bin")
	}
	parent, err := os.MkdirTemp("", "archive-cleaner-recycle-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(parent)
	folder := filepath.Join(parent, "photos-"+time.Now().Format("150405.000"))
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}

	volume := filepath.VolumeName(folder) + `\`
	before, err := queryRecycleBinItems(volume)
	if err != nil {
		t.Skipf("cannot query recycle bin: %v", err)
	}
	if err := MoveToRecycleBin(folder); err != nil {
		t.Fatalf("MoveToRecycleBin: %v", err)
	}
	if _, err := os.Lstat(folder); !os.IsNotExist(err) {
		t.Fatalf("folder still exists after recycle: %v", err)
	}
	after, err := queryRecycleBinItems(volume)
	if err != nil {
		t.Fatalf("cannot query recycle bin after move: %v", err)
	}
	if after <= before {
		t.Fatalf("recycle bin item count did not increase: %d -> %d", before, after)
	}
	t.Logf("moved to recycle bin (items %d -> %d)", before, after)
}
