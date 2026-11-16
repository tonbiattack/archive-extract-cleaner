package advisor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
)

const (
	// maxSamples はプロンプトに載せる差分サンプルの上限。
	maxSamples = 12
	// maxSiblingNames は文脈として載せる兄弟名の上限。
	maxSiblingNames = 8
	// maxDetailLines は evidence に載せる理由詳細の上限。
	maxDetailLines = 20
)

// BuildEvidence は model.Item から AI 用の証拠を組み立てる。
// ファイル内容は含めず、件数・比率・代表例だけを渡す。
func BuildEvidence(item model.Item, siblings []string) Evidence {
	evidence := Evidence{
		Verdict:        string(item.Verdict),
		Reason:         item.Reason,
		ZipName:        baseName(item.ZipPath),
		ZipEntries:     item.ZipEntries,
		ZipBytes:       item.ZipSize,
		FolderName:     baseName(item.FolderPath),
		FolderFiles:    item.FileCount,
		FolderDirs:     item.DirCount,
		FolderBytes:    item.FolderSize,
		NameSimilarity: nameSimilarity(item.Name, baseName(item.FolderPath)),
		Details:        capList(item.Details, maxDetailLines),
	}

	// Details の接頭辞から差分件数を数える。verify が生成する形式に依存するが、
	// 件数はプロンプトの補助情報であり、判定には使わない。
	for _, detail := range item.Details {
		switch {
		case strings.HasPrefix(detail, "ZIPにのみ存在: "):
			evidence.OnlyZipFiles++
		case strings.HasPrefix(detail, "フォルダにのみ存在: "):
			evidence.OnlyFolderFiles++
		case strings.HasPrefix(detail, "ZIPにのみ存在するディレクトリ: "):
			evidence.MissingDirs++
		case strings.HasPrefix(detail, "フォルダにのみ存在するディレクトリ: "):
			evidence.ExtraDirs++
		case strings.HasPrefix(detail, "種類が異なります"):
			evidence.Conflicts++
		}
	}

	if evidence.ZipEntries > 0 && evidence.FolderFiles > 0 {
		common := evidence.ZipEntries - evidence.OnlyZipFiles - evidence.MissingDirs
		if common < 0 {
			common = 0
		}
		if common > evidence.FolderFiles {
			common = evidence.FolderFiles
		}
		if common > 0 {
			overlap := float64(common-evidence.Conflicts) / float64(common)
			if overlap < 0 {
				overlap = 0
			}
			evidence.ContentOverlap = round3(overlap)
		}
	}

	for _, detail := range item.Details {
		const zipPrefix = "ZIPにのみ存在: "
		const folderPrefix = "フォルダにのみ存在: "
		if strings.HasPrefix(detail, zipPrefix) && len(evidence.SampleOnlyZip) < maxSamples {
			evidence.SampleOnlyZip = append(evidence.SampleOnlyZip, strings.TrimPrefix(detail, zipPrefix))
		}
		if strings.HasPrefix(detail, folderPrefix) && len(evidence.SampleOnlyFolder) < maxSamples {
			evidence.SampleOnlyFolder = append(evidence.SampleOnlyFolder, strings.TrimPrefix(detail, folderPrefix))
		}
		if strings.HasPrefix(detail, "内容が異なります: ") && len(evidence.SampleDiff) < maxSamples {
			evidence.SampleDiff = append(evidence.SampleDiff, strings.TrimPrefix(detail, "内容が異なります: "))
		}
	}
	evidence.Siblings = capList(siblings, maxSiblingNames)
	return evidence
}

// EvidenceHash は証拠の安定したハッシュを返す。
// 同じ証拠に対する再問い合わせを避けるキャッシュキーと、監査記録に使う。
func EvidenceHash(evidence Evidence) string {
	data, err := json.Marshal(evidence)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:16])
}

// Siblings は同じ親ディレクトリにある ZIP／フォルダの名前を返す。
// AI に「兄弟に何があるか」という文脈だけを与える。
func Siblings(item model.Item, items []model.Item) []string {
	parent := strings.ToLower(item.ParentPath)
	seen := map[string]struct{}{}
	var names []string
	for _, other := range items {
		if other.ID == item.ID {
			continue
		}
		if strings.ToLower(other.ParentPath) != parent {
			continue
		}
		for _, candidate := range []string{baseName(other.ZipPath), baseName(other.FolderPath)} {
			if candidate == "" {
				continue
			}
			key := strings.ToLower(candidate)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			names = append(names, candidate)
		}
	}
	sort.Strings(names)
	return names
}

func baseName(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Base(path)
}

func capList(values []string, limit int) []string {
	if len(values) <= limit {
		return append([]string(nil), values...)
	}
	return append([]string(nil), values[:limit]...)
}

func round3(value float64) float64 {
	return float64(int(value*1000+0.5)) / 1000
}

// nameSimilarity は簡易的な類似度。文字の bigram の Dice 係数。
// 「photos」と「photos (1)」のような対応を数値化する。
func nameSimilarity(a, b string) float64 {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	ga := bigrams(a)
	gb := bigrams(b)
	if len(ga) == 0 || len(gb) == 0 {
		return 0
	}
	common := 0
	for gram, count := range ga {
		if other, ok := gb[gram]; ok {
			if count < other {
				common += count
			} else {
				common += other
			}
		}
	}
	return round3(float64(2*common) / float64(len(ga)+len(gb)))
}

func bigrams(value string) map[string]int {
	runes := []rune(value)
	if len(runes) < 2 {
		return map[string]int{value: 1}
	}
	result := make(map[string]int, len(runes)-1)
	for i := 0; i < len(runes)-1; i++ {
		result[string(runes[i:i+2])]++
	}
	return result
}
