// Package skillseval 提供 skill 使用度量分析（analyze-usage）与 eval 清单生成（skill-eval）。
// 弱依赖、独立：usage 读 skill-usage.jsonl 做 undertrigger 分析；eval 从 description 生成测试 prompt。
package skillseval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MjxUpUp/Forge/internal/skillsdist"
)

// DefaultUsageLog 返回 ~/.forge/research/skill-usage.jsonl（对齐 analyze-usage.py USAGE_LOG）。
func DefaultUsageLog() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".pi", "research", "skill-usage.jsonl"), nil
}

// LoadUsage 读 skill-usage.jsonl，返回 skill→加载次数 与总事件数（对齐 analyze-usage.py load_usage）。
// 仅计 type=="skill-load" 且 skill 非空的行；坏 JSON 行跳过；日志不存在视为空。
func LoadUsage(logPath string) (map[string]int, int, error) {
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]int{}, 0, nil
		}
		return nil, 0, err
	}
	counts := map[string]int{}
	total := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e struct {
			Type  string `json:"type"`
			Skill string `json:"skill"`
		}
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		if e.Type == "skill-load" && e.Skill != "" {
			counts[e.Skill]++
			total++
		}
	}
	return counts, total, nil
}

// SkillCount 是单个 skill 的加载次数。
type SkillCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// UsageReport 是使用度量分析结果（对齐 analyze-usage.py JSON 输出）。
type UsageReport struct {
	TotalEvents    int          `json:"total_events"`
	TotalSkills    int          `json:"total_skills"`
	UsedSkills     int          `json:"used_skills"`
	NeverTriggered []string     `json:"never_triggered"`
	HotSkills      []SkillCount `json:"hot_skills"`
}

// AnalyzeUsage 交叉 usage 日志与 canonical skill 集，产出 undertrigger 分析（对齐 analyze-usage.py main）。
func AnalyzeUsage(canonical, logPath string) (*UsageReport, error) {
	counts, total, err := LoadUsage(logPath)
	if err != nil {
		return nil, err
	}
	all, err := skillsdist.ListSkills(canonical)
	if err != nil {
		return nil, err
	}

	never := []string{}
	for _, n := range all {
		if counts[n] == 0 {
			never = append(never, n)
		}
	}
	sort.Strings(never)

	// canonical 集：HotSkills/UsedSkills 只计 canonical 中存在的 skill，过滤日志里的
	// "幽灵技能"（canonical 已删但日志残留）——与 NeverTriggered（仅 canonical）对称，
	// 否则 used_skills 可能 > total_skills，hot_skills 里混入已不存在的名字。
	canonicalSet := make(map[string]bool, len(all))
	for _, n := range all {
		canonicalSet[n] = true
	}
	hot := make([]SkillCount, 0, len(counts))
	used := 0
	for name, cnt := range counts {
		if !canonicalSet[name] {
			continue
		}
		hot = append(hot, SkillCount{Name: name, Count: cnt})
		used++
	}
	sort.Slice(hot, func(i, j int) bool {
		if hot[i].Count != hot[j].Count {
			return hot[i].Count > hot[j].Count
		}
		return hot[i].Name < hot[j].Name
	})
	if len(hot) > 10 {
		hot = hot[:10]
	}

	return &UsageReport{
		TotalEvents:    total,
		TotalSkills:    len(all),
		UsedSkills:     used,
		NeverTriggered: never,
		HotSkills:      hot,
	}, nil
}
