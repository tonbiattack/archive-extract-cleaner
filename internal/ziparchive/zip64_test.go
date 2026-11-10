package ziparchive

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
)

func writeZip64(t *testing.T, path, name string, content []byte) {
	t.Helper()
	crc := crc32.ChecksumIEEE(content)
	size := uint64(len(content))
	centralOffset := uint64(30 + len(name) + 20 + len(content))
	centralSize := uint64(46 + len(name) + 28)
	zip64EOCDOffset := centralOffset + centralSize

	var buffer bytes.Buffer
	le := binary.LittleEndian
	write := func(value any) {
		if err := binary.Write(&buffer, le, value); err != nil {
			t.Fatal(err)
		}
	}

	write(uint32(0x04034b50))
	write(uint16(45))
	write(uint16(0))
	write(uint16(zip.Store))
	write(uint16(0))
	write(uint16(0))
	write(crc)
	write(uint32(0xFFFFFFFF))
	write(uint32(0xFFFFFFFF))
	write(uint16(len(name)))
	write(uint16(20))
	buffer.WriteString(name)
	write(uint16(0x0001))
	write(uint16(16))
	write(size)
	write(size)
	buffer.Write(content)

	write(uint32(0x02014b50))
	write(uint16(45))
	write(uint16(45))
	write(uint16(0))
	write(uint16(zip.Store))
	write(uint16(0))
	write(uint16(0))
	write(crc)
	write(uint32(0xFFFFFFFF))
	write(uint32(0xFFFFFFFF))
	write(uint16(len(name)))
	write(uint16(28))
	write(uint16(0))
	write(uint16(0))
	write(uint16(0))
	write(uint32(0))
	write(uint32(0xFFFFFFFF))
	buffer.WriteString(name)
	write(uint16(0x0001))
	write(uint16(24))
	write(size)
	write(size)
	write(uint64(0))

	write(uint32(0x06064b50))
	write(uint64(44))
	write(uint16(45))
	write(uint16(45))
	write(uint32(0))
	write(uint32(0))
	write(uint64(1))
	write(uint64(1))
	write(centralSize)
	write(centralOffset)

	write(uint32(0x07064b50))
	write(uint32(0))
	write(uint64(zip64EOCDOffset))
	write(uint32(1))

	write(uint32(0x06054b50))
	write(uint16(0))
	write(uint16(0))
	write(uint16(0xFFFF))
	write(uint16(0xFFFF))
	write(uint32(0xFFFFFFFF))
	write(uint32(0xFFFFFFFF))
	write(uint16(0))

	if err := os.WriteFile(path, buffer.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOpenZip64(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zip64.zip")
	content := []byte("hello zip64")
	writeZip64(t, path, "a.txt", content)
	archive, err := Open(path, model.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	file, ok := archive.Variants[0].Files["a.txt"]
	if !ok {
		t.Fatalf("files = %+v", archive.Variants[0].Files)
	}
	reader, err := file.File.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	buffer := new(bytes.Buffer)
	if _, err := buffer.ReadFrom(reader); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buffer.Bytes(), content) {
		t.Fatalf("content = %q", buffer.Bytes())
	}
}
