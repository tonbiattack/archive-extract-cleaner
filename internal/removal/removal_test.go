package removal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/testutil"
)

func TestDeleteFolder(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "photos")
	if err := testutil.WriteFile(folder, "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(root, "photos.zip")
	if err := testutil.WriteZip(zipPath, []testutil.Entry{{Name: "photos/a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	result := Delete(context.Background(), folder, []string{root})
	if !result.Success {
		t.Fatalf("delete failed: %s", result.Reason)
	}
	if _, err := os.Lstat(folder); !os.IsNotExist(err) {
		t.Fatalf("folder still exists: %v", err)
	}
	if _, err := os.Stat(zipPath); err != nil {
		t.Fatalf("zip was removed: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".deleting-") {
			t.Fatalf("quarantine left behind: %s", entry.Name())
		}
	}
}

func TestDeleteRefusesRootAndOutside(t *testing.T) {
	root := t.TempDir()
	if result := Delete(context.Background(), root, []string{root}); result.Success {
		t.Fatal("root deletion should be refused")
	}
	outside := t.TempDir()
	if result := Delete(context.Background(), outside, []string{root}); result.Success {
		t.Fatal("outside deletion should be refused")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside folder was removed: %v", err)
	}
}

func TestDeleteRefusesJunction(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "photos")
	if err := testutil.MakeDir(folder, "sub"); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := testutil.Junction(filepath.Join(folder, "link"), target); err != nil {
		t.Skipf("cannot create junction: %v", err)
	}
	result := Delete(context.Background(), folder, []string{root})
	if result.Success {
		t.Fatal("junction deletion should be refused")
	}
	if _, err := os.Stat(folder); err != nil {
		t.Fatalf("folder was removed: %v", err)
	}
}
