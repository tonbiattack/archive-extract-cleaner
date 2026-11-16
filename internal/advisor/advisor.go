// Package advisor は、決定的な照合では判断できない組について
// AIによる「推薦」を取得するための抽象を提供する。
//
// 設計上の重要な制約:
//   - AIの出力は推薦であり、削除可否を単独で決定しない。
//   - 削除の権威は verify パッケージの判定（SHA-256 完全一致）に残す。
//   - 送信内容は証拠（集約済みメタデータ）に限り、生データを含めない。
//   - 既定では無効。クラウド送信は明示的なオプトインがある場合のみ行う。
package advisor

import (
	"context"
	"errors"
	"fmt"
)

// Decision はAIが返す判断の種類。
type Decision string

const (
	DecisionDeleteFolder Decision = "delete_folder"
	DecisionDeleteZip    Decision = "delete_zip"
	DecisionDeleteBoth   Decision = "delete_both"
	DecisionKeep         Decision = "keep"
	DecisionUnknown      Decision = "unknown"
)

// Evidence はAIに渡す判断材料。生のファイル内容は含まない。
// すべて決定的な処理で計算済みの値だけを入れる。
type Evidence struct {
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`

	ZipName    string `json:"zipName"`
	ZipEntries int    `json:"zipEntries"`
	ZipBytes   int64  `json:"zipBytes"`

	FolderName  string `json:"folderName"`
	FolderFiles int    `json:"folderFiles"`
	FolderDirs  int    `json:"folderDirs"`
	FolderBytes int64  `json:"folderBytes"`

	OnlyZipFiles    int `json:"onlyZipFiles"`
	OnlyFolderFiles int `json:"onlyFolderFiles"`
	MissingDirs     int `json:"missingDirs"`
	ExtraDirs       int `json:"extraDirs"`
	Conflicts       int `json:"conflicts"`

	// ContentOverlap は両方に存在するファイルのうち内容が一致した割合（0.0-1.0）。
	ContentOverlap float64 `json:"contentOverlap"`
	// NameSimilarity は ZIP名とフォルダ名の類似度（0.0-1.0）。
	NameSimilarity float64 `json:"nameSimilarity"`

	// SampleOnlyZip / SampleOnlyFolder は差分の代表例（件数上限あり）。
	SampleOnlyZip    []string `json:"sampleOnlyZip,omitempty"`
	SampleOnlyFolder []string `json:"sampleOnlyFolder,omitempty"`
	// SampleDiff は内容が一致しなかったファイルの代表例。
	SampleDiff []string `json:"sampleDiff,omitempty"`

	// Siblings は同じ親にある他の ZIP／フォルダ名（文脈用、上限あり）。
	Siblings []string `json:"siblings,omitempty"`

	// Details は判定理由の詳細（件数上限あり）。
	Details []string `json:"details,omitempty"`
}

// Suggestion はAIの推薦結果。
type Suggestion struct {
	Decision   Decision `json:"decision"`
	Confidence float64  `json:"confidence"`
	Reasons    []string `json:"reasons"`

	Model string `json:"model"`
	// EvidenceSHA は Evidence のハッシュ。監査とキャッシュに使う。
	EvidenceSHA string `json:"evidenceSha"`
	// Disabled はAIが無効だったことを示す（エラーではなく未実行）。
	Disabled bool `json:"disabled,omitempty"`
}

// Validate はAI出力の妥当性を検査する。
// モデルの応答は信頼できない入力として扱う。
func (s Suggestion) Validate() error {
	switch s.Decision {
	case DecisionDeleteFolder, DecisionDeleteZip, DecisionDeleteBoth, DecisionKeep, DecisionUnknown:
	default:
		return fmt.Errorf("未知の判断です: %q", s.Decision)
	}
	if s.Confidence < 0 || s.Confidence > 1 {
		return fmt.Errorf("信頼度が範囲外です: %v", s.Confidence)
	}
	if len(s.Model) > 200 {
		return errors.New("モデル名が長すぎます")
	}
	return nil
}

// Advisor は推薦を生成する実装の抽象。実装は複数差し替え可能。
type Advisor interface {
	// Name はログやUIに出す識別子。
	Name() string
	// Enabled は現在この実装が利用可能かを返す。
	Enabled() bool
	// Suggest は Evidence から推薦を返す。Enabled() が false の場合は
	// Disabled 付きの Suggestion を返し、エラーにしない。
	Suggest(ctx context.Context, evidence Evidence) (Suggestion, error)
}

// Disabled は常に無効な実装。AI未設定時の既定。
type Disabled struct{}

func (Disabled) Name() string  { return "disabled" }
func (Disabled) Enabled() bool { return false }

func (Disabled) Suggest(context.Context, Evidence) (Suggestion, error) {
	return Suggestion{Decision: DecisionUnknown, Disabled: true, Model: "disabled"}, nil
}

// ClampConfidence はモデル出力の信頼度を 0..1 に丸める。
// 範囲外の値は破棄せず、安全側（低い値）へ寄せる。
func ClampConfidence(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
