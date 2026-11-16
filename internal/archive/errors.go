package archive

import (
	"errors"

	"github.com/tonbiattack/archive-extract-cleaner/internal/ziparchive"
)

// isZIPEncrypted は ziparchive の暗号化エラーかを判定する。
func isZIPEncrypted(err error) bool {
	return errors.Is(err, ziparchive.ErrEncrypted)
}

// isZIPLimit は ziparchive の上限エラーかを判定する。
func isZIPLimit(err error) bool {
	return errors.Is(err, ziparchive.ErrLimit)
}
