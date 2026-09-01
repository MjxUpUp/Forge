package cliskills

import (
	"encoding/json"
	"fmt"
	"github.com/MjxUpUp/Forge/internal/projectroot"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/skillseval"
	"github.com/MjxUpUp/Forge/internal/skilltrigger"
	"github.com/MjxUpUp/Forge/internal/toolusage"
	"github.com/spf13/cobra"
)

var (
	skUseTop          int
	skUseUndertrigger bool
	skUseJSON         bool
	skUseByKeyword    bool
)

var skillsUsageCmd = &cobra.Command{
	Use:   "usage",
	Short: "使用度量分析（热门 skill + 从未触发的 undertrigger 候选）",
	Long:  `forge skills usage — 合并两路触达信号（toollog.jsonl 的主动 Skill 工具调用 + checklog.jsonl 的被动 skill-trigger 触发），与 canonical skill 集交叉，输出热门排名与从未触发列表；另输出被动触发漏斗（命中 → 送达 → 加载）与判定集漂移行（生产 canonical vs 仓库源 skills/ 的 triggers 声明对比）。两源均 agent-neutral，替代断链的 pi 旧源（~/.pi/research/skill-usage.jsonl）。被动触发是更大信号——skill-trigger 每个匹配事件都触发，而 Skill 工具只在显式加载时调用，故只数主动会低估触达。`,
	RunE:  runSkillsUsage,
}

// triggerDeclaredSkills 返回 dir 下带 triggers 声明（skilltrigger.LoadAll 引擎会装载）的
// skill 名集。cli 层职责——skillseval import skilltrigger 会成环（见 skillseval/drift.go）。
func triggerDeclaredSkills(dir string) map[string]bool {
	set := map[string]bool{}
	for _, st := range skilltrigger.LoadAll(dir) {
		set[st.Skill] = true
	}
	return set
}

func runSkillsUsage(cmd *cobra.Command, args []string) error {
	proj, err := projectroot.FindProject()
	if err != nil {
		return err
	}
	canonical, _, err := ResolveCanonical()
	if err != nil {
		return err
	}
	// --by-keyword 分支在最前（review m3/m4）：不需要 usage/funnel 报告——放后面会被
	// --json/--undertrigger 静默吞掉，且 AnalyzeUsageWithFunnel 的双份 LoadAllAll 白跑、
	// 其失败还会连带 by-keyword 不可用。--json 同样支持（KeywordReport 序列化出口）。
	if skUseByKeyword {
		return runSkillsUsageByKeyword(proj.GitRoot, canonical)
	}
	rep, err := skillseval.AnalyzeUsageWithFunnel(proj.GitRoot, canonical)
	if err != nil {
		return err
	}
	// 判定集漂移：生产 canonical vs 仓库源 skills/。在 forge 仓库内运行时（dogfood/CI）
	// 自动对比——2026-08-16 的 15/33 stale cache（build 早于 triggers 提交 8h46m）靠人工
	// 翻目录五步确认，此处一行暴露。源目录不存在 → RepoCompared=false（非仓库内运行）；
	// 目录存在但零个带 triggers 的 skill 同样不可比较——那多半是恰好有 skills/ 目录的
	// 非 Forge 项目，置成可比会渲染出假的「与仓库源一致」（审查 LOW-1）。
	repoSet := repoTriggerSet(proj.GitRoot)
	drift := skillseval.CompareTriggerSets(triggerDeclaredSkills(canonical), repoSet)
	rep.Drift = &drift

	if skUseJSON {
		b, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(b))
		return nil
	}

	if skUseUndertrigger {
		fmt.Printf("=== 从未触发的 skill（%d/%d）— undertrigger 候选 ===\n", len(rep.NeverTriggered), rep.TotalSkills)
		for _, s := range rep.NeverTriggered {
			fmt.Printf("  %s\n", s)
		}
		return nil
	}

	fmt.Printf("Skill 使用度量  (源: toollog 主动调用 + checklog 被动触发 · agent-neutral)\n")
	fmt.Printf("总 skill 触达: %d  |  canonical skill 数: %d  |  被用过: %d\n", rep.TotalEvents, rep.TotalSkills, rep.UsedSkills)

	// 漂移行：总是给生产判定集规模；仓库可比较且缺声明时升级为显式警告（发版滞后信号）。
	if rep.Drift != nil {
		if rep.Drift.RepoCompared {
			if rep.Drift.HasDrift() {
				names := rep.Drift.MissingInProd
				preview := names
				if len(preview) > 6 {
					preview = preview[:6]
				}
				fmt.Printf("⚠ 判定集漂移: 生产 %d skills 带 triggers | 仓库源 %d → %d 个触发声明未进生产（发版滞后）: %s%s\n",
					rep.Drift.ProdDeclared, rep.Drift.RepoDeclared, len(names),
					strings.Join(preview, ", "), ellipsisIf(len(names) > len(preview)))
			} else {
				fmt.Printf("判定集: 生产 %d skills 带 triggers（与仓库源一致）\n", rep.Drift.ProdDeclared)
			}
		} else {
			fmt.Printf("判定集: 生产 %d skills 带 triggers（仓库源不可比较）\n", rep.Drift.ProdDeclared)
		}
	}
	fmt.Println()

	top := rep.HotSkills
	if skUseTop > 0 && skUseTop < len(top) {
		top = top[:skUseTop]
	}
	fmt.Printf("=== 热门 skill Top %d ===\n", len(top))
	for _, h := range top {
		bar := strings.Repeat("█", min(h.Count, 30))
		fmt.Printf("  %-32s %3d %s\n", h.Name, h.Count, bar)
	}

	// 漏斗段：命中 → 送达 → 加载。0/134 盲区（触发臂转了但无法证明注入被照做）的答案——
	// 加载列 = 命中后 10 分钟内同 session 出现对该 skill 的 Read/Skill 调用（归因信号，
	// 非遵循证明）。
	if rep.Funnel != nil && len(rep.Funnel.Skills) > 0 {
		fmt.Printf("\n=== 被动触发漏斗（命中 → 送达 → %dmin 内加载）===\n", int(rep.Funnel.Window.Minutes()))
		fmt.Printf("  %-32s %4s %4s %4s %4s\n", "skill", "命中", "送达", "未知", "加载")
		for _, f := range rep.Funnel.Skills {
			fmt.Printf("  %-32s %4d %4d %4d %4d\n", f.Name, f.Hits, f.Delivered, f.DeliveryUnknown, f.Engaged)
		}
		fmt.Printf("  注: 未知=送达章引入前的旧条目; 加载=命中后同 session Read SKILL.md / Skill 调用（归因信号，非遵循证明）\n")
	}

	fmt.Printf("\n=== 从未触发（%d/%d）===\n", len(rep.NeverTriggered), rep.TotalSkills)
	limit := 15
	for i, s := range rep.NeverTriggered {
		if i >= limit {
			fmt.Printf("  ... 还有 %d 个\n", len(rep.NeverTriggered)-limit)
			break
		}
		fmt.Printf("  %s\n", s)
	}
	return nil
}

// runSkillsUsageByKeyword 渲染 per-keyword 触发分析（P0.5）：命中/engaged/抑制切片 +
// 死关键词。宿主偏差标注强制随行（G3）——engaged 信号只在产生工具事件的宿主可见，
// codex/cursor 等注入型宿主天然零信号，裸报"低遵循率"会把宿主能力差误读为关键词噪声。
func runSkillsUsageByKeyword(root, canonical string) error {
	entries, err := checklog.LoadAllAll(root)
	if err != nil {
		return fmt.Errorf("checklog 读取失败: %w", err)
	}
	calls, err := toolusage.LoadAllAll(root)
	if err != nil {
		return fmt.Errorf("toollog 读取失败: %w", err)
	}
	// 声明关键词集：skill → 该 skill 全部 triggers 声明的关键词并集（review n2：seen
	// 预建到 skill 层）。
	declared := map[string][]string{}
	seen := map[string]map[string]bool{}
	for _, st := range skilltrigger.LoadAll(canonical) {
		if seen[st.Skill] == nil {
			seen[st.Skill] = map[string]bool{}
		}
		for _, tr := range st.Triggers {
			for _, kw := range tr.Keywords {
				kw = strings.TrimSpace(kw)
				if kw == "" || seen[st.Skill][kw] {
					continue
				}
				seen[st.Skill][kw] = true
				declared[st.Skill] = append(declared[st.Skill], kw)
			}
		}
	}
	rep := skillseval.AnalyzeKeywords(entries, calls, declared)

	if skUseJSON {
		b, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(b))
		return nil
	}

	fmt.Printf("Per-keyword 触发分析  (源: checklog CheckSkillTrigger × toollog engaged · v2 Meta)\n")
	fmt.Printf("窗口内命中总数: %d（其中 v2 证据条目 %d）", rep.TotalHits, rep.V2Hits)
	if rep.TotalHits == 0 {
		fmt.Println("\n（无命中记录——窗口内零命中）")
		return nil
	}
	fmt.Println()
	if len(rep.Stats) > 0 {
		fmt.Printf("\n=== 关键词切片（命中降序）===\n")
		fmt.Printf("  %-28s %-20s %4s %4s %4s\n", "skill", "keyword", "命中", "加载", "抑制")
		for _, st := range rep.Stats {
			fmt.Printf("  %-28s %-20s %4d %4d %4d\n", st.Skill, st.Keyword, st.Hits, st.Engaged, st.Suppressed)
		}
	}
	if total := condTotal(rep.ConditionOnly); total > 0 {
		fmt.Printf("\n  condition/legacy 触发: %d 次（无 matched_keyword——命名 condition 或 v2 前旧条目，按 skill: %s）\n",
			total, condSkills(rep.ConditionOnly))
	}
	if len(rep.DeadKeywords) > 0 {
		limit := 20
		fmt.Printf("\n=== 死关键词（声明 %d 个从未命中——关键词表删除候选）===\n", len(rep.DeadKeywords))
		for i, d := range rep.DeadKeywords {
			if i >= limit {
				fmt.Printf("  ... 还有 %d 个\n", len(rep.DeadKeywords)-limit)
				break
			}
			fmt.Printf("  %-28s %s\n", d.Skill, d.Keyword)
		}
	} else if rep.V2Hits < rep.TotalHits {
		fmt.Printf("\n（死关键词检测停用：窗口混有 %d 条 v1 条目——归因上线前的\"零命中\"不构成删除依据，需全窗口 v2 证据）\n", rep.TotalHits-rep.V2Hits)
	}
	fmt.Printf("\n注: 加载=命中后 10min 内同 session Read/Skill 调用（per-hit 归因，无漏斗去重）。宿主偏差: 该信号仅在产生工具事件的宿主（如 Claude Code）可见，codex/cursor 等注入型宿主天然零信号——低加载率可能反映宿主形态而非关键词噪声。抑制列按 skill 累计、挂在触发行（跨词比较会错挂，按 skill 汇总读）。\n")
	return nil
}

// condTotal/condSkills 汇总 condition-only 行（per-skill）。
func condTotal(rows []skillseval.KeywordStat) int {
	n := 0
	for _, r := range rows {
		n += r.Hits
	}
	return n
}

func condSkills(rows []skillseval.KeywordStat) string {
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, fmt.Sprintf("%s×%d", r.Skill, r.Hits))
	}
	return strings.Join(names, ", ")
}

// repoTriggerSet 返回仓库源 skills/ 下带 triggers 声明的 skill 名集；nil = 不可比较。
// 两种不可比较：目录不存在（非 forge 仓库内运行），或目录存在但零个带 triggers 的 skill
// （多半是恰好有 skills/ 目录的非 Forge 项目——置成可比会渲染出假的「与仓库源一致」，
// 审查 LOW-1）。
func repoTriggerSet(gitRoot string) map[string]bool {
	repoDir := filepath.Join(gitRoot, "skills")
	st, err := os.Stat(repoDir)
	if err != nil || !st.IsDir() {
		return nil
	}
	out := triggerDeclaredSkills(repoDir)
	// skills-forge/（2026-08 迁移后的 forge 原生覆盖层）有自己的 triggers 声明——
	// 不扫它的话这些 skill 会渲染出假漂移行（生产 canonical 有、仓库源集合没有）。
	forgeDir := filepath.Join(gitRoot, "skills-forge")
	if fst, ferr := os.Stat(forgeDir); ferr == nil && fst.IsDir() {
		for name := range triggerDeclaredSkills(forgeDir) {
			out[name] = true
		}
	}
	if len(out) > 0 {
		return out
	}
	return nil
}

// ellipsisIf 在有截断时返回 " ..."，否则返回 ""。让漂移行的 printf 保持可读。
func ellipsisIf(more bool) string {
	if more {
		return " ..."
	}
	return ""
}

func init() {
	skillsUsageCmd.Flags().IntVar(&skUseTop, "top", 10, "热门 skill 显示数量")
	skillsUsageCmd.Flags().BoolVar(&skUseUndertrigger, "undertrigger", false, "只看从未触发的 skill")
	skillsUsageCmd.Flags().BoolVar(&skUseByKeyword, "by-keyword", false, "per-keyword 触发分析（命中/engaged/抑制切片 + 死关键词）")
	skillsUsageCmd.Flags().BoolVar(&skUseJSON, "json", false, "JSON 输出")
	Root.AddCommand(skillsUsageCmd)
}
