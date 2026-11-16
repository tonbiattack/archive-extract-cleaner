package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"
)

// structureHashOf はエントリ属性を正規化してSHA-256にする。
// 圧縮データ本体は読まない。同サイズ・同更新時刻のまま中身を差し替えた
// アーカイブを検出するために使う。
func structureHashOf(entries []Entry) string {
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		kind := "f"
		if entry.Dir {
			kind = "d"
		}
		lines = append(lines, fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%d",
			kind,
			strings.ToLower(entry.Path),
			entry.Size,
			entry.CRC32,
			entry.ModTime.UnixNano(),
		))
	}
	sort.Strings(lines)
	hash := sha256.New()
	for _, line := range lines {
		io.WriteString(hash, line)
		hash.Write([]byte{'\n'})
	}
	return hex.EncodeToString(hash.Sum(nil))
}
