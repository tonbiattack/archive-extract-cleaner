package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/testutil"
)

// 受け入れ試験: 破損アーカイブ・7z 未導入・不一致が削除候補にならないこと。
// docs/STORE_READINESS.md のチェックリストに対応する自動検証。
func TestAcceptanceUnsafeItemsAreNeverCandidates(t *testing.T) {
	root := t.TempDir()

	// 一致する組。これだけが候補になる。
	if err := testutil.WriteZip(filepath.Join(root, "ok.zip"), []testutil.Entry{{Name: "ok/a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "ok"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}

	// 内容が異なる組。
	if err := testutil.WriteZip(filepath.Join(root, "diff.zip"), []testutil.Entry{{Name: "diff/a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "diff"), "a.txt", "bravo"); err != nil {
		t.Fatal(err)
	}

	// 破損した ZIP。
	brokenZip := filepath.Join(root, "broken.zip")
	if err := testutil.WriteZip(brokenZip, []testutil.Entry{{Name: "broken/a.txt", Content: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(brokenZip)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(brokenZip, data[:len(data)/2], 0o600); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "broken"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}

	// 7z は 7-Zip 未導入の環境では未検証になる。
	if err := os.WriteFile(filepath.Join(root, "seven.7z"), []byte("not a real 7z"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFile(filepath.Join(root, "seven"), "a.txt", "alpha"); err != nil {
		t.Fatal(err)
	}

	a := newTestApp(t)
	if _, err := a.Scan([]string{root}); err != nil {
		t.Fatal(err)
	}

	byName := map[string]model.Item{}
	for _, item := range a.items {
		byName[item.DisplayName()] = item
	}

	// 一致した組だけが削除候補。
	if got := byName["ok"].Verdict; got != model.VerdictDeleteCandidate {
		t.Fatalf("ok verdict = %s, want DELETE_CANDIDATE", got)
	}
	// 不一致は候補にしない。
	if got := byName["diff"].Verdict; got != model.VerdictMismatch {
		t.Fatalf("diff verdict = %s, want MISMATCH", got)
	}
	// 破損は未検証。
	if got := byName["broken"].Verdict; got != model.VerdictUnverified {
		t.Fatalf("broken verdict = %s, want UNVERIFIED", got)
	}
	// 7z は 7-Zip が無ければ未検証。
	if got := byName["seven"].Verdict; got != model.VerdictUnverified {
		t.Fatalf("seven verdict = %s, want UNVERIFIED", got)
	}

	// 候補は ok だけ。安全側の判定が候補へ昇格していないこと。
	ids := a.CandidateIDs(RowQuery{Filter: "candidate"})
	if len(ids) != 1 {
		t.Fatalf("candidate count = %d, want 1: %v", len(ids), ids)
	}
	if a.items[a.index[ids[0]]].DisplayName() != "ok" {
		t.Fatalf("unexpected candidate: %s", a.items[a.index[ids[0]]].DisplayName())
	}
}

// 受け入れ試験: スキャン後にフォルダが変わったら移動しないこと。
func TestAcceptanceChangedFolderIsSkipped(t *testing.T) {
	a, id, _, folder := safetyPair(t)
	if err := testutil.WriteFile(folder, "a.txt", "changed"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	a.mover = func(string) error { calls++; return nil }
	response, err := a.DeleteSelected([]MoveRequest{{ID: id, Method: MoveMethodFolder}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || response.Skipped != 1 {
		t.Fatalf("changed folder was not skipped: calls=%d response=%+v", calls, response)
	}
	if _, err := os.Stat(folder); err != nil {
		t.Fatalf("folder disappeared: %v", err)
	}
}
