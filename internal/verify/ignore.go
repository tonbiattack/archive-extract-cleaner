package verify

import "strings"

// 補助ファイル判定。
//
// OSやアーカイバが自動生成するファイルは、ZIPと解凍済みフォルダで
// 有無が食い違いやすい。これらだけが差分である場合、共通部分の内容が
// SHA-256 で一致していれば「実質同一」と扱える。
//
// ただし判定を緩めるのは「ZIPにしか無い補助ファイル」だけにする。
// フォルダ側にしか無いファイルは、ユーザーが意図して置いた可能性があり、
// 削除すると情報が失われるため対象外とする。

// ignorableExactNames は完全一致で補助ファイルとみなす名前（小文字）。
var ignorableExactNames = map[string]struct{}{
	".ds_store":       {},
	"thumbs.db":       {},
	"desktop.ini":     {},
	".localized":      {},
	".apdisk":         {},
	"ehthumbs.db":     {},
	".spotlight-v100": {},
	".trashes":        {},
	".fseventsd":      {},
	"icon\r":          {},
}

// shouldIgnoreZipOnly は「ZIPにのみ存在する」パスが補助ファイルかを返す。
// パスはスラッシュ区切りの表示用パス。
func shouldIgnoreZipOnly(path string) bool {
	segments := strings.Split(strings.ToLower(path), "/")
	if len(segments) == 0 {
		return false
	}
	name := segments[len(segments)-1]
	if name == "" {
		return false
	}
	if _, ok := ignorableExactNames[name]; ok {
		return true
	}
	// 拡張子ベースの判定。ビルドやエディタが生成する一時ファイル。
	switch {
	case strings.HasSuffix(name, ".tmp"):
		return true
	case strings.HasSuffix(name, ".crdownload"):
		return true
	case strings.HasSuffix(name, ".part"):
		return true
	}
	// AppleDouble 形式。__MACOSX/ 配下の "._name" はリソースフォーク。
	if strings.HasPrefix(name, "._") {
		return true
	}
	// macOS のメタデータディレクトリ配下は丸ごと補助扱い。
	for _, segment := range segments[:len(segments)-1] {
		switch segment {
		case "__macosx", ".spotlight-v100", ".trashes", ".fseventsd":
			return true
		}
	}
	return false
}

// shouldIgnoreZipOnlyDir は「ZIPにのみ存在するディレクトリ」が補助かを返す。
func shouldIgnoreZipOnlyDir(path string) bool {
	segments := strings.Split(strings.ToLower(path), "/")
	for _, segment := range segments {
		switch segment {
		case "__macosx", ".spotlight-v100", ".trashes", ".fseventsd":
			return true
		}
	}
	return false
}
