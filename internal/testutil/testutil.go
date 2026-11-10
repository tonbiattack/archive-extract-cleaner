package testutil

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Entry struct {
	Name    string
	Content string
	Mode    os.FileMode
	Store   bool
}

func WriteZip(path string, entries []Entry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	writer := zip.NewWriter(file)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.Name, Method: zip.Deflate, Modified: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)}
		if entry.Mode != 0 {
			header.SetMode(entry.Mode)
		}
		if entry.Store || strings.HasSuffix(entry.Name, "/") {
			header.Method = zip.Store
		}
		part, err := writer.CreateHeader(header)
		if err != nil {
			writer.Close()
			file.Close()
			return err
		}
		if _, err := io.WriteString(part, entry.Content); err != nil {
			writer.Close()
			file.Close()
			return err
		}
	}
	if err := writer.Close(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func WriteFile(root, rel, content string) error {
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(content), 0o644)
}

func MakeDir(root, rel string) error {
	return os.MkdirAll(filepath.Join(root, filepath.FromSlash(rel)), 0o755)
}

func Snapshot(root string) (map[string]string, error) {
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		result[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	return result, err
}

func EqualSnapshot(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func PatchByte(path string, patterns [][]byte, patch func([]byte) bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	changed := false
	for _, pattern := range patterns {
		offset := 0
		for {
			index := bytes.Index(data[offset:], pattern)
			if index < 0 {
				break
			}
			index += offset
			patch(data[index:])
			changed = true
			offset = index + len(pattern)
		}
	}
	if !changed {
		return os.ErrNotExist
	}
	return os.WriteFile(path, data, 0o644)
}

func SetEncrypted(path string) error {
	local := []byte{'P', 'K', 0x03, 0x04}
	central := []byte{'P', 'K', 0x01, 0x02}
	return PatchByte(path, [][]byte{local, central}, func(data []byte) bool {
		if len(data) < 10 {
			return false
		}
		flagOffset := 6
		if data[0] == 'P' && data[1] == 'K' && data[2] == 0x01 {
			flagOffset = 8
		}
		data[flagOffset] |= 0x01
		return true
	})
}

func SetMethod(path string, method uint16) error {
	local := []byte{'P', 'K', 0x03, 0x04}
	central := []byte{'P', 'K', 0x01, 0x02}
	return PatchByte(path, [][]byte{local, central}, func(data []byte) bool {
		methodOffset := 8
		if data[0] == 'P' && data[1] == 'K' && data[2] == 0x01 {
			methodOffset = 10
		}
		if len(data) < methodOffset+2 {
			return false
		}
		data[methodOffset] = byte(method)
		data[methodOffset+1] = byte(method >> 8)
		return true
	})
}

func CorruptFirstFileData(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	central := []byte{'P', 'K', 0x01, 0x02}
	index := bytes.Index(data, central)
	if index < 0 || len(data) < index+46 {
		return os.ErrNotExist
	}
	compressed := binary.LittleEndian.Uint32(data[index+20 : index+24])
	localOffset := binary.LittleEndian.Uint32(data[index+42 : index+46])
	offset := int(localOffset)
	if len(data) < offset+30 {
		return os.ErrNotExist
	}
	nameLen := binary.LittleEndian.Uint16(data[offset+26 : offset+28])
	extraLen := binary.LittleEndian.Uint16(data[offset+28 : offset+30])
	dataOffset := offset + 30 + int(nameLen) + int(extraLen)
	if compressed == 0 || dataOffset+int(compressed) > len(data) {
		return os.ErrInvalid
	}
	data[dataOffset+int(compressed)/2] ^= 0xff
	return os.WriteFile(path, data, 0o644)
}

func Junction(link, target string) error {
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		return os.ErrInvalid
	}
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/c", "mklink", "/J", link, target).Run()
	}
	return os.Symlink(target, link)
}
