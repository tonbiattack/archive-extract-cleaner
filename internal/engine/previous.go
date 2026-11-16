package engine

import (
	"context"
	"os"
	"sync"

	"github.com/tonbiattack/archive-extract-cleaner/internal/cache"
	"github.com/tonbiattack/archive-extract-cleaner/internal/fswalk"
	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/winpath"
)

// folderWalker は差分スキャン用にフォルダ走査結果を共有する。
// 1回のスキャン内で同じフォルダを二度走査しない。
type folderWalker struct {
	mu    sync.Mutex
	trees map[string]*fswalk.Tree
	errs  map[string]error
}

func newFolderWalker() *folderWalker {
	return &folderWalker{
		trees: map[string]*fswalk.Tree{},
		errs:  map[string]error{},
	}
}

// walk はフォルダを走査してツリーを返す。結果はスキャン内でキャッシュする。
func (f *folderWalker) walk(ctx context.Context, path string, limits model.Limits) (*fswalk.Tree, error) {
	key := winpath.Key(path)
	f.mu.Lock()
	if tree, ok := f.trees[key]; ok {
		err := f.errs[key]
		f.mu.Unlock()
		return tree, err
	}
	f.mu.Unlock()

	tree, err := fswalk.Walk(ctx, path, limits)

	f.mu.Lock()
	f.trees[key] = tree
	f.errs[key] = err
	f.mu.Unlock()
	return tree, err
}

// previousIndex は前回結果を ID とパスで引けるようにしたもの。
type previousIndex struct {
	items  map[string]cache.ItemRecord
	folder map[string]cache.FolderFingerprint
	zip    map[string]cache.ArchiveKey
}

// buildPreviousIndex は前回のルート記録から索引を作る。
func buildPreviousIndex(record *cache.RootRecord) *previousIndex {
	if record == nil {
		return nil
	}
	index := &previousIndex{
		items:  make(map[string]cache.ItemRecord, len(record.Items)),
		folder: record.FolderIndex(),
		zip:    record.ZipIndex(),
	}
	for _, item := range record.Items {
		index.items[item.ID] = item
	}
	return index
}

// canReuse はタスクを前回結果で置き換えられるかを判定する。
//
// 再利用の条件（すべて満たす場合だけ）:
//   - 前回の項目が再利用可能（UNVERIFIED・エラー・移動済みでない）
//   - ZIP のパス・サイズ・更新時刻が一致する
//   - フォルダの指紋（パス集合・サイズ・更新時刻）が一致する
//   - 対応付けが変わっていない（項目IDが同じ）
//
// 構造指紋とハッシュの一致は ZIP ハッシュキャッシュ側で CRC とサイズを
// 突き合わせて確認する。1つでも欠ければ再検証する。
func (p *previousIndex) canReuse(task verifyTask, limits model.Limits, walker *folderWalker, ctx context.Context) (cache.ItemRecord, *fswalk.Tree, bool) {
	if p == nil {
		return cache.ItemRecord{}, nil, false
	}
	target := task.folderPath
	if task.archivePath != "" {
		if len(task.candidates) != 1 {
			return cache.ItemRecord{}, nil, false
		}
		target = task.candidates[0].path
	}
	if target == "" {
		return cache.ItemRecord{}, nil, false
	}
	record, ok := p.items[taskID(task)]
	if !ok || !record.Cacheable {
		return cache.ItemRecord{}, nil, false
	}
	previousPrint, ok := p.folder[winpath.Key(target)]
	if !ok {
		return cache.ItemRecord{}, nil, false
	}
	if task.archivePath != "" {
		previousKey, ok := p.zip[winpath.Key(task.archivePath)]
		if !ok {
			return cache.ItemRecord{}, nil, false
		}
		info, err := os.Stat(task.archivePath)
		if err != nil {
			return cache.ItemRecord{}, nil, false
		}
		if previousKey.Size != info.Size() || previousKey.ModTimeUnixNS != info.ModTime().UnixNano() {
			return cache.ItemRecord{}, nil, false
		}
		// サイズと更新時刻は偽装できる。セントラルディレクトリの構造指紋まで
		// 一致しない限り再利用しない（圧縮データは読まないので安い）。
		currentKey, ok := archiveIdentity(task.archivePath, limits, info)
		if !ok || currentKey.StructureHash != previousKey.StructureHash {
			return cache.ItemRecord{}, nil, false
		}
	}

	tree, err := walker.walk(ctx, target, limits)
	if err != nil {
		return cache.ItemRecord{}, nil, false
	}
	current := cache.FolderFingerprintFromTree(target, tree)
	if !previousPrint.Same(current) {
		return cache.ItemRecord{}, nil, false
	}
	return record, tree, true
}

// taskID はタスクに対応する項目IDを返す。analyzeArchive と同じ規則で作る。
func taskID(task verifyTask) string {
	if task.archivePath != "" {
		if len(task.candidates) == 1 {
			return model.MakeID(task.archivePath, task.candidates[0].path)
		}
		return model.MakeID(task.archivePath, "")
	}
	return model.MakeID("", task.folderPath)
}
