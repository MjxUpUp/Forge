package review

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestReReviewGuidanceContract pins the single-tier re-review semantics: code
// re-review always needs a NEW reviewer id; self re-stamping and `--note`
// baseline refreshes are gone. A pipe would break the CLAUDE.md table cell.
//
// TestReReviewGuidanceContract 钉住代码复审只有一档：必须换新的 reviewer id；
// 同一 reviewer 自证与 `--note` 刷新基线都已移除。含竖线会撑破 CLAUDE.md 表格单元格。
func TestReReviewGuidanceContract(t *testing.T) {
	for _, want := range []string{"新的只读子 agent", "--reviewer <新 id>", "--note", "移除"} {
		if !strings.Contains(ReReviewGuidance, want) {
			t.Errorf("ReReviewGuidance 缺少 %q，got: %s", want, ReReviewGuidance)
		}
	}
	for _, banned := range []string{"续用首轮", "按轨道续用", "刷新审查基线", "|"} {
		if strings.Contains(ReReviewGuidance, banned) {
			t.Errorf("ReReviewGuidance 不得含 %q（分档续用/自供刷新已废止，或会撑破表格）: %s", banned, ReReviewGuidance)
		}
	}
}

// TestDocReReviewGuidanceContract pins the tiered doc re-review semantics: both
// tiers, the incremental-input contract, the no-fork rule. `%` and "; " are
// banned (the text is spliced into fmt.Sprintf format strings and into gate
// reasons joined by "; "); a pipe is not, because unlike ReReviewGuidance this
// text never lands in a markdown table cell.
//
// TestDocReReviewGuidanceContract 钉住文档复审的分档语义：两档、增量输入契约、
// 禁 fork 作者会话。禁 `%` 与「; 」（文本被拼进 fmt.Sprintf 格式串与以「; 」连接
// 的门禁原因串）；不禁竖线——与 ReReviewGuidance 不同，它不进 markdown 表格单元格。
func TestDocReReviewGuidanceContract(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		{"增量档：续用首轮评审者", "续用首轮评审者"},
		{"增量档判据：修订 diff 小", "修订 diff 小"},
		{"前置：L1 未全过先修 L1，不进评审", "L1 lint 未全过先修 L1，不进评审"},
		{"不可续用时新派并粘贴上轮 findings", "新派只读子代理，并把上轮 findings 全文含编号粘进 prompt"},
		{"增量输入契约：只喂上轮 findings + 修订 diff", "上轮 findings（含编号）+ 修订 diff"},
		{"增量输入契约：不给作者推理", "不给作者推理"},
		{"范围约束：不重读未改动段落", "不重读未改动段落"},
		{"升档：新派只读子代理全量评审", "新派只读子代理全量评审"},
		{"升档触发：重写结论或判定段", "重写结论或判定段"},
		{"升档触发：即将进行第 3 轮评审", "即将进行第 3 轮评审"},
		{"升档换视角", "全量评审并换视角"},
		{"增量档对抗立场不变", "对抗立场不变"},
		{"增量档记分：以上轮为底", "四维分以上轮为底"},
		{"增量档记分：未触及的维度不得升档", "未触及的维度不得升档"},
		{"增量档：派审前留存被评审版本生成 diff", "派审前留存被评审版本"},
		{"增量档：被评审版本放仓库外", "放仓库外临时目录"},
		{"增量档：先生成修订 diff 再覆盖快照", "先对照旧快照生成修订 diff，再用当前版本覆盖快照"},
		{"升档：只喂文档本身不喂上轮 findings", "只喂文档本身，不喂上轮 findings"},
		{"borderline 同家族放行前另派第二评委（新派全量）", "评审与产出同家族（家族未知按同家族）且总分落在 75-79 即将放行时另派第二评委（新派、全量"},
		{"第二评委不改变续用对象", "不改变续用对象"},
		{"评审者可声明需升档", "评审者认为修订实质改了结论则声明需升档"},
		{"未决 Critical 须评审者判定后才可标记", "未决 Critical 须评审者判定已解决后才可标记，作者不得自行标记"},
		{"快照缺失按升档", "快照缺失按升档"},
		{"禁止 fork 作者会话", "不 fork 作者会话"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(DocReReviewGuidance, tc.want) {
				t.Errorf("DocReReviewGuidance 缺少 %q，got: %s", tc.want, DocReReviewGuidance)
			}
		})
	}
	// 它被拼进 docgate.go 的 fmt.Sprintf 格式串（% 会被当格式动词，如「30%」会输出
	// %!…(MISSING) 乱码），并被以 "; " 连接成门禁原因串（ASCII 分号会让按分号拆分的
	// 消费者误断）。
	for _, banned := range []string{"%", "; "} {
		if strings.Contains(DocReReviewGuidance, banned) {
			t.Errorf("DocReReviewGuidance 含 %q，会破坏 Sprintf 格式串或原因串拼接: %s", banned, DocReReviewGuidance)
		}
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
			want:  []string{"增量复核（默认档）", "续用首轮评审者", "全量评审（升档）", "fork 作者会话", "另派第二评委", "家族未知按同家族处理", "每次派审前，把被评审版本逐份复制到仓库外的临时目录", "--word-diff-regex=.", "只喂文档本身，不喂上轮 findings", "再用当前文件覆盖快照，反过来 diff 恒为空", "export LC_ALL=en_US.UTF-8", "「需升档」", "多份文档逐份留快照", "第二评委不改变续用对象", "N=2 时下一次即第 3 轮", "沿用其重述的四维分记录", "只有评审者判定已解决的才可在门禁里标记已解决", "快照不存在（丢失或从没留过）按升档处理", "HEAD 前移但文档未变", "作者不得自行标记"},
			stale: []string{"重新派独立子代理评审"},
		},
		{
			name: "adversarial-verification",
			want: []string{"复检轮续用原红队", "不得引用前轮结果", "复验续用原验收者", "修完必须整体重验", "前轮输出不作证据", "喂验收点清单", "不粘贴上轮清单（防锚定）"},
		},
		{
			name: "code-review-gate",
			want: []string{"按轨道续用", "不新派补位", "`cheat-detector` 无发现时", "命中下面作弊模式速查表任一项", "`eng-reviewer` 始终续用", "首轮零发现", "有发现的轨道必续用"},
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
