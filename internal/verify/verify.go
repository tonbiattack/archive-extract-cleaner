package verify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tonbiattack/archive-extract-cleaner/internal/archive"
	"github.com/tonbiattack/archive-extract-cleaner/internal/cache"
	"github.com/tonbiattack/archive-extract-cleaner/internal/fswalk"
	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/winpath"
)

// HashCache は ZIP エントリの展開後ハッシュを再利用するための抽象。
// 実装は internal/cache.ArchiveHashes。
type HashCache interface {
	Lookup(key cache.ArchiveKey) (cache.ArchiveRecord, bool)
	Put(record cache.ArchiveRecord)
}

type Options struct {
	Limits     model.Limits
	Roots      []string
	BeforeRead func(path string)
	// Hashes は省略可能。nil なら毎回 ZIP を展開してハッシュを計算する。
	// キャッシュは高速化のためだけに使い、削除可否の判定根拠にはしない。
	Hashes HashCache
	// Tree は呼び出し側で走査済みのフォルダツリー。指定すると最初の走査を省略する。
	// 内容は解析時に検証され、検証後の再走査は従来どおり実行する。
	Tree *fswalk.Tree
}

type Result struct {
	Verdict       model.Verdict
	Reason        string
	Details       []string
	MatchMode     model.MatchMode
	FolderSize    int64
	FileCount     int
	DirCount      int
	ComparedBytes int64
	ZipEntries    int
	// CachedHashes はキャッシュから復元した ZIP エントリの数。
	CachedHashes int
	// ComputedHashes は実際に ZIP を展開して計算したエントリの数。
	ComputedHashes int
}

const maxDetails = 50

type structuralDiff struct {
	variant      archive.Variant
	missingFiles []string
	extraFiles   []string
	missingDirs  []string
	extraDirs    []string
	conflicts    []string
	total        int
	// ignorable は差分のうち、ZIPにのみ存在する補助ファイル／補助ディレクトリの数。
	ignorable int
}

func (d structuralDiff) exact() bool {
	return d.total == 0
}

// ignorableOnly は差分が補助ファイルだけかを返す。
func (d structuralDiff) ignorableOnly() bool {
	return d.total > 0 && d.ignorable == d.total
}

func Verify(ctx context.Context, zipPath, folderPath string, opts Options) Result {
	if isRoot(folderPath, opts.Roots) {
		return Result{Verdict: model.VerdictUnverified, Reason: "スキャン対象ルート自身は削除できません"}
	}
	opened, err := archive.Open(zipPath, opts.Limits)
	if err != nil {
		return Result{Verdict: model.VerdictUnverified, Reason: "アーカイブを検証できません: " + err.Error()}
	}
	defer opened.Close()

	tree := opts.Tree
	if tree == nil {
		tree, err = fswalk.Walk(ctx, folderPath, opts.Limits)
		if err != nil {
			return Result{Verdict: model.VerdictUnverified, Reason: "フォルダを検証できません: " + err.Error(), ZipEntries: opened.EntryCount()}
		}
	}

	base := Result{
		FolderSize: tree.Size,
		FileCount:  tree.FileCount,
		DirCount:   tree.DirCount,
		ZipEntries: opened.EntryCount(),
	}

	var exact []structuralDiff
	var ignorable []structuralDiff
	var best structuralDiff
	haveBest := false
	for _, variant := range opened.Variants() {
		diff := compare(variant, tree)
		if diff.exact() {
			exact = append(exact, diff)
			continue
		}
		if diff.ignorableOnly() {
			ignorable = append(ignorable, diff)
			continue
		}
		if !haveBest || diff.total < best.total || (diff.total == best.total && exactMode(variant.Mode) == model.MatchDirect) {
			best = diff
			haveBest = true
		}
	}

	if len(exact) > 1 {
		base.Verdict = model.VerdictAmbiguous
		base.Reason = "アーカイブとフォルダの対応が複数成立するため判定できません"
		base.Details = []string{"直接比較とトップディレクトリ除去比較の両方が一致しました"}
		return base
	}
	if len(exact) == 0 && len(ignorable) == 0 {
		base.Verdict = model.VerdictMismatch
		base.Reason = "アーカイブとフォルダの内容が一致しません"
		base.MatchMode = exactMode(best.variant.Mode)
		base.Details = diffDetails(best)
		return base
	}
	if len(exact) > 0 && len(ignorable) > 0 {
		base.Verdict = model.VerdictAmbiguous
		base.Reason = "アーカイブとフォルダの対応が複数成立するため判定できません"
		base.Details = []string{"完全一致と補助ファイルのみの差分が同時に成立しました"}
		return base
	}

	// 補助ファイルはZIP側にしか無い前提なので、差分が出るのは比較対象のZIPの方が
	// エントリ数が多い場合だけ。共通部分の内容はSHA-256で確認する。
	selected := exact
	ignorableOnly := false
	if len(selected) == 0 {
		selected = ignorable
		ignorableOnly = true
	}
	if len(selected) > 1 {
		base.Verdict = model.VerdictAmbiguous
		base.Reason = "アーカイブとフォルダの対応が複数成立するため判定できません"
		base.Details = []string{"直接比較とトップディレクトリ除去比較の両方が同じ差分になりました"}
		return base
	}

	diff := selected[0]
	mode := diff.variant.Mode
	base.MatchMode = exactMode(mode)
	compared, mismatch, cachedCount, computedCount, err := verifyContent(ctx, opened, tree, folderPath, diff.variant, opts)
	base.ComparedBytes = compared
	base.CachedHashes = cachedCount
	base.ComputedHashes = computedCount
	if err != nil {
		base.Verdict = model.VerdictUnverified
		base.Reason = "検証中に問題が発生しました: " + err.Error()
		return base
	}
	if len(mismatch) > 0 {
		base.Verdict = model.VerdictMismatch
		base.Reason = "ファイル内容が一致しません"
		base.Details = capStrings(mismatch)
		return base
	}

	after, err := fswalk.Walk(ctx, folderPath, opts.Limits)
	if err != nil {
		base.Verdict = model.VerdictUnverified
		base.Reason = "検証後の再走査に失敗しました: " + err.Error()
		return base
	}
	if !tree.SameAs(after) {
		base.Verdict = model.VerdictUnverified
		base.Reason = "検証中にフォルダの内容が変更されました"
		return base
	}

	zipInfo, err := os.Stat(zipPath)
	if err != nil {
		base.Verdict = model.VerdictUnverified
		base.Reason = "アーカイブの状態を再確認できません: " + err.Error()
		return base
	}
	if zipInfo.Size() != opened.Size() || !zipInfo.ModTime().Equal(opened.ModTime()) {
		base.Verdict = model.VerdictUnverified
		base.Reason = "検証中にアーカイブが変更されました"
		return base
	}

	if ignorableOnly {
		base.Verdict = model.VerdictDeleteCandidateIgnorable
		base.Reason = fmt.Sprintf(
			"補助ファイル %d 件を除いてアーカイブと解凍済みフォルダの内容が一致しました",
			diff.total,
		)
		base.Details = diffDetails(diff)
		return base
	}

	base.Verdict = model.VerdictDeleteCandidate
	base.Reason = "アーカイブと解凍済みフォルダの内容が完全一致しました"
	return base
}

func exactMode(mode string) model.MatchMode {
	if strings.HasPrefix(mode, "strip-top:") {
		return model.MatchStrip
	}
	return model.MatchDirect
}

func isRoot(folder string, roots []string) bool {
	for _, root := range roots {
		if winpath.EqualPath(root, folder) {
			return true
		}
	}
	return false
}

func compare(variant archive.Variant, tree *fswalk.Tree) structuralDiff {
	diff := structuralDiff{variant: variant}
	conflicts := map[string]bool{}

	for key, file := range variant.Files {
		if _, isDir := tree.Dirs[key]; isDir {
			diff.conflicts = append(diff.conflicts, fmt.Sprintf("種類が異なります（アーカイブはファイル、フォルダはディレクトリ）: %s", file.Path))
			conflicts[key] = true
			continue
		}
		if _, ok := tree.Files[key]; !ok {
			diff.missingFiles = append(diff.missingFiles, file.Path)
			if shouldIgnoreZipOnly(file.Path) {
				diff.ignorable++
			}
		}
	}
	for key, file := range tree.Files {
		if _, isDir := variant.Dirs[key]; isDir {
			if !conflicts[key] {
				diff.conflicts = append(diff.conflicts, fmt.Sprintf("種類が異なります（アーカイブはディレクトリ、フォルダはファイル）: %s", file.Path))
				conflicts[key] = true
			}
			continue
		}
		if _, ok := variant.Files[key]; !ok {
			diff.extraFiles = append(diff.extraFiles, file.Path)
		}
	}
	for key := range variant.Dirs {
		if conflicts[key] {
			continue
		}
		if _, ok := tree.Dirs[key]; !ok {
			display := variant.Dirs[key].Path
			if display == "" {
				display = key
			}
			diff.missingDirs = append(diff.missingDirs, display)
			if shouldIgnoreZipOnlyDir(display) {
				diff.ignorable++
			}
		}
	}
	for key := range tree.Dirs {
		if conflicts[key] {
			continue
		}
		if _, ok := variant.Dirs[key]; !ok {
			diff.extraDirs = append(diff.extraDirs, key)
		}
	}

	sort.Strings(diff.missingFiles)
	sort.Strings(diff.extraFiles)
	sort.Strings(diff.missingDirs)
	sort.Strings(diff.extraDirs)
	sort.Strings(diff.conflicts)
	diff.total = len(diff.missingFiles) + len(diff.extraFiles) + len(diff.missingDirs) + len(diff.extraDirs) + len(diff.conflicts)
	return diff
}

func diffDetails(diff structuralDiff) []string {
	var details []string
	for _, path := range diff.missingFiles {
		details = append(details, "アーカイブにのみ存在: "+path)
	}
	for _, path := range diff.extraFiles {
		details = append(details, "フォルダにのみ存在: "+path)
	}
	for _, path := range diff.missingDirs {
		details = append(details, "アーカイブにのみ存在するディレクトリ: "+path)
	}
	for _, path := range diff.extraDirs {
		details = append(details, "フォルダにのみ存在するディレクトリ: "+path)
	}
	details = append(details, diff.conflicts...)
	return capStrings(details)
}

func capStrings(values []string) []string {
	if len(values) <= maxDetails {
		return values
	}
	capped := append([]string(nil), values[:maxDetails]...)
	return append(capped, fmt.Sprintf("ほか %d 件", len(values)-maxDetails))
}

// verifyContent は対応するファイルの内容を比較する。
// ZIPエントリのハッシュは、メタデータと構造指紋が一致する場合だけキャッシュから復元する。
// キャッシュは ZIP の展開コストを省くだけで、フォルダ側は毎回読み直す。
func verifyContent(ctx context.Context, opened archive.Archive, tree *fswalk.Tree, folder string, variant archive.Variant, opts Options) (int64, []string, int, int, error) {
	zipBefore, err := os.Stat(opened.Path())
	if err != nil {
		return 0, nil, 0, 0, err
	}
	if zipBefore.Size() != opened.Size() || !zipBefore.ModTime().Equal(opened.ModTime()) {
		return 0, nil, 0, 0, errors.New("検証前にアーカイブが変更されました")
	}
	keys := make([]string, 0, len(variant.Files))
	for key := range variant.Files {
		// フォルダ側に存在しないエントリは補助ファイルとして扱い、内容比較しない。
		// ここに来るのは compare() が補助ファイルのみの差分と判定した場合だけ。
		if _, ok := tree.Files[key]; !ok {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// キャッシュは ZIP 全体の構造指紋で引く。指紋はセントラルディレクトリから
	// 作るため、同じサイズ・更新時刻のまま中身を差し替えた ZIP も検出できる。
	zipHashes, cachedCount := lookupArchiveHashes(opened, keys, variant, opts)
	computed := make(map[string][]byte, len(keys))

	var compared int64
	var mismatches []string
	computedCount := 0
	buffer := make([]byte, 1<<20)
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return compared, nil, cachedCount, computedCount, err
		}
		zf := variant.Files[key]
		tf := tree.Files[key]
		path := filepath.Join(folder, filepath.FromSlash(tf.Path))

		info, err := os.Lstat(path)
		if err != nil {
			return compared, nil, cachedCount, computedCount, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return compared, nil, cachedCount, computedCount, fswalk.ErrLink
		}
		reparse, err := winpath.IsReparsePoint(path)
		if err != nil {
			return compared, nil, cachedCount, computedCount, err
		}
		if reparse {
			return compared, nil, cachedCount, computedCount, fswalk.ErrLink
		}
		if !info.Mode().IsRegular() {
			return compared, nil, cachedCount, computedCount, fswalk.ErrIrregular
		}
		if info.Size() != tf.Size || !info.ModTime().Equal(tf.ModTime) {
			return compared, nil, cachedCount, computedCount, errors.New("検証中にファイルが変更されました: " + tf.Path)
		}
		if uint64(info.Size()) != uint64(zf.Size) {
			mismatches = append(mismatches, fmt.Sprintf("サイズが異なります: %s（アーカイブ %d / フォルダ %d）", zf.Path, zf.Size, info.Size()))
			continue
		}
		if opts.BeforeRead != nil {
			opts.BeforeRead(path)
		}

		fileHash, n, err := hashFolderFile(path, tf, buffer)
		if err != nil {
			return compared, nil, cachedCount, computedCount, err
		}

		zipDigest, cached := zipHashes[key]
		if !cached {
			zipDigest, err = hashArchiveEntry(opened, zf, buffer)
			if err != nil {
				return compared, nil, cachedCount, computedCount, err
			}
			computed[key] = zipDigest
			computedCount++
		}
		compared += n
		if !bytesEqual(fileHash, zipDigest) {
			mismatches = append(mismatches, "内容が異なります: "+zf.Path)
		}
	}

	zipAfter, err := os.Stat(opened.Path())
	if err != nil {
		return compared, nil, cachedCount, computedCount, err
	}
	if zipAfter.Size() != zipBefore.Size() || !zipAfter.ModTime().Equal(zipBefore.ModTime()) {
		return compared, nil, cachedCount, computedCount, errors.New("検証中にアーカイブが変更されました")
	}

	// 内容比較が最後まで完了したときだけキャッシュへ保存する。
	// 途中で失敗した ZIP を「検証済み」として残さない。
	if len(computed) > 0 {
		saveArchiveHashes(opened, variant, computed, opts)
	}
	return compared, mismatches, cachedCount, computedCount, nil
}

// lookupArchiveHashes はキャッシュから ZIP エントリのハッシュを復元する。
// キャッシュに無い・キーが合わない場合は空を返し、呼び出し側が計算する。
func lookupArchiveHashes(opened archive.Archive, keys []string, variant archive.Variant, opts Options) (map[string][]byte, int) {
	if opts.Hashes == nil {
		return nil, 0
	}
	structure := opened.StructureHash()
	if structure == "" {
		return nil, 0
	}
	key := cache.NewArchiveKey(opened.Path(), opened.Size(), opened.ModTime(), structure)
	record, ok := opts.Hashes.Lookup(key)
	if !ok || len(record.Entries) == 0 {
		return nil, 0
	}
	byPath := make(map[string]cache.ArchiveEntry, len(record.Entries))
	for _, entry := range record.Entries {
		byPath[strings.ToLower(entry.Path)] = entry
	}
	result := make(map[string][]byte, len(keys))
	for _, key := range keys {
		zf, exists := variant.Files[key]
		if !exists {
			continue
		}
		entry, found := byPath[strings.ToLower(zf.Path)]
		if !found {
			return nil, 0
		}
		// CRC とサイズが合わないエントリが1つでもあれば、キャッシュ全体を捨てる。
		if entry.CRC32 != zf.CRC32 || entry.Size != zf.Size {
			return nil, 0
		}
		digest, err := hex.DecodeString(entry.SHA256)
		if err != nil || len(digest) != sha256.Size {
			return nil, 0
		}
		result[key] = digest
	}
	return result, len(result)
}

// saveArchiveHashes は計算したハッシュをキャッシュへ書き込む。
// キャッシュに既にあるエントリも含め、今回比較した ZIP 全体を1レコードとして保存する。
func saveArchiveHashes(opened archive.Archive, variant archive.Variant, computed map[string][]byte, opts Options) {
	if opts.Hashes == nil {
		return
	}
	structure := opened.StructureHash()
	if structure == "" {
		return
	}
	key := cache.NewArchiveKey(opened.Path(), opened.Size(), opened.ModTime(), structure)
	existing, hasExisting := opts.Hashes.Lookup(key)
	byPath := make(map[string]cache.ArchiveEntry, len(computed))
	if hasExisting {
		for _, entry := range existing.Entries {
			byPath[strings.ToLower(entry.Path)] = entry
		}
	}
	var total int64
	for _, zf := range variant.Files {
		digest, ok := computed[zf.Key]
		if !ok {
			continue
		}
		size := zf.Size
		byPath[strings.ToLower(zf.Path)] = cache.ArchiveEntry{
			Path:   zf.Path,
			SHA256: hex.EncodeToString(digest),
			Size:   size,
			CRC32:  zf.CRC32,
		}
		total += size
	}
	entries := make([]cache.ArchiveEntry, 0, len(byPath))
	for _, entry := range byPath {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Path) < strings.ToLower(entries[j].Path)
	})
	opts.Hashes.Put(cache.ArchiveRecord{
		Key:        key,
		Entries:    entries,
		TotalBytes: total,
	})
}

// hashFolderFile はフォルダ側ファイルの SHA-256 を計算する。読取前後の変化も確認する。
func hashFolderFile(path string, tf fswalk.File, buffer []byte) ([]byte, int64, error) {
	fileHandle, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	before, err := fileHandle.Stat()
	if err != nil {
		fileHandle.Close()
		return nil, 0, err
	}
	if before.Size() != tf.Size || !before.ModTime().Equal(tf.ModTime) {
		fileHandle.Close()
		return nil, 0, errors.New("検証中にファイルが変更されました: " + tf.Path)
	}
	fileHash := sha256.New()
	n, err := io.CopyBuffer(fileHash, fileHandle, buffer)
	after, statErr := fileHandle.Stat()
	closeErr := fileHandle.Close()
	if err != nil {
		return nil, 0, err
	}
	if statErr != nil {
		return nil, 0, statErr
	}
	if closeErr != nil {
		return nil, 0, closeErr
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return nil, 0, errors.New("検証中にファイルが変更されました: " + tf.Path)
	}
	if after.Size() != tf.Size || !after.ModTime().Equal(tf.ModTime) {
		return nil, 0, errors.New("検証中にファイルが変更されました: " + tf.Path)
	}
	if n != before.Size() {
		return nil, 0, errors.New("ファイルの読み取りサイズが変化しました: " + tf.Path)
	}
	return fileHash.Sum(nil), n, nil
}

// hashArchiveEntry はアーカイブの1エントリをストリーミング展開して SHA-256 を返す。
func hashArchiveEntry(opened archive.Archive, entry archive.Entry, buffer []byte) ([]byte, error) {
	handle, err := opened.Open(entry)
	if err != nil {
		return nil, err
	}
	hash := sha256.New()
	written, copyErr := io.CopyBuffer(hash, handle, buffer)
	closeErr := handle.Close()
	if copyErr != nil {
		return nil, copyErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if entry.Size > 0 && written != entry.Size {
		return nil, errors.New("アーカイブエントリのサイズが一致しません: " + entry.Path)
	}
	return hash.Sum(nil), nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
