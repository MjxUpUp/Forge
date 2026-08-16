package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/skillseval"
	"github.com/MjxUpUp/Forge/internal/skilltrigger"
	"github.com/spf13/cobra"
)

var (
	skUseTop          int
	skUseUndertrigger bool
	skUseJSON         bool
)

var skillsUsageCmd = &cobra.Command{
	Use:   "usage",
	Short: "使用度量分析（热门 skill + 从未触发的 undertrigger 候选）",
	Long:  `forge skills usage — 合并两路触达信号（toollog.jsonl 的主动 Skill 工具调用 + checklog.jsonl 的被动 skill-trigger 触发），与 canonical skill 集交叉，输出热门排名与从未触发列表；另输出被动触发漏斗（命中 → 送达 → 加载）与判定集漂移行（生产 canonical vs 仓库源 skills/ 的 triggers 声明对比）。两源均 agent-neutral，替代断链的 pi 旧源（~/.pi/research/skill-usage.jsonl）。被动触发是更大信号——skill-trigger 每个匹配事件都触发，而 Skill 工具只在显式加载时调用，故只数主动会低估触达。`,
	RunE:  runSkillsUsage,
}

// triggerDeclaredSkills 返回 dir 下带 triggers 声明（skilltrigger.LoadAll 引擎会装载）的
// skill 名集。cli 层职责——skillseval import skilltrigger 会成环（见 skillseval/drift.go）。
//
// triggerDeclaredSkills returns the set of skill names under dir carrying trigger
// declarations (loaded by the engine via skilltrigger.LoadAll). Lives in the cli layer —
// skillseval importing skilltrigger would cycle (see skillseval/drift.go).
func triggerDeclaredSkills(dir string) map[string]bool {
	set := map[string]bool{}
	for _, st := range skilltrigger.LoadAll(dir) {
		set[st.Skill] = true
	}
	return set
}

func runSkillsUsage(cmd *cobra.Command, args []string) error {
	proj, err := findProject()
	if err != nil {
		return err
	}
	canonical, _, err := resolveCanonical()
	if err != nil {
		return err
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
	//
	// Trigger-set drift: production canonical vs repo-source skills/. Compared automatically
	// when running inside a forge repo (dogfood/CI) — the 2026-08-16 15/33 stale cache (build
	// predating the trigger commit by 8h46m) took five manual directory steps to confirm;
	// one line exposes it here. Missing source dir → RepoCompared=false (outside a repo); a
	// present-but-zero-triggers dir is ALSO not comparable — that's usually a non-Forge project
	// that happens to carry a skills/ directory, and comparing would render a fake "consistent
	// with repo source" (review LOW-1).
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
	//
	// Drift line: always shows the production trigger-set size; upgrades to an explicit
	// warning (release-lag signal) when the repo side is comparable and has declarations
	// missing from production.
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
	//
	// Funnel section: hit → delivered → engaged. The answer to the 0/134 blind spot (the
	// trigger arm fired but nothing proved the injection was acted on) — the engaged column
	// counts a Read/Skill call on that skill within 10 minutes after the hit, same session
	// (attribution signal, not proof of following).
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

// repoTriggerSet 返回仓库源 skills/ 下带 triggers 声明的 skill 名集；nil = 不可比较。
// 两种不可比较：目录不存在（非 forge 仓库内运行），或目录存在但零个带 triggers 的 skill
// （多半是恰好有 skills/ 目录的非 Forge 项目——置成可比会渲染出假的「与仓库源一致」，
// 审查 LOW-1）。
//
// repoTriggerSet returns the set of skills under repo-source skills/ carrying trigger
// declarations; nil = not comparable. Two not-comparable cases: the dir is missing (running
// outside a forge repo), or the dir exists with zero trigger-declared skills (usually a
// non-Forge project that happens to carry skills/ — comparing would render a fake
// "consistent with repo source", review LOW-1).
func repoTriggerSet(gitRoot string) map[string]bool {
	repoDir := filepath.Join(gitRoot, "skills")
	st, err := os.Stat(repoDir)
	if err != nil || !st.IsDir() {
		return nil
	}
	if rs := triggerDeclaredSkills(repoDir); len(rs) > 0 {
		return rs
	}
	return nil
}

// ellipsisIf returns " ..." when more items were truncated, else "". Keeps the drift-line
// printf readable.
//
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
	skillsUsageCmd.Flags().BoolVar(&skUseJSON, "json", false, "JSON 输出")
	skillsCmd.AddCommand(skillsUsageCmd)
}
