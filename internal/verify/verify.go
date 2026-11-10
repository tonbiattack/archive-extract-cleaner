package verify

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tonbiattack/archive-extract-cleaner/internal/fswalk"
	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/winpath"
	"github.com/tonbiattack/archive-extract-cleaner/internal/ziparchive"
)

type Options struct {
	Limits     model.Limits
	Roots      []string
	BeforeRead func(path string)
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
}

const maxDetails = 50

type structuralDiff struct {
	variant      ziparchive.Variant
	missingFiles []string
	extraFiles   []string
	missingDirs  []string
	extraDirs    []string
	conflicts    []string
	total        int
}

func (d structuralDiff) exact() bool {
	return d.total == 0
}

func Verify(ctx context.Context, zipPath, folderPath string, opts Options) Result {
	if isRoot(folderPath, opts.Roots) {
		return Result{Verdict: model.VerdictUnverified, Reason: "スキャン対象ルート自身は削除できません"}
	}
	archive, err := ziparchive.Open(zipPath, opts.Limits)
	if err != nil {
		return Result{Verdict: model.VerdictUnverified, Reason: "ZIPを検証できません: " + err.Error()}
	}
	defer archive.Close()

	tree, err := fswalk.Walk(ctx, folderPath, opts.Limits)
	if err != nil {
		return Result{Verdict: model.VerdictUnverified, Reason: "フォルダを検証できません: " + err.Error(), ZipEntries: archive.EntryCount}
	}

	base := Result{
		FolderSize: tree.Size,
		FileCount:  tree.FileCount,
		DirCount:   tree.DirCount,
		ZipEntries: archive.EntryCount,
	}

	var exact []structuralDiff
	var best structuralDiff
	haveBest := false
	for _, variant := range archive.Variants {
		diff := compare(variant, tree)
		if diff.exact() {
			exact = append(exact, diff)
			continue
		}
		if !haveBest || diff.total < best.total || (diff.total == best.total && exactMode(variant.Mode) == model.MatchDirect) {
			best = diff
			haveBest = true
		}
	}

	if len(exact) > 1 {
		base.Verdict = model.VerdictAmbiguous
		base.Reason = "ZIPとフォルダの対応が複数成立するため判定できません"
		base.Details = []string{"直接比較とトップディレクトリ除去比較の両方が一致しました"}
		return base
	}
	if len(exact) == 0 {
		base.Verdict = model.VerdictMismatch
		base.Reason = "ZIPとフォルダの内容が一致しません"
		base.MatchMode = exactMode(best.variant.Mode)
		base.Details = diffDetails(best)
		return base
	}

	mode := exact[0].variant.Mode
	base.MatchMode = exactMode(mode)
	compared, mismatch, err := verifyContent(ctx, archive, tree, folderPath, exact[0].variant, opts)
	base.ComparedBytes = compared
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
		base.Reason = "ZIPの状態を再確認できません: " + err.Error()
		return base
	}
	if zipInfo.Size() != archive.Size || !zipInfo.ModTime().Equal(archive.ModTime) {
		base.Verdict = model.VerdictUnverified
		base.Reason = "検証中にZIPが変更されました"
		return base
	}

	base.Verdict = model.VerdictDeleteCandidate
	base.Reason = "ZIPと解凍済みフォルダの内容が完全一致しました"
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

func compare(variant ziparchive.Variant, tree *fswalk.Tree) structuralDiff {
	diff := structuralDiff{variant: variant}
	conflicts := map[string]bool{}

	for key, file := range variant.Files {
		if _, isDir := tree.Dirs[key]; isDir {
			diff.conflicts = append(diff.conflicts, fmt.Sprintf("種類が異なります（ZIPはファイル、フォルダはディレクトリ）: %s", file.Path))
			conflicts[key] = true
			continue
		}
		if _, ok := tree.Files[key]; !ok {
			diff.missingFiles = append(diff.missingFiles, file.Path)
		}
	}
	for key, file := range tree.Files {
		if _, isDir := variant.Dirs[key]; isDir {
			if !conflicts[key] {
				diff.conflicts = append(diff.conflicts, fmt.Sprintf("種類が異なります（ZIPはディレクトリ、フォルダはファイル）: %s", file.Path))
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
			display := variant.DirPaths[key]
			if display == "" {
				display = key
			}
			diff.missingDirs = append(diff.missingDirs, display)
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
		details = append(details, "ZIPにのみ存在: "+path)
	}
	for _, path := range diff.extraFiles {
		details = append(details, "フォルダにのみ存在: "+path)
	}
	for _, path := range diff.missingDirs {
		details = append(details, "ZIPにのみ存在するディレクトリ: "+path)
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

func verifyContent(ctx context.Context, archive *ziparchive.Archive, tree *fswalk.Tree, folder string, variant ziparchive.Variant, opts Options) (int64, []string, error) {
	zipBefore, err := os.Stat(archive.Path)
	if err != nil {
		return 0, nil, err
	}
	if zipBefore.Size() != archive.Size || !zipBefore.ModTime().Equal(archive.ModTime) {
		return 0, nil, errors.New("検証前にZIPが変更されました")
	}
	keys := make([]string, 0, len(variant.Files))
	for key := range variant.Files {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var compared int64
	var mismatches []string
	buffer := make([]byte, 1<<20)
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return compared, nil, err
		}
		zf := variant.Files[key]
		tf := tree.Files[key]
		path := filepath.Join(folder, filepath.FromSlash(tf.Path))

		info, err := os.Lstat(path)
		if err != nil {
			return compared, nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return compared, nil, fswalk.ErrLink
		}
		reparse, err := winpath.IsReparsePoint(path)
		if err != nil {
			return compared, nil, err
		}
		if reparse {
			return compared, nil, fswalk.ErrLink
		}
		if !info.Mode().IsRegular() {
			return compared, nil, fswalk.ErrIrregular
		}
		if info.Size() != tf.Size || !info.ModTime().Equal(tf.ModTime) {
			return compared, nil, errors.New("検証中にファイルが変更されました: " + tf.Path)
		}
		if uint64(info.Size()) != zf.File.UncompressedSize64 {
			mismatches = append(mismatches, fmt.Sprintf("サイズが異なります: %s（ZIP %d / フォルダ %d）", zf.Path, zf.File.UncompressedSize64, info.Size()))
			continue
		}
		if opts.BeforeRead != nil {
			opts.BeforeRead(path)
		}

		fileHandle, err := os.Open(path)
		if err != nil {
			return compared, nil, err
		}
		before, err := fileHandle.Stat()
		if err != nil {
			fileHandle.Close()
			return compared, nil, err
		}
		if before.Size() != tf.Size || !before.ModTime().Equal(tf.ModTime) {
			fileHandle.Close()
			return compared, nil, errors.New("検証中にファイルが変更されました: " + tf.Path)
		}
		fileHash := sha256.New()
		n, err := io.CopyBuffer(fileHash, fileHandle, buffer)
		after, statErr := fileHandle.Stat()
		closeErr := fileHandle.Close()
		if err != nil {
			return compared, nil, err
		}
		if statErr != nil {
			return compared, nil, statErr
		}
		if closeErr != nil {
			return compared, nil, closeErr
		}
		if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
			return compared, nil, errors.New("検証中にファイルが変更されました: " + tf.Path)
		}
		if after.Size() != tf.Size || !after.ModTime().Equal(tf.ModTime) {
			return compared, nil, errors.New("検証中にファイルが変更されました: " + tf.Path)
		}
		if n != before.Size() {
			return compared, nil, errors.New("ファイルの読み取りサイズが変化しました: " + tf.Path)
		}

		zipHandle, err := zf.File.Open()
		if err != nil {
			return compared, nil, err
		}
		zipHash := sha256.New()
		zn, err := io.CopyBuffer(zipHash, zipHandle, buffer)
		closeErr = zipHandle.Close()
		if err != nil {
			return compared, nil, err
		}
		if closeErr != nil {
			return compared, nil, closeErr
		}
		if uint64(zn) != zf.File.UncompressedSize64 {
			return compared, nil, errors.New("ZIPエントリのサイズが一致しません: " + zf.Path)
		}
		compared += n
		if n != zn || !bytesEqual(fileHash.Sum(nil), zipHash.Sum(nil)) {
			mismatches = append(mismatches, "内容が異なります: "+zf.Path)
		}
	}

	zipAfter, err := os.Stat(archive.Path)
	if err != nil {
		return compared, nil, err
	}
	if zipAfter.Size() != zipBefore.Size() || !zipAfter.ModTime().Equal(zipBefore.ModTime()) {
		return compared, nil, errors.New("検証中にZIPが変更されました")
	}
	return compared, mismatches, nil
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
