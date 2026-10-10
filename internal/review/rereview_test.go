package review

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestReReviewGuidanceContract pins the tiered re-review semantics carried by
// ReReviewGuidance: both tiers, the incremental-input contract, and the
// `--note` refresh step must stay; a pipe would break the CLAUDE.md table cell
// that embeds it.
//
// TestReReviewGuidanceContract 钉住 ReReviewGuidance 承载的分档复审语义：两档、
// 增量复核的输入契约、`--note` 刷新基线步骤都不得丢；含竖线会撑破嵌入它的
// CLAUDE.md 表格单元格。
func TestReReviewGuidanceContract(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		{"低风险档：续用首轮 reviewer", "续用首轮 reviewer"},
		{"低风险档判据：确定性检查全绿", "确定性检查全绿"},
		{"增量输入契约：不给作者推理", "不给作者推理"},
		{"增量输入契约：只喂 findings + 修复 diff", "上轮 findings + 修复 diff"},
		{"收敛靠范围约束：只审修复 diff 及其波及面", "只审修复 diff 及其波及面"},
		{"收敛靠范围约束：不重审未改动代码", "不重审未改动代码"},
		{"高风险档：新派只读子 agent", "新派只读子 agent 全量复审"},
		{"高风险档判据：终审/第 3 轮起", "终审/第 3 轮起"},
		{"刷新基线步骤", "forge review pass --note"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(ReReviewGuidance, tc.want) {
				t.Errorf("ReReviewGuidance 缺少 %q，got: %s", tc.want, ReReviewGuidance)
			}
		})
	}
	if strings.Contains(ReReviewGuidance, "|") {
		t.Errorf("ReReviewGuidance 含 '|'，会撑破 CLAUDE.md 常见错误表单元格: %s", ReReviewGuidance)
	}
	// code-review-gate 明令不分级（无 severity 字段）：复审收敛只能用范围约束，
	// 严重性措辞在其契约里无定义、不可执行。
	for _, severity := range []string{"Important", "Critical", "nit", "Minor"} {
		if strings.Contains(ReReviewGuidance, severity) {
			t.Errorf("ReReviewGuidance 含严重性措辞 %q——与 code-review-gate 不分级契约冲突: %s", severity, ReReviewGuidance)
		}
	}
}

// TestReReviewGuidanceConsumersReferenceConstant is the single-source guard:
// every user-facing re-review instruction must derive from ReReviewGuidance,
// and the old "always dispatch a fresh reviewer" literals must not come back.
//
// TestReReviewGuidanceConsumersReferenceConstant 是单一真相源守卫：所有面向用户
// 的复审指引都须引用 ReReviewGuidance，旧的「一律重新派」字面量不得回归（手抄
// 第二拷贝 = 漂移）。
func TestReReviewGuidanceConsumersReferenceConstant(t *testing.T) {
	staleLiterals := []string{
		"请重新派只读子 agent 审查",
		"**重新派只读子 agent 复审修复**",
		"重派【只读】子 agent 复审",
		"重派只读子 agent 复审",
	}
	// 只认表达式形态的引用（拼接 / 实参），注释里提到常量名不算派生。
	refExpr := regexp.MustCompile(`[+,(]\s*review\.ReReviewGuidance|review\.ReReviewGuidance\s*[+,)]`)
	for _, rel := range []string{
		filepath.Join("..", "taskpipeline", "executor_check_complete.go"),
		filepath.Join("..", "cli", "review.go"),
		filepath.Join("..", "skillgen", "claudemd.go"),
	} {
		t.Run(rel, func(t *testing.T) {
			b, err := os.ReadFile(rel)
			if err != nil {
				t.Fatalf("读取使用方源码: %v", err)
			}
			src := string(b)
			if !refExpr.MatchString(src) {
				t.Errorf("%s 未引用 review.ReReviewGuidance——复审指引须从单一真相源派生", rel)
			}
			for _, stale := range staleLiterals {
				if strings.Contains(src, stale) {
					t.Errorf("%s 回归了旧复审字面量 %q", rel, stale)
				}
			}
		})
	}
}
