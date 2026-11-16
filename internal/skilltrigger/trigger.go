// Package skilltrigger 解析 skill 的 triggers 声明。
//
// skill 在 SKILL.md 的 frontmatter metadata.triggers 里声明触发条件（关键词或命名
// condition）。LoadAll 扫描 canonical skills 目录，跳过未声明 triggers、解析失败
// 或被 deny 的 skill，供 CLI 与 skill 统计使用。
package skilltrigger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/skillsfm"
	"github.com/MjxUpUp/Forge/internal/util"
)

// Trigger 是 frontmatter metadata.triggers JSON 数组中的一项。
type Trigger struct {
	Event    string   `json:"event"`              // 事件名，如 UserPromptSubmit
	Keywords []string `json:"keywords,omitempty"` // 子串不区分大小写；与 When 为 AND
	When     string   `json:"when,omitempty"`     // 命名 condition
	Match    string   `json:"match,omitempty"`    // tool_name matcher（| 分隔）
	Reason   string   `json:"reason,omitempty"`   // 注入理由（覆盖默认模板）
	Cooldown int      `json:"cooldown,omitempty"` // per-session per-skill 冷却秒数
	Inline   string   `json:"inline,omitempty"`   // 一行动作指令（动作点事件使用）
	Follow   string   `json:"follow,omitempty"`   // 判定「跟随了 Inline 指令」的正则
}

// SkillTriggers 是一个 skill 的全部 triggers。
type SkillTriggers struct {
	Skill    string // skill 名（frontmatter.name，fallback 目录名）
	SkillDir string // canonical/<skill> 绝对路径
	Triggers []Trigger
}

// DeniedSkills 是有专用驱动、不走 triggers 的 skill：LoadAll 跳过它们，避免重复装载。
var DeniedSkills = map[string]bool{
	"code-review-gate": true,
	"skill-routing":    true,
}

// ParseTriggers 解析 JSON 字符串 → []Trigger；空串/非法 JSON 返 nil（不阻塞框架）。
func ParseTriggers(raw string) []Trigger {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var ts []Trigger
	if err := json.Unmarshal([]byte(raw), &ts); err != nil {
		return nil
	}
	return ts
}

// LoadAll 扫描 canonicalDir 下所有 SKILL.md，解析 triggers。无 triggers / 解析失败 / 被
// deny 的 skill 一律跳过，绝不阻塞框架。
func LoadAll(canonicalDir string) []SkillTriggers {
	if canonicalDir == "" {
		return nil
	}
	entries, err := os.ReadDir(canonicalDir)
	if err != nil {
		return nil
	}
	var out []SkillTriggers
	for _, e := range entries {
		if !util.DirEntryIsDir(canonicalDir, e) {
			continue
		}
		name := e.Name()
		if DeniedSkills[name] {
			continue
		}
		skillMD := filepath.Join(canonicalDir, name, "SKILL.md")
		data, err := os.ReadFile(skillMD)
		if err != nil {
			continue
		}
		fm := skillsfm.Parse(data)
		raw := fm.Metadata["triggers"]
		triggers := ParseTriggers(raw)
		trimmed := strings.TrimSpace(raw)
		if trimmed != "" && trimmed != "[]" && len(triggers) == 0 {
			// 非空、非合法空数组（"[]" 是 skill 明确声明「无 trigger」）但解析失败 =
			// 非法 JSON；stderr 警告让运行时也可见，否则 skill 作者不知 triggers 静默失效。
			fmt.Fprintf(os.Stderr, "[skill-trigger] warning: %s/SKILL.md metadata.triggers 非法 JSON，已跳过（forge skills audit R12 报详情）\n", name)
		}
		if len(triggers) == 0 {
			continue
		}
		skillName := fm.Name
		if skillName == "" {
			skillName = name
		}
		out = append(out, SkillTriggers{
			Skill:    skillName,
			SkillDir: filepath.Join(canonicalDir, name),
			Triggers: triggers,
		})
	}
	return out
}
