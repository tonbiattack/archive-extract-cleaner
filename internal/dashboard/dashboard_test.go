package dashboard

import (
	"testing"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
)

func sampleItems() []model.Item {
	return []model.Item{
		{
			ID: "a", Verdict: model.VerdictDeleteCandidate, Status: model.StatusReady,
			Size: 100, FolderSize: 100, ZipSize: 40,
		},
		{
			ID: "b", Verdict: model.VerdictDeleteCandidateIgnorable, Status: model.StatusReady,
			Size: 200, FolderSize: 200, ZipSize: 80,
		},
		{
			ID: "c", Verdict: model.VerdictKeepArchive, Status: model.StatusReady,
			ZipPath: "c.zip", ZipSize: 50, Size: 50,
		},
		{
			ID: "d", Verdict: model.VerdictKeepDirectory, Status: model.StatusReady,
			FolderPath: "d", FolderSize: 300, Size: 300,
		},
		{
			ID: "e", Verdict: model.VerdictMismatch, Status: model.StatusReady,
			FolderSize: 400, Size: 400,
		},
		{
			ID: "f", Verdict: model.VerdictUnverified, Status: model.StatusReady,
			FolderSize: 500, Size: 500,
		},
		{
			ID: "g", Verdict: model.VerdictAmbiguous, Status: model.StatusReady,
			FolderSize: 600, Size: 600,
		},
	}
}

func TestBuildScanExcludesNonCandidatesFromReclaimable(t *testing.T) {
	scan := BuildScan(sampleItems())
	if scan.Reclaimable.Count != 2 {
		t.Fatalf("reclaimable count = %d", scan.Reclaimable.Count)
	}
	if scan.Reclaimable.Bytes != 300 {
		t.Fatalf("reclaimable bytes = %d, want 300", scan.Reclaimable.Bytes)
	}
	// MISMATCH / UNVERIFIED / AMBIGUOUS が回収見込みに混ざっていないこと。
	if scan.Mismatch.Bytes != 400 || scan.Unverified.Bytes != 500 || scan.Ambiguous.Bytes != 600 {
		t.Fatalf("excluded categories = %+v", scan)
	}
}

func TestBuildScanSeparatesKeepFromReclaimable(t *testing.T) {
	scan := BuildScan(sampleItems())
	if scan.KeepArchive.Bytes != 50 {
		t.Fatalf("keep archive bytes = %d", scan.KeepArchive.Bytes)
	}
	if scan.KeepFolder.Bytes != 300 {
		t.Fatalf("keep folder bytes = %d", scan.KeepFolder.Bytes)
	}
}

func TestBuildScanCountsRecycled(t *testing.T) {
	items := sampleItems()
	items[0].Status = model.StatusDeleted
	// 移動後は判定が KEEP_ARCHIVE などへ変わる。ここでは状態だけを変えて
	// Recycled が独立して数えられることを確認する。
	items[0].Verdict = model.VerdictKeepArchive
	scan := BuildScan(items)
	if scan.Recycled.Count != 1 {
		t.Fatalf("recycled = %+v", scan.Recycled)
	}
	if scan.Reclaimable.Count != 1 {
		t.Fatalf("reclaimable count = %d, want 1", scan.Reclaimable.Count)
	}
	// 移動後は ZIP サイズが代表値になる（実装の移動後状態と同じ）。
	if scan.Recycled.Bytes != 40 {
		t.Fatalf("recycled bytes = %d, want 40", scan.Recycled.Bytes)
	}
}

func TestBuildScanTotalMatchesItems(t *testing.T) {
	items := sampleItems()
	scan := BuildScan(items)
	if scan.Total.Count != len(items) {
		t.Fatalf("total count = %d", scan.Total.Count)
	}
	sum := scan.Reclaimable.Bytes + scan.KeepArchive.Bytes + scan.KeepFolder.Bytes +
		scan.Mismatch.Bytes + scan.Unverified.Bytes + scan.Ambiguous.Bytes
	if sum != scan.Total.Bytes {
		t.Fatalf("categories sum = %d, total = %d", sum, scan.Total.Bytes)
	}
}

func TestAddMoveResultSeparatesMovedFromRequested(t *testing.T) {
	var moves Moves
	moves.AddMoveResult(MoveOutcome{Success: true, Bytes: 100, MovedBytes: 100})
	moves.AddMoveResult(MoveOutcome{Partial: true, Bytes: 200, MovedBytes: 120})
	moves.AddMoveResult(MoveOutcome{Bytes: 300})
	moves.AddMoveResult(MoveOutcome{Skipped: true, Bytes: 400})

	if moves.Succeeded.Count != 1 || moves.Succeeded.Bytes != 100 {
		t.Fatalf("succeeded = %+v", moves.Succeeded)
	}
	if moves.Partial.Count != 1 || moves.Partial.Bytes != 200 {
		t.Fatalf("partial = %+v", moves.Partial)
	}
	if moves.Failed.Count != 1 || moves.Failed.Bytes != 300 {
		t.Fatalf("failed = %+v", moves.Failed)
	}
	if moves.Skipped.Count != 1 {
		t.Fatalf("skipped = %+v", moves.Skipped)
	}
	// 実際に移せた容量だけを積む。要求容量とは別。
	if moves.MovedBytes != 220 {
		t.Fatalf("moved bytes = %d, want 220", moves.MovedBytes)
	}
}

func TestBuildKeepsFilterState(t *testing.T) {
	items := sampleItems()
	dash := Build(items, Moves{}, true, 2)
	if !dash.Filtered || dash.FilteredCount != 2 {
		t.Fatalf("dashboard = %+v", dash)
	}
	// 絞り込み中でもスキャン全体の値であること。
	if dash.Scan.Total.Count != len(items) {
		t.Fatalf("scan total = %d, want %d", dash.Scan.Total.Count, len(items))
	}
}

func TestRecycleNoteWarnsAboutRecycleBin(t *testing.T) {
	note := RecycleNote()
	if note == "" {
		t.Fatal("note must not be empty")
	}
}
