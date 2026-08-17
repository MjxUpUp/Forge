package cli

// skills_mutex_record.go — mutex-record subcommand: writes the agent-batched mutex
// results as a single MutexRun. Mirrors eval-record's flow (normalize + judge + append)
// but against the mutex case set: pass iff actual == Positive (JudgeMutexCase is the
// single source of judgment).
//
// skills_mutex_record.go — mutex-record 子命令：把 agent 整批回填的互斥结果写成一条
// MutexRun。流程对齐 eval-record（归一化 + 判定 + append），但针对互斥 case 集：
// actual == Positive 才 pass（JudgeMutexCase 是判定单一真相源）。

import (
	"encoding/json"
	"fmt"

	"github.com/MjxUpUp/Forge/internal/skillsdist"
	"github.com/MjxUpUp/Forge/internal/skillseval"
	"github.com/spf13/cobra"
)

var (
	skMRecFrom  string
	skMRecModel string
	skMRecVer   string
)

var skillsMutexRecordCmd = &cobra.Command{
	Use:   "mutex-record",
	Short: "回填一次互斥集 run 结果（agent dispatch 跑完互斥 prompt 后整批提交）",
	Long: `把 agent dispatch 子代理跑出的互斥集整批结果写成一个 MutexRun。

--from 指向一个 JSON 数组文件，或 "-" / 省略从 stdin 读。格式：
  [{"case_id":"...","actual_triggered":"<skill 名 | none>"}]

forge 归一化 actual（trim+lowercase+canonical 精确匹配）、按 actual==Positive 判定、
append 到 mutex/runs.jsonl。未知 case_id 跳过；全部未知则显式报错（重跑 mutex-gen
后需重新拉 case 集）。`,
	RunE: runSkillsMutexRecord,
}

func runSkillsMutexRecord(cmd *cobra.Command, args []string) error {
	canonical, _, err := resolveCanonical()
	if err != nil {
		return err
	}
	dir, err := evalDataDir()
	if err != nil {
		return err
	}
	cases, err := skillseval.LoadMutexCases(dir)
	if err != nil {
		return err
	}
	if len(cases) == 0 {
		return fmt.Errorf("无互斥 case 集——先跑 'forge skills mutex-gen'")
	}

	data, err := readFromArg(skMRecFrom)
	if err != nil {
		return fmt.Errorf("read results: %w", err)
	}
	var raw []skillseval.SubmitResult
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse results JSON（应为 [{case_id,actual_triggered}]）: %w", err)
	}
	if len(raw) == 0 {
		return fmt.Errorf("results 为空")
	}

	// ListSkills failure is propagated, not swallowed (same rationale as SubmitRun: a
	// silently degraded canonical set would skew NormalizeTriggered for every case).
	//
	// ListSkills 失败要传播、不吞掉（理由同 SubmitRun：静默降级的 canonical 集会让
	// 所有 case 的 NormalizeTriggered 失真）。
	canonicalSkills, err := skillsdist.ListSkills(canonical)
	if err != nil {
		return fmt.Errorf("list canonical skills: %w", err)
	}

	ver := skMRecVer
	if ver == "" {
		ver = rootCmd.Version
	}
	run, err := skillseval.RecordMutexRun(dir, cases, canonicalSkills, skMRecModel, ver, raw)
	if err != nil {
		return err
	}
	passed := 0
	for _, r := range run.Results {
		if r.Pass {
			passed++
		}
	}
	fmt.Printf("✅ mutex run %s recorded: %d/%d passed\n", run.RunID, passed, len(run.Results))
	return nil
}

func init() {
	skillsMutexRecordCmd.Flags().StringVar(&skMRecFrom, "from", "-", "结果 JSON 文件路径（- 或省略 = stdin）")
	skillsMutexRecordCmd.Flags().StringVar(&skMRecModel, "agent-model", "", "跑此 run 的 agent 模型（防跨模型假回归）")
	skillsMutexRecordCmd.Flags().StringVar(&skMRecVer, "forge-version", "", "forge 版本（默认取二进制版本）")
	skillsCmd.AddCommand(skillsMutexRecordCmd)
}
