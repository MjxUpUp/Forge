package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/scoring"
	"github.com/MjxUpUp/Forge/internal/scoringtypes"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

func init() {
	verifyCmd.Flags().String(`collect-golden`, ``, `从已完成任务采集真实 golden case 到 testdata/golden_real/（开发工具：固化真实评分形状进 CI 回归）`)
}

// buildEvaluateInput thin-wrapper：评分输入组装下沉到 taskpipeline.BuildEvaluateInput
// （单一真相源）。CollectGoldenFromTask 透明复用——cli 与 MCP forge_task_complete 共用同一
// 组装逻辑，不再两份漂移。注释/限制见 taskpipeline.BuildEvaluateInput。
func buildEvaluateInput(root string, state *taskpipeline.TaskState) (*scoring.EvaluateInput, *scoringtypes.ScoringConfig, error) {
	return taskpipeline.BuildEvaluateInput(root, state)
}

// CollectGoldenFromTask 从已完成任务的 TaskState 派生一个 golden case（真实评分形状），
// 供 forge verify --collect-golden 沉淀到 testdata/golden_real/ 做 CI 回归。
// 复用 buildEvaluateInput（与评分同输入同逻辑）→ GoldenCaseFromInput 算 Expected。
// 任务须已评分（state.Score != nil）。git 漂移限制见 taskpipeline.BuildEvaluateInput 注释。
func CollectGoldenFromTask(root, taskRef string) (*scoring.GoldenCase, error) {
	state, err := taskpipeline.LoadTaskState(root, taskRef)
	if err != nil {
		return nil, fmt.Errorf(`load task %s: %w`, taskRef, err)
	}
	if state.Score == nil {
		return nil, fmt.Errorf(`task %s not scored — complete it first`, taskRef)
	}
	input, config, err := buildEvaluateInput(root, state)
	if err != nil {
		return nil, err
	}
	name := goldenCaseName(taskRef)
	rationale := fmt.Sprintf(`真实 dogfood 任务 %s 的评分形状（采集自 TaskState+checklog+git）。钉真实组合不漂移——scoring 算法改动若让此任务评分漂移即 CI 挂。`, taskRef)
	gc := scoring.GoldenCaseFromInput(name, rationale, input, config)
	// Meta：自动采集来源 + git 漂移检测。任务完成后 HEAD 若已推进 → GitDiffStat 含事后
	// 改动 → scope 维度基于漂移 diff（固化后稳定但不真实）。检测到即标 drift_known，
	// CI 对 scope advisory 不 fail，其余维度照常断言。
	gc.Meta = scoring.GoldenMeta{Source: `auto-collected`}
	if taskpipeline.GetHeadCommit(root) != state.HeadCommit {
		gc.Meta.DriftKnown = []string{`scope`}
	}
	return gc, nil
}

// goldenCaseName 把 taskRef（如 feat/review-snapshot）转为 fixture 名片段（feat-review-snapshot）。
func goldenCaseName(taskRef string) string {
	return strings.ReplaceAll(taskRef, "/", "-")
}

// runCollectGoldenMode 是 forge verify --collect-golden <task-ref> 的入口。
// 写到 forge 仓库的 scoring testdata（开发者工具语义：采集进 CI golden 集）。
func runCollectGoldenMode(taskRef string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	gc, err := CollectGoldenFromTask(root, taskRef)
	if err != nil {
		return err
	}
	dir := filepath.Join(findRepoRoot(), "internal", "scoring", "testdata", "golden_real")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf(`mkdir golden_real: %w`, err)
	}
	data, err := json.MarshalIndent(gc, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "golden_real_"+gc.Name+".json")
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		return err
	}
	fmt.Println(`采集 golden case:`, gc.Name)
	fmt.Printf(`  overall %.2f (%s)`+string('\n'), gc.Expected.Overall, gc.Expected.Grade)
	fmt.Println(`  →`, path)
	fmt.Println(`  提醒：若任务完成后 HEAD 已推进，scope/GitDiffStat 维度会漂移。最准的采集是任务完成那刻。`)
	return nil
}
