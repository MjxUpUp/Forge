package taskpipeline

import (
	"bufio"
	"strings"
	"unicode/utf8"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// CheckNameAcceptance 是 verify-acceptance 实跑验收标准后记的 checklog 条目
// （deterministic 源）。与 test-run 同理：forge 自己跑验收命令并看结果，不可伪造。
// 把 dev-workflow Plan 的 Run+Expected 验收标准从"plan 文本里飘着"变成"实跑留痕的
// deterministic 证据"——spec 即 gate，对冲 agent 自述"满足验收"的盲区。
const CheckNameAcceptance checklog.CheckName = "acceptance"

// parseOneAcceptance 解析单条 `run :: expected` 串为 AcceptanceCriterion。从
// ParseAcceptance 抽出，供 --accept 入口与 --plan-file 提取共用同一 :: 边界处理
// （尾部裸 :: / 两侧 trim / 空期望）。纯函数。
func parseOneAcceptance(s string) AcceptanceCriterion {
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
	return AcceptanceCriterion{
		Run:      strings.TrimSpace(run),
		Expected: strings.TrimSpace(expected),
	}
}

// ParseAcceptance 把 forge task start --accept 的原始串列表解析成 AcceptanceCriterion。
// 分隔符 ` :: `（空格-冒号-冒号-空格，命令里罕见）；无分隔符→整串作 Run、Expected 空
// （只看退出码 0）。尾部裸 ::（如 `go vet ::`，用户留空 expected）也视为无期望——否则
// :: 会漏进 Run 命令导致静默误执行。纯函数，便于单测。
func ParseAcceptance(raw []string) []AcceptanceCriterion {
	out := make([]AcceptanceCriterion, 0, len(raw))
	for _, s := range raw {
		out = append(out, parseOneAcceptance(s))
	}
	return out
}

// ParseAcceptanceFromPlan 从 Plan markdown 全文提取验收标准，消除把 plan 里的
// Run/Expected 手抄到 --accept 的断口（dogfood 教训：靠自觉手抄必漏，且没抄时零信号——
// executor 的 acceptance advisory 只在 HasAcceptance() 时发，没登记即静默）。行扫描所有
// `Run: <cmd>` 行，配对紧随的 `Expected: <substr>` 行，合并成 `<cmd> :: <substr>` 串喂
// parseOneAcceptance（复用 --accept 全部 :: 边界处理）。
//
// 布局兼容：dev-workflow 阶段 2 的 Run/Expected 可集中写也可在每个 Task block 内联，全文
// 扫描一律捕获。边界：裸 `Run:`（无后续 Expected:）→ expected 空（只看退出码 0）；`Expected:`
// 前无 `Run:` → 孤立丢弃；前缀大小写敏感（Run:/Expected:）。配套：cli.task start 读取
// --plan-file 后调本函数，与显式 --accept 经 MergeAcceptance 去重。
// 局限：不区分 fenced 代码围栏（```）——若 plan 在代码示例里有 Run:/Expected: 开头的
// 行会被误提取。dev-workflow 已强约束 Run:/Expected: 为验收唯一格式，冲突概率低；plan
// 应避免在代码示例中用该前缀开头的行。
func ParseAcceptanceFromPlan(plan string) []AcceptanceCriterion {
	var out []AcceptanceCriterion
	var pendingRun string // 上一个 Run: 命令，尚未被 Expected: 配对；""=无待配对
	scanner := bufio.NewScanner(strings.NewReader(plan))
	// 对齐项目惯例（toolusage/skillseval/checklog/clone/hazard 等 6 处 scanner 全如此）：
	// 扩容单行上限到 1MB（默认 64KB——超 64KB 的内联 shell 会让 Scan 静默返回 false，
	// 后续 Run/Expected 块全丢，用户看到「0 条提取」实为「中途截断」）+ 循环后查 Err。
	// plan 的 Run 行不可能接近 1MB，Err 实际不触发；防御性返回已扫描部分而非吞错。
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, `Run:`):
			// 上一个 Run 仍未配对 → 先落盘（裸 Run = 空期望，只看退出码 0）
			if pendingRun != "" {
				out = append(out, parseOneAcceptance(pendingRun))
			}
			pendingRun = strings.TrimSpace(strings.TrimPrefix(line, `Run:`))
		case strings.HasPrefix(line, `Expected:`):
			if pendingRun != "" {
				exp := strings.TrimSpace(strings.TrimPrefix(line, `Expected:`))
				out = append(out, parseOneAcceptance(pendingRun+` :: `+exp))
				pendingRun = ""
			}
			// Expected: 前无 Run: → 孤立，丢弃
		}
	}
	// 收尾：末尾裸 Run:（文件结束仍无 Expected: 配对，或 Err 中断前最后一条未配对 Run）。
	// 必须在 Err 检查之前——pendingRun 是已扫描的合法条目，Err 分支也该落盘它（与
	// 注释「已扫描的合法条目仍返回」一致），而非被提前 return 丢弃。
	if pendingRun != "" {
		out = append(out, parseOneAcceptance(pendingRun))
	}
	if err := scanner.Err(); err != nil {
		// 单行超 1MB 等极端情况：已扫描的合法条目（含上方落盘的末尾裸 Run）均返回，
		// 仅丢弃触发 Err 的超长行本身。plan 单行不可能接近 1MB，实际不触发。
		return out
	}
	return out
}

// MergeAcceptance 合并两组验收标准：base 优先（显式 --accept），addition 按 Run 去重补充。
// 用于 --plan-file 提取与显式 --accept 共存：显式条目表达覆盖/微调某条标准应胜出，plan
// 提取只补 base 未覆盖的 Run。
// 约束：返回值可能复用 base 底层数组（addition 非空且 base 有空余容量时 append 原地写），
// 调用后不应再使用 base slice（当前唯一调用方传入后即弃，安全）。
func MergeAcceptance(base, addition []AcceptanceCriterion) []AcceptanceCriterion {
	seen := make(map[string]struct{}, len(base))
	for _, c := range base {
		seen[c.Run] = struct{}{}
	}
	for _, c := range addition {
		if _, ok := seen[c.Run]; ok {
			continue
		}
		base = append(base, c)
		seen[c.Run] = struct{}{}
	}
	return base
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
