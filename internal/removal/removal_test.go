package removal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/testutil"
)

func TestDeleteFolderToRecycleBin(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "photos")
	if err := testutil.WriteFile(folder, "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(root, "photos.zip")
	if err := testutil.WriteZip(zipPath, []testutil.Entry{{Name: "photos/a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	called := false
	result := Delete(context.Background(), folder, []string{root}, func(path string) error {
		called = true
		return os.RemoveAll(path)
	})
	if !result.Success {
		t.Fatalf("recycle failed: %s", result.Reason)
	}
	if !called {
		t.Fatal("mover was not called")
	}
	if _, err := os.Lstat(folder); !os.IsNotExist(err) {
		t.Fatalf("folder still exists: %v", err)
	}
	if _, err := os.Stat(zipPath); err != nil {
		t.Fatalf("zip was removed: %v", err)
	}
}

func TestDeleteRefusesRootAndOutside(t *testing.T) {
	root := t.TempDir()
	called := false
	mover := func(string) error {
		called = true
		return nil
	}
	if result := Delete(context.Background(), root, []string{root}, mover); result.Success {
		t.Fatal("root recycle should be refused")
	}
	outside := t.TempDir()
	if result := Delete(context.Background(), outside, []string{root}, mover); result.Success {
		t.Fatal("outside recycle should be refused")
	}
	if called {
		t.Fatal("mover must not be called for refused targets")
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
	called := false
	result := Delete(context.Background(), folder, []string{root}, func(string) error {
		called = true
		return nil
	})
	if result.Success {
		t.Fatal("junction recycle should be refused")
	}
	if called {
		t.Fatal("mover must not be called for a folder containing a junction")
	}
	if _, err := os.Stat(folder); err != nil {
		t.Fatalf("folder was removed: %v", err)
	}
}

func TestDeleteDoesNotFallBackWhenMoverFails(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "photos")
	if err := testutil.WriteFile(folder, "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	result := Delete(context.Background(), folder, []string{root}, func(string) error {
		return errors.New("recycle bin unavailable")
	})
	if result.Success {
		t.Fatal("recycle failure must not be reported as success")
	}
	if !strings.Contains(result.Reason, "ごみ箱") {
		t.Fatalf("reason = %s", result.Reason)
	}
	if _, err := os.Stat(filepath.Join(folder, "a.txt")); err != nil {
		t.Fatalf("folder was permanently deleted despite mover failure: %v", err)
	}
}

func TestDeleteFailsWhenTargetStillExists(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "photos")
	if err := testutil.WriteFile(folder, "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}
	result := Delete(context.Background(), folder, []string{root}, func(string) error {
		return nil
	})
	if result.Success {
		t.Fatal("mover that leaves the folder must not be reported as success")
	}
	if _, err := os.Stat(folder); err != nil {
		t.Fatalf("folder was removed: %v", err)
	}
}
