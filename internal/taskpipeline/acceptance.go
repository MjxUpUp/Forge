package taskpipeline

import (
	"strings"
	"unicode/utf8"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// CheckNameAcceptance 是 verify-acceptance 实跑验收标准后记的 checklog 条目
// （deterministic 源）。与 test-run 同理：forge 自己跑验收命令并看结果，不可伪造。
// 把 dev-workflow Plan 的 Run+Expected 验收标准从"plan 文本里飘着"变成"实跑留痕的
// deterministic 证据"——spec 即 gate，对冲 agent 自述"满足验收"的盲区。
const CheckNameAcceptance checklog.CheckName = "acceptance"

// ParseAcceptance 把 `forge task start --accept "run :: expected"` 的原始串列表
// 解析成 AcceptanceCriterion。分隔符 " :: "（空格-冒号-冒号-空格，命令里罕见）；
// 无分隔符→整串作 Run、Expected 空（只看退出码 0）。尾部裸 ::（如 "go vet ::"，
// 用户留空 expected）也视为"无期望"——否则 :: 会漏进 Run 命令导致静默误执行。
// 纯函数，便于单测。
func ParseAcceptance(raw []string) []AcceptanceCriterion {
	out := make([]AcceptanceCriterion, 0, len(raw))
	for _, s := range raw {
		run, expected, found := strings.Cut(s, ` :: `)
		if !found {
			// 尾部裸 "::"/" ::"（无 expected）：剥掉，避免漏进 Run 命令误执行。
			// Cut 未命中时 expected 已是 ""，这里只校正 run。
			t := strings.TrimRight(s, ` `)
			if strings.HasSuffix(t, `::`) {
				run = t[:len(t)-len(`::`)]
			} else {
				run = s
			}
		}
		out = append(out, AcceptanceCriterion{
			Run:      strings.TrimSpace(run),
			Expected: strings.TrimSpace(expected),
		})
	}
	return out
}

// VerifyAcceptance 实跑 state 里每条验收标准的 Run 命令，比对 Expected 子串，回填
// Passed/Output。复用 RunTestCommand（与 forge verify --run-tests 同一执行路径）。
// Expected 非空→Passed = 输出含该子串；Expected 空→Passed = 退出码 0。
// 不写 checklog——调用方（CLI）决定记录时机，本函数保持纯逻辑可单测。
func VerifyAcceptance(root string, state *TaskState) {
	for i := range state.Acceptance {
		c := &state.Acceptance[i]
		passed, output := RunTestCommand(root, c.Run)
		switch {
		case !passed:
			c.Passed = false
		case c.Expected != "":
			c.Passed = strings.Contains(output, c.Expected)
		default:
			c.Passed = true // 退出码 0 且无期望子串
		}
		c.Output = truncateAcceptanceOutput(output)
	}
}

// truncateAcceptanceOutput 截断实跑输出到末尾 ~500 字节：失败信息在输出尾部，
// 保留尾部即可排查；同时避免大输出撑爆 TaskState JSON。关键：切点必须回退到 rune
// 边界——字节切点会落在多字节 UTF-8 字符中间（中文编译错误/异常栈常见），产出无效
// UTF-8，json.Marshal 落盘成 � 乱码，丢掉排查价值（本特性要的就是可追溯证据）。
func truncateAcceptanceOutput(s string) string {
	const maxBytes = 500
	if len(s) <= maxBytes {
		return s
	}
	start := len(s) - maxBytes
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++ // 跳过续字节（10xxxxxx），退到下一个 rune 起始字节
	}
	return `...(省略前部)...` + s[start:]
}
