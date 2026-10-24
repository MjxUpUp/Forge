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
		{"首轮并行多 reviewer：按轨道续用，不新派补位", "只续用发现所在轨道的那个，不新派补位"},
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

// TestDocReReviewGuidanceContract pins the tiered doc re-review semantics: both
// tiers, the incremental-input contract, the no-fork rule; a pipe is banned
// because the guidance is spliced into gate reasons that are joined by "; ".
//
// TestDocReReviewGuidanceContract 钉住文档复审的分档语义：两档、增量输入契约、
// 禁 fork 作者会话；禁竖线——它被拼进以「; 」连接的门禁原因串。
func TestDocReReviewGuidanceContract(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		{"增量档：续用首轮评审者", "续用首轮评审者"},
		{"增量档判据：修订 diff 小", "修订 diff 小"},
		{"增量档判据：L1 lint 全过", "L1 lint 全过"},
		{"增量输入契约：只喂上轮 findings + 修订 diff", "上轮 findings（含编号）+ 修订 diff"},
		{"增量输入契约：不给作者推理", "不给作者推理"},
		{"范围约束：不重读未改动段落", "不重读未改动段落"},
		{"升档：新派只读子代理全量评审", "新派只读子代理全量评审"},
		{"升档触发：重写结论或判定段", "重写结论或判定段"},
		{"升档触发：第 3 轮起", "第 3 轮起"},
		{"升档触发：同类发现不收敛", "同类发现连续两轮不收敛"},
		{"禁止 fork 作者会话", "不 fork 作者会话"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(DocReReviewGuidance, tc.want) {
				t.Errorf("DocReReviewGuidance 缺少 %q，got: %s", tc.want, DocReReviewGuidance)
			}
		})
	}
	if strings.Contains(DocReReviewGuidance, "|") {
		t.Errorf("DocReReviewGuidance 含 '|'，会撑破嵌入它的表格单元格: %s", DocReReviewGuidance)
	}
}

// TestDocReReviewGuidanceConsumersReferenceConstant: the doc gate's retry
// reasons (stale HEAD, stale fingerprint, failed round, low score) must splice
// the constant instead of hand-copying a second wording.
//
// TestDocReReviewGuidanceConsumersReferenceConstant：文档门禁的四条重评原因
// （HEAD 过期/指纹过期/未通过/低分）须拼接该常量，不得手抄第二份口径。
func TestDocReReviewGuidanceConsumersReferenceConstant(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "taskpipeline", "docgate.go"))
	if err != nil {
		t.Fatalf("读取 docgate.go: %v", err)
	}
	refExpr := regexp.MustCompile(`\+\s*review\.DocReReviewGuidance`)
	if n := len(refExpr.FindAllIndex(b, -1)); n < 4 {
		t.Errorf("docgate.go 引用 review.DocReReviewGuidance 的重评原因应 ≥4 条，got %d", n)
	}
}

// TestSkillsCarryResumeTier pins the re-review tiering text in the skills whose
// first version dispatched a fresh subagent every round: the stale literal must
// stay gone and the resume wording must stay in.
//
// TestSkillsCarryResumeTier 钉住曾每轮新派子代理的几份 skill 的复审分档文字：
// 旧字面量不得回归，续用口径不得丢。
func TestSkillsCarryResumeTier(t *testing.T) {
	for _, tc := range []struct {
		name  string
		want  []string
		stale []string
	}{
		{
			name:  "doc-review",
			want:  []string{"增量复核（默认档）", "续用首轮评审者", "全量评审（升档）", "fork 作者会话"},
			stale: []string{"重新派独立子代理评审"},
		},
		{
			name:  "adversarial-verification",
			want:  []string{"增量重验（修复轮默认）", "宣布 PASS 之前必须整体重验一遍", "复检轮续用原红队"},
			stale: []string{"**修完必须整体重验**"},
		},
		{
			name: "code-review-gate",
			want: []string{"按轨道续用", "不新派补位"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join("..", "..", "skills", tc.name, "SKILL.md"))
			if err != nil {
				t.Fatalf("读取 skill: %v", err)
			}
			src := string(b)
			for _, w := range tc.want {
				if !strings.Contains(src, w) {
					t.Errorf("skills/%s/SKILL.md 缺少 %q", tc.name, w)
				}
			}
			for _, s := range tc.stale {
				if strings.Contains(src, s) {
					t.Errorf("skills/%s/SKILL.md 回归了旧字面量 %q", tc.name, s)
				}
			}
		})
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
