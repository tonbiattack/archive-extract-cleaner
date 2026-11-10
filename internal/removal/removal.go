package removal

import (
	"context"
	"errors"
	"os"

	"github.com/tonbiattack/archive-extract-cleaner/internal/fswalk"
	"github.com/tonbiattack/archive-extract-cleaner/internal/winpath"
)

type Mover func(folder string) error

type Result struct {
	Success bool
	Reason  string
}

func Delete(ctx context.Context, folder string, roots []string, mover Mover) Result {
	if err := ctx.Err(); err != nil {
		return Result{Reason: err.Error()}
	}
	abs, err := winpath.CleanAbs(folder)
	if err != nil {
		return Result{Reason: "パスが不正です: " + err.Error()}
	}
	for _, root := range roots {
		if winpath.EqualPath(root, abs) {
			return Result{Reason: "スキャン対象ルート自身は移動できません"}
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
		return Result{Reason: "対象ルートの外にあるフォルダは移動できません"}
	}
	if err := winpath.CheckNoReparseAncestors(matchedRoot, abs); err != nil {
		return Result{Reason: "安全を確認できないため移動を中止しました: " + err.Error()}
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return Result{Reason: "フォルダを確認できません: " + err.Error()}
	}
	if !info.IsDir() {
		return Result{Reason: "移動対象がフォルダではありません"}
	}
	reparse, err := winpath.IsReparsePoint(abs)
	if err != nil {
		return Result{Reason: "リンク/ジャンクションを確認できません: " + err.Error()}
	}
	if reparse {
		return Result{Reason: "リンク/ジャンクションは移動できません"}
	}
	if err := fswalk.CheckNoLinks(ctx, abs); err != nil {
		return Result{Reason: "安全を確認できないため移動を中止しました: " + err.Error()}
	}
	if mover == nil {
		mover = MoveToRecycleBin
	}
	if err := mover(abs); err != nil {
		return Result{Reason: "Windowsのごみ箱に移動できませんでした: " + err.Error()}
	}
	if _, err := os.Lstat(abs); err == nil {
		return Result{Reason: "ごみ箱への移動後も元のパスが存在します"}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{Reason: "移動後の状態を確認できません: " + err.Error()}
	}
	return Result{Success: true, Reason: "Windowsのごみ箱に移動しました"}
}
