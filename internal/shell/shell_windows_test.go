//go:build windows

package shell

import (
	"path/filepath"
	"testing"
)

func TestRevealFileMissing(t *testing.T) {
	if err := RevealFile(filepath.Join(t.TempDir(), "missing.zip")); err == nil {
		t.Fatal("missing file must return an error")
	}
}
