package skillseval

// blind.go 是 skill eval 闭环的盲测 dispatch prompt 构造层。
//
// 普通 eval-cases 问「这个 prompt 会不会触发 skill X」——dispatch 方已知答案域的
// 单 skill 视角。真实路由是全库竞争：路由方只看到每个 skill 的 name+description
// （渐进披露 L1），必须选一个。盲测把 eval.go EvalSkill 里已有的思想（「你是新
// session，收到这个 prompt，你会加载哪个 skill？」）形式化进 case dispatch 通路：
// 每条 case prompt 前置全库清单，问题从「是否触发 X」变成「该触发哪个」。ground
// truth（case 的 target）不进 prompt——record 通路（SubmitResult.actual_triggered，
// NormalizeTriggered 的 canonical 精确匹配）本就承载「<skill名|none>」，盲测 run
// 无需改 record 侧，actual ≠ target 的行即误路由混淆数据。

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/MjxUpUp/Forge/internal/skillsdist"
	"github.com/MjxUpUp/Forge/internal/skillsfm"
)

// blindDescRunes 是全库清单里每条 description 的截断上限。50+ skill 全文会爆
// dispatch prompt 上下文；200 rune 对齐 eval.go 已有的 firstNRunes 先例
// （EvalSkill 的 description 展示）。
const blindDescRunes = 200

// BlindPreamble builds the blind-dispatch preamble: every canonical skill's name
// + description (truncated, single-lined) plus the routing question.
//
// BlindPreamble 构造盲测 dispatch 前置：全库 skill 的 name + description（截断、
// 单行化）+ 路由问题。清单顺序跟 ListSkills（确定性）——该范式打乱的是查询，
// 不是库清单。
func BlindPreamble(canonical string) (string, error) {
	names, err := skillsdist.ListSkills(canonical)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("你是新 session。下面是全部可用 skill 的 name + description（渐进披露 L1：你只能看到这些元数据，看不到任何 SKILL.md 正文）。\n")
	b.WriteString("收到下方的用户 prompt 后，你该加载哪个 skill？只回答 skill 名；都不该触发时回答 none。\n\n")
	b.WriteString("## Skill 库\n")
	for _, n := range names {
		data, err := os.ReadFile(filepath.Join(canonical, n, "SKILL.md"))
		if err != nil {
			return "", err
		}
		desc := skillsfm.Parse(data).Description
		if utf8.RuneCountInString(desc) > blindDescRunes {
			desc = firstNRunes(desc, blindDescRunes) + "..."
		}
		// 每条一行：内嵌换行会破坏清单可扫读性。
		desc = strings.Join(strings.Fields(desc), " ")
		b.WriteString("- " + n + ": " + desc + "\n")
	}
	return b.String(), nil
}

// BlindPrompt wraps one case prompt with the preamble into a self-contained
// dispatch prompt (each case is dispatched to a fresh subagent, so the preamble
// must travel with every case).
//
// BlindPrompt 把单条 case prompt 包上前置，成自包含 dispatch prompt（每条 case
// 独立 dispatch 给 fresh subagent，前置必须随每条走）。
func BlindPrompt(preamble, casePrompt string) string {
	return preamble + "\n## 用户 Prompt\n" + casePrompt + "\n"
}
