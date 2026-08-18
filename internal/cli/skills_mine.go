package cli

// skills_mine.go — `forge skills mine`：把生产触发记录挖成 golden case 草稿
//（skill-trigger v2 / 辩论 P2 的 CLI 入口）。
//
// 输出边界（half-automatic）：草稿写 <eval-dir>/mined/<skill>.draft.json，**永不**自动
// 进 golden（evals/golden/）。策展流程（skill-evolution skill）负责改写降噪 + 合并 +
// 超限淘汰；本命令对超 GoldenCap 的 golden 集发 advisory（G2 退出机制的告警端）。
//
// skills_mine.go — `forge skills mine`: mine production trigger records into golden-case
// drafts (the CLI entry of skill-trigger v2 / debate P2).
//
// Output boundary (half-automatic): drafts go to <eval-dir>/mined/<skill>.draft.json and
// NEVER auto-enter golden (evals/golden/). The curation flow (skill-evolution skill)
// handles rewrite-noise-reduction + merge + over-cap eviction; this command raises the
// advisory for golden sets over GoldenCap (the alarm end of the G2 exit mechanism).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/skillseval"
	"github.com/MjxUpUp/Forge/internal/toolusage"
	"github.com/MjxUpUp/Forge/internal/util"
	"github.com/spf13/cobra"
)

var skillsMineSkill string

var skillsMineCmd = &cobra.Command{
	Use:   "mine",
	Short: "把生产触发记录挖成 golden case 草稿（precision 侧；需 FORGE_TRIGGER_EXCERPT=1 的摘录数据）",
	Long: "把 checklog 里的 CheckSkillTrigger 记录（带 opt-in 摘录）× toollog engaged 判定挖成 golden case 草稿：\n" +
		"  - engaged=true 的命中 → trigger 正例候选；engaged=false → not-trigger（near-miss）负例候选\n" +
		"  - 按 prompt_hash 跨 session 去重，每 skill 至多 " + strconv.Itoa(skillseval.MaxMinedPerSkill) + " 条，草稿写 <eval-dir>/mined/<skill>.draft.json\n" +
		"  - 草稿不自动进 golden：人工改写（草稿已过机械脱敏，改写仍在环上）后走 evals/golden/ 策展\n" +
		"  - recall 侧（该触发没触发）不在命中日志里，本命令明示不解决\n\n" +
		"Mine CheckSkillTrigger records (with opt-in excerpts) × toollog engagement into golden-case drafts:\n" +
		"  - engaged=true hits → trigger positives; engaged=false → not-trigger (near-miss) negatives\n" +
		"  - deduped by prompt_hash across sessions, at most " + strconv.Itoa(skillseval.MaxMinedPerSkill) + " per skill; drafts land in <eval-dir>/mined/<skill>.draft.json\n" +
		"  - drafts NEVER auto-enter golden: rewrite by hand (drafts already passed mechanical redaction) then curate into evals/golden/\n" +
		"  - the recall side (should-have-fired) is not in a hit log — explicitly not solved here",
	RunE: runSkillsMine,
}

func init() {
	skillsMineCmd.Flags().StringVar(&skillsMineSkill, "skill", "", "只挖该 skill（空 = 全部）")
	skillsCmd.AddCommand(skillsMineCmd)
}

// runSkillsMine 执行挖矿：load（checklog×toollog）→ MineGoldenDrafts → 草稿落盘 +
// 原料漏斗打印 + golden 超限 advisory。
//
// runSkillsMine does the mining: load (checklog×toollog) → MineGoldenDrafts → draft
// persistence + raw-material funnel print + golden over-cap advisory.
func runSkillsMine(cmd *cobra.Command, args []string) error {
	root, _ := findProjectRoot()
	entries, err := checklog.LoadAllAll(root)
	if err != nil {
		return fmt.Errorf("checklog 读取失败: %w", err)
	}
	calls, err := toolusage.LoadAllAll(root)
	if err != nil {
		return fmt.Errorf("toollog 读取失败: %w", err)
	}
	rep := skillseval.MineGoldenDrafts(entries, calls, skillsMineSkill)

	// 原料漏斗：落差直说「摘录没开」，不藏在零结果里（诚实性信号）。
	if rep.TotalHits == 0 {
		fmt.Println("窗口内无 CheckSkillTrigger 命中记录——无原料可挖。")
		return nil
	}
	fmt.Printf("原料漏斗：命中 %d → 带摘录 %d → 去重后 %d\n", rep.TotalHits, rep.WithExcerpt, rep.Deduped)
	if rep.WithExcerpt == 0 {
		fmt.Println("全部命中无 opt-in 摘录（FORGE_TRIGGER_EXCERPT 未开）——hash 只够聚类不够改写成 case。")
		fmt.Println("开启采集：export FORGE_TRIGGER_EXCERPT=1（摘录默认关是隐私折中，见 checklog.MetaKeyExcerpt 注释）。")
		return nil
	}

	dir, err := skillseval.ResolveDir("")
	if err != nil {
		return fmt.Errorf("eval 目录解析失败: %w", err)
	}
	home, _ := os.UserHomeDir()
	names := make([]string, 0, len(rep.Skills))
	for name := range rep.Skills {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		cases := rep.Skills[name]
		if len(cases) == 0 {
			continue
		}
		// 机械 sanitize（二次脱敏 + home 折叠）；人工改写仍在环上。
		for i := range cases {
			cases[i].Excerpt = skillseval.SanitizeDraft(cases[i].Excerpt, home)
		}
		outDir := filepath.Join(dir, "mined")
		if err := os.MkdirAll(outDir, 0755); err != nil {
			return err
		}
		out := filepath.Join(outDir, name+".draft.json")
		data, _ := json.MarshalIndent(cases, "", "  ")
		if err := util.AtomicWrite(out, data, 0644); err != nil {
			return fmt.Errorf("草稿写入失败: %w", err)
		}
		pos, neg := 0, 0
		for _, c := range cases {
			if c.Kind == skillseval.KindTrigger {
				pos++
			} else {
				neg++
			}
		}
		fmt.Printf("  %s: %d 条草稿（正例 %d / near-miss 负例 %d）→ %s\n", name, len(cases), pos, neg, out)

		// G2 退出机制告警端：golden 集超限时，合并前必须先淘汰。
		if existing, err := skillseval.LoadCases(dir, name); err == nil {
			if golden := countGolden(existing); golden > skillseval.GoldenCap {
				fmt.Printf("  ADVISORY: %s 的 golden 集已达 %d 条（上限 %d）——ever-growing 信号，合并新草稿前先淘汰旧 case（策展流程执行）\n",
					name, golden, skillseval.GoldenCap)
			}
		}
	}
	fmt.Println("草稿是半成品：人工改写降噪（去项目私有术语/合并同类）后进 evals/golden/<skill>/cases.json（Origin=curated，g- 前缀）。")
	return nil
}

// countGolden 统计 golden（curated）case 数。
func countGolden(cases []skillseval.EvalCase) int {
	n := 0
	for _, c := range cases {
		if c.Origin == skillseval.OriginCurated {
			n++
		}
	}
	return n
}
