package skillseval

// mine.go — 生产触发记录 → golden case 草稿挖矿（skill-trigger v2 / 辩论 P2）。
//
// 定位（对抗辩论 R3/R4 收窄后的边界）：
//   - 只解决 **precision 侧** golden 供给——"真实触发过但 engaged=false" 的输入是最真实
//     的 not-trigger（near-miss）负例候选；"触发且 engaged=true" 是正例候选。recall 侧
//     （该触发没触发）在命中日志中结构性不存在（没发生的事件没有日志，信息论约束），
//     明示不解决——那条线走 transcript 取证与人工构造（与官方 skill-creator 同路）。
//   - 挖矿原料 = Entry.Meta 的 prompt_hash（去重键，prompt 或 tool 事件命中来源文本的
//     项目盐哈希——stdout 命中同样可挖，review m4）+ excerpt（opt-in 采集的真实输入
//     脱敏摘录，FORGE_TRIGGER_EXCERPT=1）。无摘录数据时挖矿如实报告零可用——不降级、
//     不假装。
//   - 草稿永不自动进 golden：机械改写+脱敏（SanitizeDraft，无条件执行）是把「真实话语改写」从注释约定升为
//     机械步骤（R4），但人工策展仍在环上（half-automatic，与 skillseval 定位一致）。
//   - golden 集退出机制（辩论 G2，防 AWM 式 ever-growing）：GoldenCap 告警——策展合并
//     时超上限必须先淘汰旧 case；本命令对超限 golden 集发 advisory，淘汰本身由策展
//     流程（skill-evolution skill）执行。

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/toolusage"
	"github.com/MjxUpUp/Forge/internal/util"
)

// MaxMinedPerSkill per-skill draft cap per mine run (same-source G2 constraint: mining output is itself bounded — an unbounded draft list would drown curation).
//
// MaxMinedPerSkill 每次 mine 每 skill 的草稿上限（G2 同源约束：挖矿产出本身有界，
// 防一次跑出无界草稿清单淹没策展）。
const MaxMinedPerSkill = 20

// GoldenCap advisory threshold for golden sets.
//
// GoldenCap golden 集上限告警阈值。超限的 golden 集是 ever-growing 信号——合并新草稿
// 前必须先淘汰（策展流程执行；本常量是 advisory 的判定线，不是硬门禁）。
const GoldenCap = 100

// MinedCase is one mined golden-draft candidate (redacted excerpt + attribution metadata).
//
// MinedCase 是一条挖出的 golden 草稿候选（脱敏摘录 + 归因元数据）。
type MinedCase struct {
	Skill      string    `json:"skill"`
	PromptHash string    `json:"prompt_hash"`
	Excerpt    string    `json:"excerpt"`
	Kind       string    `json:"kind"` // KindTrigger（engaged=true）| KindNotTrigger（engaged=false）
	SessionID  string    `json:"session_id,omitempty"`
	HitAt      time.Time `json:"hit_at"`
}

// MineReport is one mining run's output and honesty signals.
//
// MineReport 是一次挖矿的产出与诚实性信号。
type MineReport struct {
	Skills map[string][]MinedCase `json:"skills"`
	// TotalHits / WithExcerpt / Deduped — funnel-style raw-material stats.
	//
	// TotalHits / WithExcerpt / Deduped — 漏斗式的原料统计：TotalHits 是窗口内全部
	// CheckSkillTrigger 命中；WithExcerpt 其中带 opt-in 摘录的；Deduped 是 prompt_hash
	// 去重后的。三者落差（尤其 TotalHits≫WithExcerpt）直说「摘录没开」，不藏在零结果里。
	TotalHits   int `json:"total_hits"`
	WithExcerpt int `json:"with_excerpt"`
	Deduped     int `json:"deduped"`
}

// MineGoldenDrafts mines golden drafts from checklog (CheckSkillTrigger + Meta excerpt/prompt_hash) × toollog (engagement via funnel.go's engagedAfter — single judgment source).
//
// MineGoldenDrafts 从 checklog（CheckSkillTrigger + Meta excerpt/prompt_hash）× toollog
// （engaged 判定，复用 funnel.go 的 engagedAfter——单一判定真相源）挖 golden 草稿。
// 纯函数（可测）；skill 为空串 = 全部 skill。stop-cap warn advisory 无 " hit (" 标记，
// SkillFromTriggerDetail 返回 ""，天然被跳过——不会混进草稿。
func MineGoldenDrafts(entries []checklog.Entry, calls []toolusage.ToolCall, skill string) *MineReport {
	rep := &MineReport{Skills: map[string][]MinedCase{}}
	type key struct{ skill, hash string }
	dedup := map[key]MinedCase{}
	for _, e := range entries {
		if e.Check != checklog.CheckSkillTrigger {
			continue
		}
		name := checklog.SkillFromTriggerDetail(e.Detail)
		if name == "" || (skill != "" && name != skill) {
			continue
		}
		rep.TotalHits++
		hash, excerpt := e.Meta[checklog.MetaKeyPromptHash], e.Meta[checklog.MetaKeyExcerpt]
		if hash == "" || excerpt == "" {
			continue
		}
		rep.WithExcerpt++
		k := key{name, hash}
		// 同 hash 取最新一条（摘录与 engaged 判定用最新命中的时点）：仅当已有条目
		// 比本条新（或同时）时跳过——条件曾写反（!prev.HitAt.After），旧条目永远赢，
		// TestMineGoldenDrafts_DedupByHash 钉住。
		if prev, ok := dedup[k]; ok && !e.RecordedAt.After(prev.HitAt) {
			continue
		}
		kind := KindNotTrigger
		if engagedAfter(calls, e.SessionID, name, e.RecordedAt) {
			kind = KindTrigger
		}
		dedup[k] = MinedCase{
			Skill: name, PromptHash: hash, Excerpt: excerpt,
			Kind: kind, SessionID: e.SessionID, HitAt: e.RecordedAt,
		}
	}
	for _, mc := range dedup {
		rep.Skills[mc.Skill] = append(rep.Skills[mc.Skill], mc)
	}
	for name := range rep.Skills {
		cases := rep.Skills[name]
		slices.SortFunc(cases, func(a, b MinedCase) int {
			if c := cmp.Compare(b.HitAt.Unix(), a.HitAt.Unix()); c != 0 {
				return c // 新的在前（策展优先看最近的生产形态）
			}
			return cmp.Compare(a.PromptHash, b.PromptHash)
		})
		if len(cases) > MaxMinedPerSkill {
			cases = cases[:MaxMinedPerSkill]
		}
		rep.Skills[name] = cases
		// Deduped 在截断**后**按落盘草稿计数（review n2：截断前计数会让漏斗数字
		// 大于实际产物，统计与产物不一致）。
		rep.Deduped += len(cases)
	}
	return rep
}

// SanitizeDraft applies second-layer defense to mined draft text: secret redaction (already redacted once at capture — defense in depth against pattern drift) and absolute home-path folding to ~ (no usernames in drafts).
//
// SanitizeDraft 对挖出的草稿文本做二次防御：secret 脱敏（采集时已脱敏过一次——defense
// in depth，防 pattern 演进错位）+ 绝对 home 路径折叠为 ~（用户名不进草稿）。
// 返回改写后的草稿（机械 sanitize 的实现；人工改写仍在环上）。
func SanitizeDraft(excerpt, homeDir string) string {
	out := util.RedactSecrets(excerpt)
	if homeDir != "" {
		out = strings.ReplaceAll(out, homeDir, "~")
	}
	return out
}
