package taskpipeline

import (
	"bufio"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// CheckNameAcceptance is the checklog entry recorded after verify-acceptance actually runs the acceptance criteria
// (deterministic source). Like test-run: forge runs the acceptance command itself and checks the result, cannot be forged.
// Turns the Run+Expected acceptance criteria of dev-workflow Plan from floating in plan text into actually-run recorded
// deterministic evidence — spec-as-gate, hedging against the blind spot of agents self-claiming meets acceptance.
//
// CheckNameAcceptance 是 verify-acceptance 实跑验收标准后记的 checklog 条目
// （deterministic 源）。与 test-run 同理：forge 自己跑验收命令并看结果，不可伪造。
// 把 dev-workflow Plan 的 Run+Expected 验收标准从"plan 文本里飘着"变成"实跑留痕的
// deterministic 证据"——spec 即 gate，对冲 agent 自述"满足验收"的盲区。
const CheckNameAcceptance checklog.CheckName = "acceptance"

// parseOneAcceptance parses a single `run :: expected` string into AcceptanceCriterion. Extracted from
// ParseAcceptance so the --accept entry and --plan-file extraction share the same :: boundary handling
// (trailing bare :: / both-side trim / empty expected). Pure function.
//
// parseOneAcceptance 解析单条 `run :: expected` 串为 AcceptanceCriterion。从
// ParseAcceptance 抽出，供 --accept 入口与 --plan-file 提取共用同一 :: 边界处理
// （尾部裸 :: / 两侧 trim / 空期望）。纯函数。
func parseOneAcceptance(s string) AcceptanceCriterion {
	run, expected, found := strings.Cut(s, ` :: `)
	if !found {
		// Trailing bare :: / space-:: (no expected): strip it, avoid leaking into the Run command and mis-executing.
		// Cut already set expected to "" on miss; here we only correct run.
		//
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

// ParseAcceptance parses the raw string list from forge task start --accept into AcceptanceCriterion.
// Delimiter ` :: ` (space-colon-colon-space, rare in commands); no delimiter → whole string is Run, Expected empty
// (only check exit code 0). Trailing bare :: (e.g. `go vet ::`, user left expected empty) is also treated as no expectation — otherwise
// :: would leak into the Run command and cause silent mis-execution. Pure function, easy to unit-test.
//
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

// ParseAcceptanceFromPlan extracts acceptance criteria from the full Plan markdown text, eliminating the disconnect of
// hand-copying Run/Expected from plan to --accept (dogfood lesson: self-driven hand-copying always misses, and zero signal
// when not copied — executor's acceptance advisory only fires when HasAcceptance() is true, not registered means silent).
// Line-scans all `Run: <cmd>` lines, pairs each with the following `Expected: <substr>` line, merges them into `<cmd> :: <substr>`
// strings fed to parseOneAcceptance (reuses all :: boundary handling of --accept).
//
// Layout compatibility: dev-workflow phase 2 Run/Expected can be written centrally or inlined per Task block; full-text
// scan captures both. Boundaries: bare `Run:` (no following Expected:) → empty expected (only check exit code 0); `Expected:`
// without preceding `Run:` → orphaned, discarded; prefix is case-sensitive (Run:/Expected:). Companion: cli.task start reads
// --plan-file then calls this function, dedupes with explicit --accept via MergeAcceptance.
// fenced fence recognition: lines between ```/~~~ are treated as code examples (like shell snippets pasted in plan); their
// Run:/Expected: are not extracted — without this the original would misextract lines starting with Run: in code examples. The for loop below
// uses inFence state to skip fenced content (isFenceMarker detects fence boundaries).
//
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
// fenced 围栏识别：```/~~~ 之间的行视为代码示例（如 plan 贴的 shell 片段），其中的
// Run:/Expected: 不提取——原版无此识别会误提取代码示例里 Run: 开头的行。下方 for 循环
// 用 inFence 状态跳过围栏内容（isFenceMarker 判定围栏边界）。
func ParseAcceptanceFromPlan(plan string) []AcceptanceCriterion {
	var out []AcceptanceCriterion
	// pendingRun holds the previous Run: command not yet paired with Expected:; ""=none pending.
	var pendingRun string // 上一个 Run: 命令，尚未被 Expected: 配对；""=无待配对
	scanner := bufio.NewScanner(strings.NewReader(plan))
	// Align with project convention (6 scanners across toolusage/skillseval/checklog/clone/hazard all do this):
	// raise the per-line limit to 1MB (default 64KB — an inline shell over 64KB makes Scan silently return false,
	// all subsequent Run/Expected blocks dropped, user sees "0 extracted" instead of "truncated mid-way") + check Err after the loop.
	// Run lines in plan cannot approach 1MB, Err never actually fires; defensively return the scanned portion rather than swallow the error.
	//
	// 对齐项目惯例（toolusage/skillseval/checklog/clone/hazard 等 6 处 scanner 全如此）：
	// 扩容单行上限到 1MB（默认 64KB——超 64KB 的内联 shell 会让 Scan 静默返回 false，
	// 后续 Run/Expected 块全丢，用户看到「0 条提取」实为「中途截断」）+ 循环后查 Err。
	// plan 的 Run 行不可能接近 1MB，Err 实际不触发；防御性返回已扫描部分而非吞错。
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	// inFence tracks being inside a fenced code block (between ```/~~~) → skip Run:/Expected: code examples.
	inFence := false // fenced code 围栏内（```/~~~ 之间）→ 跳过 Run:/Expected: 代码示例
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// fenced fence (```/~~~) toggles inFence: Run:/Expected: inside the fence are code examples (e.g. plan
		// pastes a shell snippet that happens to start with Run:), not acceptance criteria, skip them.
		//
		// fenced 围栏（```/~~~）切换 inFence：围栏内的 Run:/Expected: 是代码示例（如 plan
		// 贴的 shell 片段恰好含 Run: 开头行），不是验收标准，跳过。
		if isFenceMarker(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		switch {
		case strings.HasPrefix(line, `Run:`):
			// Previous Run still unpaired → flush first (bare Run = empty expectation, only check exit code 0).
			//
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
			// Expected: without preceding Run: → orphaned, discarded.
			//
			// Expected: 前无 Run: → 孤立，丢弃
		}
	}
	// Final flush: trailing bare Run: (end-of-file still no Expected: pair, or the last unpaired Run before Err interruption).
	// Must run BEFORE the Err check — pendingRun is a scanned valid entry, the Err branch should also flush it (consistent with
	// the comment "scanned valid entries still returned"), rather than being discarded by an early return.
	//
	// 收尾：末尾裸 Run:（文件结束仍无 Expected: 配对，或 Err 中断前最后一条未配对 Run）。
	// 必须在 Err 检查之前——pendingRun 是已扫描的合法条目，Err 分支也该落盘它（与
	// 注释「已扫描的合法条目仍返回」一致），而非被提前 return 丢弃。
	if pendingRun != "" {
		out = append(out, parseOneAcceptance(pendingRun))
	}
	if err := scanner.Err(); err != nil {
		// Extreme cases like a single line over 1MB: scanned valid entries (including the trailing bare Run flushed above) are all returned,
		// only the over-long line that triggered Err is dropped. A single plan line cannot approach 1MB, never actually fires.
		//
		// 单行超 1MB 等极端情况：已扫描的合法条目（含上方落盘的末尾裸 Run）均返回，
		// 仅丢弃触发 Err 的超长行本身。plan 单行不可能接近 1MB，实际不触发。
		return out
	}
	return out
}

// MergeAcceptance merges two sets of acceptance criteria: base takes priority (explicit --accept), addition deduplicates by Run to fill in.
// Used when --plan-file extraction coexists with explicit --accept: explicit entries express override/tweak of a criterion and should win, plan
// extraction only fills Runs that base did not cover.
// Constraint: the return value may reuse base's backing array (when addition is non-empty and base has spare capacity, append writes in place),
// callers should not use the base slice afterwards (the only current caller discards it after passing, safe).
//
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

// JudgeAcceptance is the single source of truth for acceptance three-state judgement: RunTestCommand's passed (exit 0)
// AND Expected substring match. Two paths share it — VerifyAcceptance (verify-acceptance actually runs and fills state)
// and forge_task_proof's v1 re-run fallback (read-only judge, no write-back) call the same function, preventing semantic drift (historical bug: proof v1 once
// judged only exit code and missed Expected substring → when a command exits 0 but output lacks Expected, acceptance false-greened, breaking proof's claim).
//
// Three states: passed=false → false; Expected non-empty → Contains(output, Expected); otherwise → true.
//
// JudgeAcceptance 是 acceptance 三态判定的单一真相源：RunTestCommand 的 passed(exit 0)
// 与 Expected 子串比对。两条路径共用——VerifyAcceptance(verify-acceptance 实跑回填 state)
// 与 forge_task_proof 的 v1 重跑兜底(只读判不回写)同调，防语义漂移（历史 bug：proof v1 曾只
// 判退出码漏 Expected 子串 → 命令退出 0 但输出不含 Expected 时假绿 acceptance，击穿 proof 主张）。
//
// 三态：passed=false → false；Expected 非空 → Contains(output, Expected)；否则 → true。
func JudgeAcceptance(passed bool, output, expected string) bool {
	switch {
	case !passed:
		return false
	case expected != "":
		return strings.Contains(output, expected)
	default:
		// Exit code 0 and no expected substring.
		return true // 退出码 0 且无期望子串
	}
}

// TruncateAcceptanceOutput exports the truncation helper so forge_task_proof's v1 re-run output reuses the same truncation
// logic (failure info at the tail + cut-point retreats to rune boundary + prevents MCP payload blowup). The private truncateAcceptanceOutput
// remains the internal implementation; this export is its stable facade.
//
// TruncateAcceptanceOutput 导出截断 helper，供 forge_task_proof 的 v1 重跑输出复用同一截断
// 逻辑（失败信息在尾部 + 截点退到 rune 边界 + 防 MCP payload 撑爆）。私有 truncateAcceptanceOutput
// 仍是内部实现，本导出是其稳定门面。
func TruncateAcceptanceOutput(s string) string {
	return truncateAcceptanceOutput(s)
}

// VerifyAcceptance actually runs each Run command of state's acceptance criteria, matches the Expected substring, fills back
// Passed/Output. Reuses RunTestCommand (same execution path as forge verify --run-tests).
// Expected non-empty → Passed = output contains the substring; Expected empty → Passed = exit code 0.
// Does not write checklog — the caller (CLI) decides when to record; this function stays pure logic for unit-testing.
//
// VerifyAcceptance 实跑 state 里每条验收标准的 Run 命令，比对 Expected 子串，回填
// Passed/Output。复用 RunTestCommand（与 forge verify --run-tests 同一执行路径）。
// Expected 非空→Passed = 输出含该子串；Expected 空→Passed = 退出码 0。
// 不写 checklog——调用方（CLI）决定记录时机，本函数保持纯逻辑可单测。
func VerifyAcceptance(root string, state *TaskState) {
	for i := range state.Acceptance {
		c := &state.Acceptance[i]
		passed, output := RunTestCommand(root, c.Run)
		c.Passed = JudgeAcceptance(passed, output, c.Expected)
		c.Output = truncateAcceptanceOutput(output)
		// Record the HEAD snapshot at actual-run time: forge_task_proof uses this to decide whether Passed is fresh (== current HEAD).
		// Old no-snapshot (empty) → proof v1 re-run fallback; has snapshot but != HEAD → acceptance is based on old code, must re-run.
		//
		// 记实跑时的 HEAD 快照：forge_task_proof 据此判定 Passed 是否 fresh（== 当前 HEAD）。
		// 老无快照（空）→ proof v1 重跑兜底；有快照但 != HEAD → acceptance 基于旧代码，须重跑。
		c.AcceptedHeadCommit = GetHeadCommit(root)
	}
}

// truncateAcceptanceOutput truncates the actual-run output to the last ~500 bytes: failure info is at the tail,
// keeping the tail is enough for debugging; also avoids large output blowing up TaskState JSON. Key: the cut-point must retreat to a rune
// boundary — a byte cut-point lands inside a multi-byte UTF-8 character (common in Chinese compile errors / exception stacks), producing invalid
// UTF-8, json.Marshal writes � garbage on disk, losing debugging value (this feature exists for traceable evidence).
//
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
		// Skip continuation bytes (10xxxxxx), retreat to the next rune's start byte.
		start++ // 跳过续字节（10xxxxxx），退到下一个 rune 起始字节
	}
	return `...(省略前部)...` + s[start:]
}

// isFenceMarker decides whether a line is a markdown fenced code boundary (>=3 backticks or tildes at line start,
// optionally followed by a language tag like bash). ParseAcceptanceFromPlan uses this to skip Run:/Expected:
// misextraction inside code example blocks. Backticks are compared via byte code 96, avoiding the Windows Edit quote-corruption pitfall of writing bare backtick strings in source.
//
// isFenceMarker 判定一行是否 markdown fenced code 围栏边界（行首 >=3 个反引号或波浪号，
// 后可跟语言标注如 bash）。ParseAcceptanceFromPlan 据此跳过代码示例块内的 Run:/Expected:
// 误提取。反引号用字节码 96 比较，规避源码里写裸反引号串在 Windows Edit 的引号腐蚀坑。
func isFenceMarker(line string) bool {
	if len(line) < 3 {
		return false
	}
	first := line[0]
	if first != 96 && first != '~' { // 96 = '`'（反引号）；'~' 波浪号
		return false
	}
	return line[1] == first && line[2] == first
}

// acceptanceGateDisableEnv lets a task exit the acceptance pre-flight at task-complete
// (symmetric to FORGE_TEST_COVERAGE). Legitimate scenarios: acceptance criteria not machine-executable, or
// pure manual acceptance. The CLI names this escape hatch in the BLOCKED text (no silent bypass); escape → checklog
// CheckEscapeHatch → evidence Strength cap Weak (has cost).
//
// acceptanceGateDisableEnv 让 task 可在 task-complete 处退出 acceptance pre-flight
// task-complete（symmetric to FORGE_TEST_COVERAGE）. 合法场景：验收标准不可机器执行、或
// 纯人工验收。CLI 在 BLOCKED 文案里明示此逃生舱（不静默绕过）；逃生 → checklog
// CheckEscapeHatch → evidence Strength cap Weak（有代价）。
const acceptanceGateDisableEnv = "FORGE_ACCEPTANCE_GATE"

// CheckAcceptanceFresh is task-complete's acceptance pre-flight — gives AcceptedHeadCommit a
// deterministic consumer (after MCP teardown this field is written by VerifyAcceptance but has no consumer, orphaned).
// When a task declares acceptance, each criterion must simultaneously satisfy:
//   - AcceptedHeadCommit non-empty (has run forge task verify-acceptance, has an actual-run snapshot)
//   - AcceptedHeadCommit == current HEAD (acceptance based on current code; if code changed after acceptance the snapshot is stale, must re-run)
//   - Passed == true (acceptance actually-run passed)
//
// Any failure → ok=false + reasons (for the BLOCKED text). No acceptance → pass. Non-git directory:
// GetHeadCommit returns "", fresh-check short-circuits to pass (consistent with VerifyAcceptance's NonGit degradation).
// escape (per-task override / FORGE_ACCEPTANCE_GATE=disable) records a checklog audit entry then passes.
//
// Design corresponds to Emergence World affordance gate + Proof of Work: claiming "acceptance passed" must have a deterministic
// consumer to verify, otherwise it is sounds-like-verification on an orphaned field (the proof v2 fast-path consumer went with MCP teardown).
//
// CheckAcceptanceFresh 是 task-complete 的 acceptance pre-flight——给 AcceptedHeadCommit 补
// deterministic consumer（MCP 拆除后该字段在 VerifyAcceptance 写入但无消费方，成孤儿）。
// task 声明了 acceptance 时，每条必须同时满足：
//   - AcceptedHeadCommit 非空（跑过 forge task verify-acceptance，有实跑快照）
//   - AcceptedHeadCommit == 当前 HEAD（验收基于当前代码；验收后改码则快照过期，须重跑）
//   - Passed == true（验收实跑通过）
//
// 任一不满足 → ok=false + reasons（给 BLOCKED 文案）。无 acceptance → 放行。非 git 目录：
// GetHeadCommit 返 ""，fresh 检查短路放行（与 VerifyAcceptance 的 NonGit 退化一致）。
// escape（per-task override / FORGE_ACCEPTANCE_GATE=disable）落 checklog 审计后放行。
//
// 设计对应 Emergence World affordance gate + Proof of Work：声称「验收过」须有 deterministic
// consumer 校验，否则就是孤儿字段的 sounds-like-verification（proof v2 快路径消费者随 MCP 拆除）。
func CheckAcceptanceFresh(root string, state *TaskState) (ok bool, reasons []string) {
	if len(state.Acceptance) == 0 {
		return true, nil
	}
	if EscapeDisabled(state, escapeAcceptanceGate, acceptanceGateDisableEnv) {
		recordAudit(root, &checklog.Entry{
			Check:   checklog.CheckEscapeHatch,
			Passed:  true,
			Checked: true,
			TaskRef: state.TaskRef,
			Detail:  `escape-hatch: acceptance gate bypassed (per-task override or FORGE_ACCEPTANCE_GATE=disable)`,
		})
		return true, nil
	}
	head := GetHeadCommit(root)
	// Non-git directory short-circuits to pass: when GetHeadCommit returns "", AcceptedHeadCommit is always empty (VerifyAcceptance
	// non-git degradation also writes empty), the case 1 "not actually run" below would misfire and BLOCK forever. Consistent with the doc contract (NonGit short-circuit)
	// and VerifyAcceptance degradation. Forge explicitly supports non-git (IsGitRepo "degrades gracefully without git").
	//
	// 非 git 目录短路放行：GetHeadCommit 返 "" 时 AcceptedHeadCommit 永远空（VerifyAcceptance
	// 非 git 退化也写空），下方 case 1「未实跑」会误命中致永远 BLOCKED。与文档契约（NonGit 短路）
	// 和 VerifyAcceptance 退化一致。Forge 显式支持非 git（IsGitRepo "degrades gracefully without git"）。
	if head == "" {
		return true, nil
	}
	for i := range state.Acceptance {
		c := &state.Acceptance[i]
		switch {
		case c.AcceptedHeadCommit == "":
			reasons = append(reasons, fmt.Sprintf(`验收 #%d（%s）未实跑（AcceptedHeadCommit 空）——先 forge task verify-acceptance`, i+1, c.Run))
		// head is guaranteed non-empty here (empty HEAD already returned above —
		// the non-git short-circuit).
		//
		// 此处 head 恒非空（空 HEAD 已在上方非 git 短路提前 return）。
		case c.AcceptedHeadCommit != head:
			reasons = append(reasons, fmt.Sprintf(`验收 #%d（%s）基于旧代码（快照 %s ≠ HEAD %s）——验收后改了码，重跑 forge task verify-acceptance`, i+1, c.Run, c.AcceptedHeadCommit, head))
		case !c.Passed:
			reasons = append(reasons, fmt.Sprintf(`验收 #%d（%s）未通过——修码使验收通过或调整验收标准`, i+1, c.Run))
		}
	}
	return len(reasons) == 0, reasons
}
