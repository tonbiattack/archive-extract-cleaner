package engine

import (
	"errors"
	"fmt"

	"github.com/tonbiattack/archive-extract-cleaner/internal/archive"
)

// describeArchiveError はアーカイブ読み取りエラーをユーザー向けの文言にする。
// どの形式でも「なぜ削除候補にできないか」が分かるようにする。
func describeArchiveError(err error) string {
	switch {
	case errors.Is(err, archive.ErrEncrypted):
		return "アーカイブが暗号化されているため検証できません"
	case errors.Is(err, archive.ErrToolMissing):
		return "7z / RAR の検証には 7-Zip が必要です。インストールするか、設定ファイルの archiveTool で 7z.exe を指定してください"
	case errors.Is(err, archive.ErrUnsupported):
		return "未対応のアーカイブ形式または圧縮方式のため検証できません"
	case errors.Is(err, archive.ErrLimit):
		return "アーカイブが設定された上限を超えているため検証できません"
	case errors.Is(err, archive.ErrInvalid):
		return "アーカイブの構造が不正なため検証できません"
	default:
		return fmt.Sprintf("アーカイブを検証できません: %v", err)
	}
}
