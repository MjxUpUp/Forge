package cli

// review_llm_test.go — L3 判官臂 CLI 面配对测试：review llm 的脚手架/校验/留档
// 三路径 + eval golden harvest 的 --since 错误路径与空产出口径。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/evalkit"
	"github.com/spf13/cobra"
)

func llmCmd(scores string) *cobra.Command {
	cmd := &cobra.Command{Use: "llm"}
	cmd.Flags().String("scores", "", "")
	_ = cmd.Flags().Set("scores", scores)
	return cmd
}

// TestReviewLLM_Scaffold：无 flag 打印派遣说明（消费方是编排 agent）。
func TestReviewLLM_Scaffold(t *testing.T) {
	out := captureStdout(t, func() {
		if err := runReviewLLM(llmCmd(""), nil); err != nil {
			t.Fatalf(`无 flag 应打印脚手架: %v`, err)
		}
	})
	if !strings.Contains(out, "独立语义评审官") || !strings.Contains(out, "mock-hallucination") {
		t.Errorf(`派遣说明缺角色/finding 类: %.120s`, out)
	}
}

// TestReviewLLM_ScoresLifecycle：坏形状拒绝 → <2 条拒绝 → 合法判分留档 judge-samples。
func TestReviewLLM_ScoresLifecycle(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := runReviewLLM(llmCmd(filepath.Join(dir, "nope.json")), nil); err == nil {
		t.Error(`缺文件应报错`)
	}
	bad := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(bad, []byte(`[{"doc_id":"x"}]`), 0o644) // 缺 judge_scores/threshold
	if err := runReviewLLM(llmCmd(bad), nil); err == nil {
		t.Error(`缺字段的 entry 应拒绝`)
	}
	thin := filepath.Join(dir, "thin.json")
	entries := []evalkit.JudgeAuditEntry{{DocID: "only-one", JudgeScores: []int{80}, HumanScore: 80, Threshold: 60}}
	body, _ := json.Marshal(entries)
	_ = os.WriteFile(thin, body, 0o644)
	if err := runReviewLLM(llmCmd(thin), nil); err == nil {
		t.Error(`<2 条判分应拒绝（κ 需 ≥2）`)
	}

	full := filepath.Join(dir, "full.json")
	entries = append(entries, evalkit.JudgeAuditEntry{DocID: "second", JudgeScores: []int{30}, HumanScore: 30, Threshold: 60})
	body, _ = json.Marshal(entries)
	_ = os.WriteFile(full, body, 0o644)
	if err := runReviewLLM(llmCmd(full), nil); err != nil {
		t.Fatalf(`合法判分应留档: %v`, err)
	}
	matches, _ := filepath.Glob(filepath.Join("evals", "forge", "judge-samples", "llm-*.json"))
	if len(matches) != 1 {
		t.Fatalf(`应留档 1 份判分副本，got %v`, matches)
	}
}

// TestEvalGoldenHarvest_CLI：--since 不可达 ref 报错（eval.go 配对面）。
func TestEvalGoldenHarvest_CLI(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	if out, _, code := runForgeStreams(t, dir, `init`, `--mode`, `medium`); code != 0 {
		t.Fatalf(`forge init failed: %s`, out)
	}
	t.Chdir(dir)
	cmd := &cobra.Command{Use: "harvest"}
	cmd.Flags().String("since", "", "")
	_ = cmd.Flags().Set("since", "no-such-ref")
	err := runEvalGoldenHarvest(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "不是可达的 git ref") {
		t.Errorf(`坏 ref 应明确报错，got %v`, err)
	}
}
