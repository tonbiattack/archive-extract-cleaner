package removal

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/tonbiattack/archive-extract-cleaner/internal/fswalk"
	"github.com/tonbiattack/archive-extract-cleaner/internal/winpath"
)

type Result struct {
	Success     bool
	Reason      string
	Quarantined string
}

func Delete(ctx context.Context, folder string, roots []string) Result {
	if err := ctx.Err(); err != nil {
		return Result{Reason: err.Error()}
	}
	abs, err := winpath.CleanAbs(folder)
	if err != nil {
		return Result{Reason: "パスが不正です: " + err.Error()}
	}
	for _, root := range roots {
		if winpath.EqualPath(root, abs) {
			return Result{Reason: "スキャン対象ルート自身は削除できません"}
		}
	}
	allowed := false
	var matchedRoot string
	for _, root := range roots {
		if winpath.IsWithin(root, abs) {
			allowed = true
			matchedRoot = root
			break
		}
	}
	if !allowed {
		return Result{Reason: "対象ルートの外にあるフォルダは削除できません"}
	}
	if err := winpath.CheckNoReparseAncestors(matchedRoot, abs); err != nil {
		return Result{Reason: "安全を確認できないため削除を中止しました: " + err.Error()}
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return Result{Reason: "フォルダを確認できません: " + err.Error()}
	}
	if !info.IsDir() {
		return Result{Reason: "削除対象がフォルダではありません"}
	}
	reparse, err := winpath.IsReparsePoint(abs)
	if err != nil {
		return Result{Reason: "リンク/ジャンクションを確認できません: " + err.Error()}
	}
	if reparse {
		return Result{Reason: "リンク/ジャンクションは削除できません"}
	}
	if err := fswalk.CheckNoLinks(ctx, abs); err != nil {
		return Result{Reason: "安全を確認できないため削除を中止しました: " + err.Error()}
	}

	parent := filepath.Dir(abs)
	base := filepath.Base(abs)
	stamp := strconv.FormatInt(time.Now().UnixNano(), 36)
	quarantine := filepath.Join(parent, "."+base+".deleting-"+stamp)
	if _, err := os.Lstat(quarantine); err == nil {
		return Result{Reason: "一時退避先が既に存在します"}
	}
	if err := os.Rename(abs, quarantine); err != nil {
		return Result{Reason: "削除前の退避に失敗しました: " + err.Error()}
	}
	restore := func(reason string) Result {
		if renameErr := os.Rename(quarantine, abs); renameErr != nil {
			return Result{Reason: reason + "（元に戻せませんでした: " + renameErr.Error() + "）", Quarantined: quarantine}
		}
		return Result{Reason: reason}
	}

	info, err = os.Lstat(quarantine)
	if err != nil {
		return restore("退避後の確認に失敗しました: " + err.Error())
	}
	if !info.IsDir() {
		return restore("退避先がフォルダではありません")
	}
	reparse, err = winpath.IsReparsePoint(quarantine)
	if err != nil {
		return restore("退避先の確認に失敗しました: " + err.Error())
	}
	if reparse {
		return restore("退避先がリンク/ジャンクションです")
	}
	if err := fswalk.CheckNoLinks(ctx, quarantine); err != nil {
		return restore("退避後にリンクを検出しました: " + err.Error())
	}
	if err := os.RemoveAll(quarantine); err != nil {
		return Result{Reason: "削除に失敗しました: " + err.Error(), Quarantined: quarantine}
	}
	return Result{Success: true, Reason: "削除しました"}
}
