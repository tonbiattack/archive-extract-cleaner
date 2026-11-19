// Package duplicates は、整理対象のアーカイブと E/F/G の保存先に同名の
// アーカイブが存在するかを横断的に調べる。
//
// 重要な前提:
//   - 「同名」は「同一内容」ではない。サイズ比較は補助情報にすぎず、
//     この結果を削除の可否判定に使ってはならない。
//   - ドライブ未接続・アクセス拒否は「なし」ではなく「未確認」として扱う。
//     存在しないと誤表示すると、ユーザーが重複コピーを作る判断を誤る。
//   - 索引は保存先フォルダ単位で1回だけ作り、対象ごとに全件走査しない。
package duplicates

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/tonbiattack/archive-extract-cleaner/internal/winpath"
)

// Status は同名アーカイブの検出結果。
type Status string

const (
	// StatusNone は同名アーカイブが見つからなかった状態。
	StatusNone Status = "none"
	// StatusSameSize は同名かつサイズが一致した状態。内容一致は意味しない。
	StatusSameSize Status = "same_size"
	// StatusDifferentSize は同名だがサイズが異なる状態。
	StatusDifferentSize Status = "different_size"
	// StatusUnknown は未接続・アクセス拒否などで確認できなかった状態。
	StatusUnknown Status = "unknown"
)

// Hit は1件の同名アーカイブ。
type Hit struct {
	// Drive はドライブレター（例: "E"）。
	Drive string `json:"drive"`
	// Path は見つかったアーカイブのフルパス。
	Path string `json:"path"`
	// Size は見つかったファイルのサイズ。
	Size int64 `json:"size"`
	// SameSize は対象とサイズが一致するか。
	SameSize bool `json:"sameSize"`
}

// DriveState はドライブごとの確認状況。
type DriveState struct {
	// Drive はドライブレター。
	Drive string `json:"drive"`
	// Root は解決した保存先フォルダ。
	Root string `json:"root"`
	// Available は保存先を読めたか。
	Available bool `json:"available"`
	// Reason は読めなかった理由。Available が true なら空。
	Reason string `json:"reason,omitempty"`
	// Indexed は索引化したエントリ数。
	Indexed int `json:"indexed"`
	// Truncated は上限に達して索引を打ち切ったか。
	Truncated bool `json:"truncated"`
}

// Result は1つの対象アーカイブに対する検索結果。
type Result struct {
	// Status は総合判定。同名が1件でもあれば same_size / different_size。
	// 同名が無く、かつ未確認のドライブがあれば unknown。
	Status Status `json:"status"`
	// Hits は見つかった同名アーカイブ。ドライブ順・パス順で安定させる。
	Hits []Hit `json:"hits"`
	// Drives はドライブごとの確認状況。
	Drives []DriveState `json:"drives"`
}

// Options は検索の設定。
type Options struct {
	// Drives は検索するドライブレター（例: []string{"E","F","G"}）。
	Drives []string
	// RelativePath は各ドライブ内の保存先フォルダ。空ならドライブ直下。
	RelativePath string
	// Roots はドライブレターごとの保存先を明示する。テストと、ドライブ以外の
	// パスを検索したい場合に使う。指定したドライブは Drives より優先する。
	Roots map[string]string
	// Recursive は配下サブフォルダまで検索するか。
	Recursive bool
	// MaxDepth は Recursive 時の深さ上限。
	MaxDepth int
	// MaxEntries は1ドライブあたりの索引上限。
	MaxEntries int
}

// Target は検索対象のアーカイブ。
type Target struct {
	// Path は対象アーカイブのフルパス。
	Path string
	// Size は対象アーカイブのサイズ。
	Size int64
}

// index は1ドライブ分の索引。小文字ファイル名から候補を引く。
type index struct {
	state DriveState
	// byName は小文字のファイル名から、その名前を持つエントリ一覧。
	byName map[string][]entry
}

type entry struct {
	path string
	size int64
}

// Searcher は索引を保持して複数の対象を照合する。
// 同じ保存先を何度も走査しないよう、1回のスキャンで使い回す。
type Searcher struct {
	opts    Options
	mu      sync.Mutex
	indexes map[string]*index
	order   []string
}

// NewSearcher は検索器を作る。索引は最初の Search で遅延構築する。
func NewSearcher(opts Options) *Searcher {
	return &Searcher{opts: opts, indexes: map[string]*index{}}
}

// DriveStates は構築済みのドライブ状況を返す。索引が未構築なら空。
func (s *Searcher) DriveStates() []DriveState {
	s.mu.Lock()
	defer s.mu.Unlock()
	states := make([]DriveState, 0, len(s.order))
	for _, drive := range s.order {
		states = append(states, s.indexes[drive].state)
	}
	return states
}

// Search は1つの対象について同名アーカイブを探す。
func (s *Searcher) Search(ctx context.Context, target Target) Result {
	if err := ctx.Err(); err != nil {
		return Result{Status: StatusUnknown}
	}
	name := strings.ToLower(filepath.Base(target.Path))
	states := make([]DriveState, 0, len(s.opts.Drives))
	hits := make([]Hit, 0, 4)
	unknown := false
	for _, drive := range s.opts.Drives {
		idx := s.indexFor(ctx, drive)
		states = append(states, idx.state)
		if !idx.state.Available {
			unknown = true
			continue
		}
		for _, found := range idx.byName[name] {
			// 対象自身は除外する。同じドライブ上で整理している場合に
			// 自分自身を「重複」と表示しないため。
			if winpath.EqualPath(found.path, target.Path) {
				continue
			}
			hits = append(hits, Hit{
				Drive:    drive,
				Path:     found.path,
				Size:     found.size,
				SameSize: found.size == target.Size,
			})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Drive != hits[j].Drive {
			return hits[i].Drive < hits[j].Drive
		}
		return strings.ToLower(hits[i].Path) < strings.ToLower(hits[j].Path)
	})
	return Result{Status: summarize(hits, unknown), Hits: hits, Drives: states}
}

// summarize は総合判定を決める。
//
// 同名が1件でもあれば、未確認のドライブがあっても same_size /
// different_size を優先する（見つかった事実の方が有用なため）。
// 同名が無く未確認があれば unknown。どちらでもなければ none。
func summarize(hits []Hit, unknown bool) Status {
	if len(hits) > 0 {
		for _, hit := range hits {
			if hit.SameSize {
				return StatusSameSize
			}
		}
		return StatusDifferentSize
	}
	if unknown {
		return StatusUnknown
	}
	return StatusNone
}

// indexFor はドライブの索引を返す。未構築なら構築する。
func (s *Searcher) indexFor(ctx context.Context, drive string) *index {
	s.mu.Lock()
	if idx, ok := s.indexes[drive]; ok {
		s.mu.Unlock()
		return idx
	}
	s.mu.Unlock()

	idx := buildIndex(ctx, drive, s.opts)

	s.mu.Lock()
	s.indexes[drive] = idx
	s.order = append(s.order, drive)
	s.mu.Unlock()
	return idx
}

// buildIndex は1ドライブ分の索引を作る。
func buildIndex(ctx context.Context, drive string, opts Options) *index {
	root := drive + `:\`
	if explicit, ok := opts.Roots[drive]; ok && explicit != "" {
		root = explicit
	} else if opts.RelativePath != "" {
		root = filepath.Join(root, opts.RelativePath)
	}
	idx := &index{byName: map[string][]entry{}}
	idx.state = DriveState{Drive: drive, Root: root}

	info, err := os.Stat(root)
	if err != nil {
		idx.state.Reason = describeAccessError(err)
		return idx
	}
	if !info.IsDir() {
		idx.state.Reason = "保存先がフォルダではありません"
		return idx
	}
	// リパースポイントを辿ると別ボリュームへ抜ける可能性があるため拒否する。
	if reparse, err := winpath.IsReparsePoint(root); err != nil {
		idx.state.Reason = describeAccessError(err)
		return idx
	} else if reparse {
		idx.state.Reason = "保存先がリンク/ジャンクションです"
		return idx
	}

	maxDepth := opts.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 8
	}
	maxEntries := opts.MaxEntries
	if maxEntries <= 0 {
		maxEntries = 200000
	}

	count := 0
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			// 個別のフォルダが読めなくても、読めた範囲で索引を作る。
			// ただし「読めなかった」事実は残し、未確認と誤表示しない。
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if path == root {
				return nil
			}
			if !opts.Recursive {
				return fs.SkipDir
			}
			depth := strings.Count(strings.TrimPrefix(path, root), string(filepath.Separator))
			if depth >= maxDepth {
				return fs.SkipDir
			}
			return nil
		}
		if count >= maxEntries {
			idx.state.Truncated = true
			return fs.SkipAll
		}
		count++
		if !isArchiveName(d.Name()) {
			return nil
		}
		fileInfo, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		if !fileInfo.Mode().IsRegular() {
			return nil
		}
		key := strings.ToLower(d.Name())
		idx.byName[key] = append(idx.byName[key], entry{path: path, size: fileInfo.Size()})
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, fs.SkipAll) {
		if ctx.Err() != nil {
			idx.state.Reason = "検索を中断しました"
			return idx
		}
		// 途中まで読めた索引は使えるが、全体を確認できたとは言えない。
		idx.state.Reason = describeAccessError(walkErr)
		return idx
	}
	idx.state.Available = true
	idx.state.Indexed = count
	return idx
}

// isArchiveName は対象にするアーカイブ拡張子かを返す。
// 同名検知は ZIP だけでなく、整理対象になり得る 7z / RAR も含める。
func isArchiveName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".zip", ".7z", ".rar":
		return true
	}
	return false
}

// describeAccessError はドライブ・フォルダが読めない理由を日本語にする。
func describeAccessError(err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "ドライブまたは保存先フォルダが見つかりません（未接続の可能性）"
	case errors.Is(err, fs.ErrPermission):
		return "アクセスが拒否されました"
	}
	msg := err.Error()
	if strings.Contains(strings.ToLower(msg), "not ready") ||
		strings.Contains(strings.ToLower(msg), "device is not ready") {
		return "ドライブの準備ができていません（未接続の可能性）"
	}
	return msg
}
