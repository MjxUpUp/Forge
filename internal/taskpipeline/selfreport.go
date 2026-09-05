package taskpipeline

// selfreport.go — 自报一致性门禁（focus-batches.md §1b，方向 B）：task-complete
// pre-flight 把 checklist 已勾选项里"声称执行过的验证类命令"与 toollog 为本任务
// 实际记录的 Bash 命令集做差集。学术依据 arXiv 2605.29442（20,574 真实会话）：
// "constraint violations and inaccurate self-reporting grow in share"——虚报
// 进度在真实使用中占比增长；Transluce 野外记录过 agent 为过验证门禁回填记录。
// 两侧都是 forge 本地台账（checklist ∈ TaskState、Bash ∈ toollog），比对确定性，
// agent 无法伪造 toollog 侧（宿主 PostToolUse hook 记录）。
//
// 判定分级（No Free Lunch 预算：阻断只留给"证据明确造假"形态）：
//   - 无 checklist / 无勾选项 / toollog 缺失（宿主遥测未接）→ 跳过（Checked=false，
//     不制造假阳性——ToollogHasData 的诚实边界同款）
//   - 全部声称有据 → pass
//   - 非测试类声称（build/lint）未匹配 → warn（差集列出，advisory）
//   - 测试类声称在任务全程 Bash 零匹配 → fail（BLOCKED 形态：声称测过但从未跑过）
//
// 逃生舱：FORGE_SELF_REPORT=disable → 落 escape-hatch 审计后放行（证据封顶 Weak
// 由 scoring 侧 escape 消费，与 acceptance/doc gate 同款契约）。

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/toolusage"
)

// selfReportDisableEnv 是本门禁的逃生舱环境变量（沿 FORGE_DOC_GATE 模式）。
const selfReportDisableEnv = "FORGE_SELF_REPORT"

// verifyCmdPrefixes 是"验证类命令"的已知前缀（测试类在前、构建/lint 类在后）。
// 提取用最长前缀匹配（go test 先于 go）；清单刻意保守——只认主流测试运行器与
// 构建入口，未识别的命令不产生声称（宁漏勿滥：漏掉的只是不检查，认错的会是
// 假阳性）。
var testCmdPrefixes = []string{
	"go test", "go vet",
	"npm test", "npm run test", "npx jest", "yarn test", "pnpm test",
	"pytest", "python -m pytest", "python3 -m pytest",
	"cargo test", "cargo nextest", "make test",
	"mvn test", "gradle test", "dotnet test", "jest",
}

var buildCmdPrefixes = []string{
	"go build", "npm run build", "yarn build", "pnpm build",
	"cargo build", "make build", "make", "tsc", "mvn package", "gradle build",
}

// claimRe 匹配一行 checklist 描述里的"命令形"片段：反引号内内容，或裸命令前缀。
// 反引号优先（agent 写 checklist 的惯例），裸前缀兜底（无反引号也抓 go test ./...）。
var claimRe = regexp.MustCompile("`([^`]+)`")

// ExtractClaimedCommands 从 checklist 已勾选项文本提取声称执行过的验证类命令。
// 返回 (测试类, 构建类) 两个去重切片（保持出现顺序）。每条声称带少量上下文
// （前缀+首个参数），用于与 toollog 的 Bash 输入做子串匹配。
func ExtractClaimedCommands(items []string) (tests, builds []string) {
	seenT := map[string]bool{}
	seenB := map[string]bool{}
	add := func(list *[]string, seen map[string]bool, cmd string) {
		if !seen[cmd] {
			seen[cmd] = true
			*list = append(*list, cmd)
		}
	}
	for _, text := range items {
		var candidates []string
		for _, m := range claimRe.FindAllStringSubmatch(text, -1) {
			candidates = append(candidates, m[1])
		}
		if len(candidates) == 0 {
			candidates = []string{text}
		}
		for _, c := range candidates {
			if cmd, ok := matchVerifyCommand(c, testCmdPrefixes); ok {
				add(&tests, seenT, cmd)
			} else if cmd, ok := matchVerifyCommand(c, buildCmdPrefixes); ok {
				add(&builds, seenB, cmd)
			}
		}
	}
	return tests, builds
}

// matchVerifyCommand 判断 s 是否含以某已知前缀开头的命令段（&& 链任一段、或
// 整串；cd 包装/环境变量前缀段跳过），返回用于子串匹配的规范片段（前缀 + 紧随
// 的首参数，如 "go test ./..."）。纯文本非命令（无前缀命中）返回 ok=false——
// 描述性文字不构成"声称"。只带首参数：`go test ./internal/foo/...` 与实跑
// `cd x && go test ./internal/foo/... -run Y` 子串可匹配；带多参数反而匹配不到
// （参数顺序/附加 flag 差异）。
func matchVerifyCommand(s string, prefixes []string) (string, bool) {
	for _, seg := range commandSegments(s) {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		for _, p := range prefixes {
			if strings.HasPrefix(seg, p) {
				rest := strings.TrimSpace(seg[len(p):])
				if rest == "" {
					return p, true
				}
				first := strings.Fields(rest)
				return p + " " + first[0], true
			}
		}
	}
	return "", false
}

// commandSegments 把命令文本按 && / ; 拆成候选段并压平空白。声称与实测两侧共用
// （实测 Bash 输入同样拆段），cd 包装与"句子 + && 命令"两种形态都不产生假差集。
func commandSegments(s string) []string {
	s = strings.ReplaceAll(s, ";", "&&")
	raw := strings.Split(s, "&&")
	out := make([]string, 0, len(raw))
	for _, seg := range raw {
		seg = strings.Join(strings.Fields(seg), " ")
		if seg == "" {
			continue
		}
		// 剥段首的连续环境变量赋值前缀（FOO=1 BAR=2 cmd）。
		for {
			f := strings.Fields(seg)
			if len(f) > 1 && isEnvAssign(f[0]) {
				seg = strings.TrimSpace(strings.TrimPrefix(seg, f[0]))
				continue
			}
			break
		}
		out = append(out, seg)
	}
	return out
}

func isEnvAssign(tok string) bool {
	if !strings.Contains(tok, "=") {
		return false
	}
	i := strings.Index(tok, "=")
	return i > 0 && !strings.ContainsAny(tok[:i], "/.-")
}

// SelfReportResult 是 CheckSelfReport 的结构化结果（供 CLI 文案与测试消费）。
type SelfReportResult struct {
	// Checked=false 表示检查未运行（无 checklist / 无勾选 / toollog 缺失 / 逃生舱）。
	Checked bool
	// Blocked=true 表示测试类声称零匹配（fail 形态，task-complete 应拒绝）。
	Blocked bool
	// UnmatchedTests / UnmatchedBuilds 是未在 toollog 找到证据的声称（差集）。
	UnmatchedTests  []string
	UnmatchedBuilds []string
	// TotalClaims 是提取到的验证类声称总数（含已匹配）。
	TotalClaims int
}

// CheckSelfReport is task-complete's self-report consistency pre-flight.
//
// CheckSelfReport 是 task-complete 的自报一致性 pre-flight。比对三段工件 checklist
// 已勾选项声称的验证命令与 toollog 实测 Bash 集（LoadForTaskAll 跨归档，子代理
// 同 TaskRef 的调用也计入）。结果落 checklog CheckSelfReport（pass/warn/fail）。
// toollog 整体无数据（宿主遥测未接）时 Checked=false——区分"无法验证"与"验证
// 通过"，不制造假阳性。
func CheckSelfReport(root string, state *TaskState) SelfReportResult {
	var res SelfReportResult
	if len(state.Checklist) == 0 {
		return res
	}
	if escapeDisabled(state, escapeSelfReport, selfReportDisableEnv) {
		recordAudit(root, &checklog.Entry{
			Check:   checklog.CheckEscapeHatch,
			Passed:  true,
			Checked: true,
			Level:   checklog.LevelWarn,
			TaskRef: state.TaskRef,
			Detail:  `escape-hatch: self-report check bypassed (FORGE_SELF_REPORT=disable)`,
			Meta:    map[string]string{"escape.gate": "self-report", "escape.reason": checklog.EscapeReasonEnv, "escape.owner": "env"},
		})
		return res
	}
	var doneTexts []string
	for _, c := range state.Checklist {
		if c.Done {
			doneTexts = append(doneTexts, c.Desc)
		}
	}
	if len(doneTexts) == 0 {
		return res
	}
	tests, builds := ExtractClaimedCommands(doneTexts)
	if len(tests)+len(builds) == 0 {
		return res // 无验证类声称 → 无可比对（描述性 checklist 不设障）
	}
	// 诚实边界：active 与归档全空 = 宿主 hook 遥测未接，比对无从谈起。探测用
	// ToollogAnyData（含归档）——另一 task start 归档 active 后仅看 active 会误判
	// "遥测未接"而静默跳过（对抗审查 should-fix）。
	if !toolusage.ToollogAnyData(root) {
		return res
	}
	calls, err := toolusage.LoadForTaskAll(root, state.TaskRef)
	if err != nil {
		return res // 读侧失败按"无法验证"处理（fail-open，观察类不设障）
	}
	var bashInputs []string
	for _, c := range calls {
		if c.ToolName == "Bash" || c.ToolName == "bash" {
			bashInputs = append(bashInputs, c.ToolInput)
		}
	}
	res.Checked = true
	res.TotalClaims = len(tests) + len(builds)
	anyTestEvidence := false
	// 测试证据的判定面是"任务全程是否跑过任何测试类命令"，不只限声称命中——
	// 声称 `go test ./...` 而实跑 `go test ./other/ -run A` 是等价命令形态差异
	// （warn 差集），与"从未跑过任何测试"（blocked）是两种事实。
	for _, input := range bashInputs {
		if _, ok := matchVerifyCommand(input, testCmdPrefixes); ok {
			anyTestEvidence = true
			break
		}
	}
	for _, claimed := range tests {
		if !containsSubstring(bashInputs, claimed) {
			res.UnmatchedTests = append(res.UnmatchedTests, claimed)
		}
	}
	for _, claimed := range builds {
		if !containsSubstring(bashInputs, claimed) {
			res.UnmatchedBuilds = append(res.UnmatchedBuilds, claimed)
		}
	}
	// BLOCKED 仅当：有测试类声称未匹配，且任务全程零测试证据——声称测过但从未
	// 跑过任何匹配命令。有部分测试证据时只 warn（等价命令形态差异，advisory）。
	res.Blocked = len(res.UnmatchedTests) > 0 && !anyTestEvidence
	recordSelfReport(root, state, res)
	return res
}

func containsSubstring(haystack []string, needle string) bool {
	for _, h := range haystack {
		if strings.Contains(h, needle) {
			return true
		}
	}
	return false
}

// recordSelfReport 把结果落 checklog（Level：Blocked→fail；有差集→warn；否则 pass）。
func recordSelfReport(root string, state *TaskState, res SelfReportResult) {
	e := &checklog.Entry{
		Check:   checklog.CheckSelfReport,
		Passed:  !res.Blocked && len(res.UnmatchedTests)+len(res.UnmatchedBuilds) == 0,
		Checked: res.Checked,
		TaskRef: state.TaskRef,
	}
	var parts []string
	if len(res.UnmatchedTests) > 0 {
		parts = append(parts, "测试类声称无证据: "+strings.Join(res.UnmatchedTests, "; "))
	}
	if len(res.UnmatchedBuilds) > 0 {
		parts = append(parts, "构建类声称无证据: "+strings.Join(res.UnmatchedBuilds, "; "))
	}
	switch {
	case res.Blocked:
		e.Level = checklog.LevelFail
		e.Detail = "ADVISORY: " + strings.Join(parts, "；") + "——任务 Bash 全程零匹配（inaccurate self-reporting 形态，arXiv 2605.29442）"
	case len(parts) > 0:
		e.Level = checklog.LevelWarn
		e.Detail = "ADVISORY: " + strings.Join(parts, "；") + "（存在其他验证证据，按等价命令差异处理）"
	default:
		e.Level = checklog.LevelPass
		e.Detail = "pass: " + strconv.Itoa(res.TotalClaims) + " 条验证类声称全部有 toollog 证据"
	}
	recordAudit(root, e)
}

// SelfReportEscapeDisabled 暴露逃生舱判定供 CLI 文案提示（与 acceptance/doc gate
// 的导出面同款）。
func SelfReportEscapeDisabled(state *TaskState) bool {
	return escapeDisabled(state, escapeSelfReport, selfReportDisableEnv)
}
