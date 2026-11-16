package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/tonbiattack/archive-extract-cleaner/internal/advisor"
	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/oplog"
)

// AI 連携。AI は「推薦」だけを返し、削除の可否は決めない。
// 削除候補の権威は verify の完全一致判定に残す。
// 提案はユーザーが項目ごとに承認した場合にのみ、既存の移動フローへ渡す。
// ただし現状は、内容一致を確認できていない項目を AI 推薦で削除対象へ
// 昇格させない（安全側の既定）。将来の拡張でもこの境界は維持する。

type SuggestionInfo struct {
	ID          string    `json:"id"`
	Decision    string    `json:"decision"`
	Confidence  float64   `json:"confidence"`
	Reasons     []string  `json:"reasons"`
	Model       string    `json:"model"`
	EvidenceSHA string    `json:"evidenceSha"`
	Error       string    `json:"error,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

type AIState struct {
	Enabled      bool   `json:"enabled"`
	Model        string `json:"model"`
	Available    bool   `json:"available"`
	KeyEnv       string `json:"keyEnv"`
	KeyPresent   bool   `json:"keyPresent"`
	PendingCount int    `json:"pendingCount"`
}

type aiSuggestionStore struct {
	mu      sync.Mutex
	entries map[string]SuggestionInfo
}

func (s *aiSuggestionStore) set(id string, info SuggestionInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries == nil {
		s.entries = map[string]SuggestionInfo{}
	}
	s.entries[id] = info
}

func (s *aiSuggestionStore) get(id string) (SuggestionInfo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	info, ok := s.entries[id]
	return info, ok
}

func (s *aiSuggestionStore) all() []SuggestionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]SuggestionInfo, 0, len(s.entries))
	for _, info := range s.entries {
		result = append(result, info)
	}
	return result
}

func (s *aiSuggestionStore) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = map[string]SuggestionInfo{}
}

func (a *App) GetAIState() AIState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.aiStateLocked()
}

// SuggestForItem は1件についてAI推薦を取得する。
// AIが無効な場合は Disabled の提案を返し、エラーにしない。
func (a *App) SuggestForItem(id string) (SuggestionInfo, error) {
	a.mu.RLock()
	index, ok := a.index[id]
	var item model.Item
	if ok {
		item = a.items[index]
	}
	items := append([]model.Item(nil), a.items...)
	base := a.ctx
	currentAdvisor := a.advisor
	a.mu.RUnlock()
	if !ok {
		return SuggestionInfo{}, errors.New("項目が見つかりません")
	}
	if base == nil {
		base = context.Background()
	}
	// AIは削除候補にしない組だけを対象にする。安全側の判定をAIで上書きしない。
	if item.Deletable() {
		return SuggestionInfo{}, errors.New("この項目は既に削除候補です。AIの判断は不要です")
	}
	if item.Verdict == model.VerdictUnverified {
		return SuggestionInfo{}, errors.New("未検証の項目はAIの対象外です。内容を確認できないため保持してください")
	}
	if currentAdvisor == nil {
		currentAdvisor = advisor.Disabled{}
	}

	evidence := advisor.BuildEvidence(item, advisor.Siblings(item, items))
	ctx, cancel := context.WithTimeout(base, 60*time.Second)
	defer cancel()
	suggestion, err := currentAdvisor.Suggest(ctx, evidence)
	info := SuggestionInfo{
		ID:          id,
		Decision:    string(suggestion.Decision),
		Confidence:  suggestion.Confidence,
		Reasons:     suggestion.Reasons,
		Model:       suggestion.Model,
		EvidenceSHA: suggestion.EvidenceSHA,
		CreatedAt:   time.Now(),
	}
	if err != nil {
		info.Error = err.Error()
		a.suggestions.set(id, info)
		return info, err
	}
	if suggestion.Disabled {
		info.Error = "AIは無効です。設定ファイルの ai.enabled と環境変数を確認してください"
		a.suggestions.set(id, info)
		return info, nil
	}
	info.Reasons = capReasons(info.Reasons)
	a.suggestions.set(id, info)
	a.appendSuggestionLog(item, info)
	a.emit("ai:suggestion", info)
	return info, nil
}

// GetSuggestions は取得済みの推薦を返す。
func (a *App) GetSuggestions() []SuggestionInfo {
	return a.suggestions.all()
}

func capReasons(reasons []string) []string {
	const limit = 5
	if len(reasons) <= limit {
		return reasons
	}
	return reasons[:limit]
}

// appendSuggestionLog は AI 推薦を監査ログに残す。
// 誤削除の事後調査のため、モデル名・証拠ハッシュ・判断を記録する。
func (a *App) appendSuggestionLog(item model.Item, info SuggestionInfo) {
	a.mu.RLock()
	logger := a.log
	a.mu.RUnlock()
	if logger == nil {
		return
	}
	reasons := strings.Join(info.Reasons, " / ")
	err := logger.Append(oplog.Entry{
		Time:       info.CreatedAt,
		Action:     "ai_suggestion",
		Method:     info.Model,
		ID:         item.ID,
		ZipPath:    item.ZipPath,
		FolderPath: item.FolderPath,
		Verdict:    string(item.Verdict),
		Success:    info.Error == "",
		Error:      info.Error,
		Reason:     reasons,
		EvidenceID: info.EvidenceSHA,
		Confidence: info.Confidence,
		Decision:   info.Decision,
	})
	if err != nil {
		a.emit("ai:log_error", err.Error())
	}
}

// aiConfigured は設定から Advisor を組み立てる。
func aiConfigured(enabled bool, endpoint, model, keyEnv string, allowInsecure bool) advisor.Advisor {
	if !enabled || strings.TrimSpace(endpoint) == "" {
		return advisor.Disabled{}
	}
	cloud, err := advisor.NewCloud(advisor.CloudConfig{
		Enabled:       true,
		Endpoint:      endpoint,
		Model:         model,
		APIKeyEnv:     keyEnv,
		AllowInsecure: allowInsecure,
	})
	if err != nil {
		return advisor.Disabled{}
	}
	return cloud
}
