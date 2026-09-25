package taskpipeline

// assertion_judge.go — spec-as-gate v2 的断言判定分派（leverage-points-landing.md
// L2 P1）：Assertion 的数据形状（P0 批）在这里获得唯一判定实现。五型全机械可判、
// 栈无关、确定性——意见不走 hard（regex/LLM 断言刻意排除，见 tasktypes 断言常量
// 注释）。分派为 verify-acceptance（可见套件）与 VerifyHeldout（保留套件）双套件
// 共用的单一真相源：两处循环都经 runAndJudgeCriterion，不存在第二份判定拷贝。
//
// 五型语义：
//   - exit：命令实际退出码 == Expected 解析值（Expected 缺省 "0"）。
//   - contains / not-contains：合并输出 Contains 子串 / 其否定（not-contains 是
//     反作弊复合断言——如 `go test -v` 输出不得含 "SKIP"）。
//   - file-changed：Arg glob 必须命中本任务变更文件集（TaskChangedFiles——任务窗口
//     语义，含工作树与 untracked；严格 scopeMatchOne 匹配，不用宽松 MatchesScope，
//     契约断言不容假阳性放行）。
//   - file-untouched：Arg glob 不得命中变更集——freeze 语义的任务级持久版（freeze-guard
//     是会话级写时拦截，这里是任务 diff 的事后判定，保护对象含非源码文件如 .md/.yml）。

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
	"gopkg.in/yaml.v3"
)

// CheckNameAcceptanceAssert 是 verify-acceptance 逐断言记的 checklog 条目
// （deterministic）。聚合行 CheckNameAcceptance 保留（兼容既有强度评级与 golden）；
// 本行把判定粒度下钻到单条断言，trace 不展开也能定位是哪条断言挂了。
// 沿 acceptance / test-run / mutation-sampling 先例：taskpipeline 侧定义常量，
// checklog 白名单以字面量引用（checklog 是叶子包，import 本包会成环）。
const CheckNameAcceptanceAssert checklog.CheckName = "acceptance-assert"

// judgeContext carries the per-criterion execution facts the assertion dispatch
// consumes. ran=false means no command executed (Run-less file-* criterion);
// exit assertions on a non-run context fail — an exit code nobody observed
// cannot be asserted.
//
// judgeContext 携带单条 criterion 的执行事实供断言分派消费。ran=false 表示未执行
// 命令（无 Run 的 file-* 条目）；非执行上下文上的 exit 断言判负——没人观测到的
// 退出码无法被断言。
type judgeContext struct {
	ran      bool     // 是否实际执行了命令
	exitCode int      // 实际退出码（RunTestCommandCode；spawn 失败/超时为 -1）
	output   string   // 合并输出（原样，未截断——截断只影响持久化）
	changed  []string // 本任务变更文件集（任务窗口语义，调用方算一次传入）
}

// AssertionVerdict is one assertion's judged outcome — the evidence-row payload.
// Deliberately NOT persisted on the criterion: results live in checklog rows
// and criterion-level Passed; adding result fields would churn the schema and
// pollute assertionsKey (the key is spec identity, not outcome).
//
// AssertionVerdict 是单条断言的判定结果——证据行的载荷。刻意不持久化在 criterion
// 上：结果活在 checklog 行与 criterion 级 Passed 里；加结果字段会搅动 schema 并
// 污染 assertionsKey（键是 spec 身份，不是结果）。
type AssertionVerdict struct {
	CriterionIdx int
	Assertion    Assertion
	Passed       bool
}

// judgeAssertion is the single source of truth for v2 assertion judging. Pure
// function over the execution context; unknown types fail closed (false) —
// declaration paths validate, judging never guesses.
//
// judgeAssertion 是 v2 断言判定的单一真相源。对执行上下文的纯函数；未知类型
// fail-closed 判负——声明通道负责校验，判定绝不负猜测。
func judgeAssertion(a Assertion, ctx judgeContext) bool {
	// 输出消费型统一 fail-closed：没人执行过命令就没有可判的输出——绕过声明门
	// 进来的持久化形态（Run-less 挂输出型断言）不得经 Contains("","") 恒真/恒假
	// 走 vacuous 判定（审查 P3-4：原先只有 exit 检查 ran）。
	if tasktypes.AssertionTypeNeedsRun(a.Type) && !ctx.ran {
		return false
	}
	switch a.Type {
	case tasktypes.AssertionTypeExit:
		want := 0
		if a.Expected != "" {
			v, err := strconv.Atoi(strings.TrimSpace(a.Expected))
			if err != nil {
				return false // 非数字期望 fail-closed（声明期已拒，这里兜底）
			}
			want = v
		}
		return ctx.exitCode == want
	case tasktypes.AssertionTypeContains:
		return strings.Contains(ctx.output, a.Expected)
	case tasktypes.AssertionTypeNotContains:
		return !strings.Contains(ctx.output, a.Expected)
	case tasktypes.AssertionTypeFileChanged:
		return changedMatches(ctx.changed, a.Arg)
	case tasktypes.AssertionTypeFileUntouched:
		return !changedMatches(ctx.changed, a.Arg)
	default:
		return false
	}
}

// changedMatches reports whether any changed file matches the glob. Strict
// scopeMatchOne (exact / dir-prefix / path.Match; no whitelist, no test-file
// derivation: contract assertions must not false-positive), PLUS a trailing
// "/**" globstar translation: path.Match has no globstar, so "a/**" would
// silently cover only one level — the declared protection and the actual
// semantics would diverge (review P2-1: silent under-protection). Trailing
// "/**" is translated to the dir-prefix recursion it obviously means; mid-path
// globstar is rejected at declaration (ValidateAssertion) instead of silently
// mis-judged here.
//
// changedMatches 报告变更集中是否有文件命中 glob。严格 scopeMatchOne（精确/目录
// 前缀/path.Match；无白名单、无测试文件衍生：契约断言不容假阳性），另加尾缀
// "/**" 的 globstar 翻译：path.Match 不支持 **，"a/**" 会静默只覆盖一层——声明
// 的保护面与实际语义背离（审查 P2-1：静默弱保护）。尾缀 "/**" 翻译为它显然
// 意指的目录前缀递归；路径中间的 globstar 在声明期拒绝（ValidateAssertion），
// 不在这里静默误判。
func changedMatches(changed []string, glob string) bool {
	if prefix, ok := strings.CutSuffix(glob, "/**"); ok && prefix != "" {
		for _, f := range changed {
			if scopeMatchOne(prefix, f) {
				return true
			}
		}
		return false
	}
	for _, f := range changed {
		if scopeMatchOne(glob, f) {
			return true
		}
	}
	return false
}

// runAndJudgeCriterion executes the criterion command (unless Run-less with
// file-only assertions) and judges v1 semantics AND each v2 assertion, filling
// Passed/Output and returning per-assertion verdicts. Shared by VerifyAcceptance
// (visible suite) and VerifyHeldout (held-out suite) — the red line is one
// judging implementation, no second copy.
//
// runAndJudgeCriterion 执行 criterion 的命令（无 Run 且断言全为 file-* 时跳过执行），
// 判定 v1 语义 AND 逐条 v2 断言，回填 Passed/Output 并返回逐断言判定。VerifyAcceptance
// （可见套件）与 VerifyHeldout（保留套件）共用——红线是判定实现唯一，不存在第二份拷贝。
func runAndJudgeCriterion(root string, c *AcceptanceCriterion, changed []string) []AssertionVerdict {
	var verdicts []AssertionVerdict
	var output string
	exitOK := true
	ctx := judgeContext{changed: changed}
	if c.Run != "" {
		code, out := RunTestCommandCode(root, c.Run)
		ctx.ran = true
		ctx.exitCode = code
		ctx.output = out
		output = out
		exitOK = code == 0
		c.Output = truncateAcceptanceOutput(output)
	} else if len(c.Assertions) == 0 {
		// v1 边角逐字节保留（审查 P2-2）：空 Run 无断言的存量条目，旧行为是
		// RunTestCommand("") → (false, "empty command") 恒判负——不得因 v2 的
		// 「跳过执行」路径翻成 vacuous pass（fail-open 方向，违背 v1 一致承诺）。
		c.Output = "empty command"
		c.Passed = false
		return nil
	} else {
		// 声明期校验保证走到这里的 Run-less 条目断言全为 file-*（无命令可跑）。
		c.Output = ""
	}
	// exit 断言接管退出码判定（L3 批精化）：声明了 exit 型断言 = 显式声明期望退出码
	// ——隐式 exit==0 检查被断言取代（否则 `sh fail.sh` + `exit: :: 1` 的「期望失败」
	// 形态永远无法整条通过，exit 断言只能陪跑）。Expected 子串判定保留。
	if hasExitAssertion(c.Assertions) {
		exitOK = true
	}
	passed := judgeAcceptance(exitOK, output, c.Expected)
	for _, a := range c.Assertions {
		ok := judgeAssertion(a, ctx)
		verdicts = append(verdicts, AssertionVerdict{Assertion: a, Passed: ok})
		if !ok {
			passed = false
		}
	}
	c.Passed = passed
	return verdicts
}

// hasExitAssertion reports whether the set declares an exit-code assertion —
// an explicit expected exit code replaces the implicit exit==0 check.
//
// hasExitAssertion 报告断言集是否声明了退出码断言——显式期望退出码取代隐式
// exit==0 检查。
func hasExitAssertion(as []Assertion) bool {
	for _, a := range as {
		if a.Type == tasktypes.AssertionTypeExit {
			return true
		}
	}
	return false
}

// ParseAssertion parses one `type:arg :: expected` declaration string into an
// Assertion. Shape errors are returned (caller reports); semantic validation
// (known type, non-empty expected where required) lives in ValidateAssertion so
// persisted-program paths can parse leniently and declaration paths can reject.
//
// ParseAssertion 把一条 `type:arg :: expected` 声明串解析成 Assertion。形状错误返回
// （调用方报告）；语义校验（类型已知、该非空的字段非空）在 ValidateAssertion——
// 持久化程序路径可宽松解析，声明路径负责拒绝。
func ParseAssertion(s string) (Assertion, error) {
	a := Assertion{}
	left, expected, _ := strings.Cut(s, ` :: `)
	a.Expected = strings.TrimSpace(expected)
	typ, arg, _ := strings.Cut(left, `:`)
	a.Type = strings.TrimSpace(typ)
	a.Arg = strings.TrimSpace(arg)
	if a.Type == "" {
		return a, fmt.Errorf("assertion %q 缺类型（格式 \"type:arg :: expected\"，type ∈ exit|contains|not-contains|file-changed|file-untouched）", s)
	}
	return a, nil
}

// ValidateAssertion is the declaration-time gate for one assertion (same spirit
// as clitask.ValidateInvariant: narrative shapes are rejected at declaration,
// not discovered at run time). Rules: known type; Negate reserved-rejected;
// contains/not-contains/exit need a Run and non-empty/numeric Expected; file-*
// need a glob Arg (trailing "/**" only — mid-path globstar rejected); file-*
// Expected must stay empty. The CJK narrative heuristic applies to the
// type:arg segment — Expected is a free substring match exactly like v1
// Expected and may legitimately be Chinese.
//
// ValidateAssertion 是单条断言的声明期门（与 clitask.ValidateInvariant 同精神：
// 叙述性形态在声明时拒绝，不是跑时才发现）。规则：类型已知；Negate 预留即拒绝；
// contains/not-contains/exit 需要 Run 与非空/数字 Expected；file-* 需要 glob Arg
// （仅尾缀 "/**"，路径中间 globstar 拒绝）；file-* 的 Expected 必须为空。CJK 叙述
// 启发式作用于 type:arg 段——Expected 与 v1 Expected 同为自由子串匹配，中文合法。
func ValidateAssertion(a Assertion) error {
	if !tasktypes.ValidAssertionType(a.Type) {
		return fmt.Errorf("未知断言类型 %q（首发五型：exit|contains|not-contains|file-changed|file-untouched；regex 刻意排除——ReDoS + 判定不可机械化）", a.Type)
	}
	if a.Negate {
		return fmt.Errorf("断言 %s:%s 带 negate=true——Negate 是预留字段，首发类型自带反义型（contains/not-contains），声明期拒绝以防静默未知语义", a.Type, a.Arg)
	}
	switch a.Type {
	case tasktypes.AssertionTypeExit:
		if a.Arg != "" {
			return fmt.Errorf("exit 断言不支持 arg（期望退出码写在 :: 右侧，如 \"exit: :: 1\"），got arg=%q", a.Arg)
		}
		if a.Expected == "" {
			return fmt.Errorf(`exit 断言需要期望退出码（如 "exit: :: 0"）`)
		}
		// 范围门（审查 P3-5）：-1 是 spawn 失败/超时的执行层哨兵，声明 -1 会把
		// 「命令没跑起来」判成 PASS；退出码域 0-255。
		code, err := strconv.Atoi(strings.TrimSpace(a.Expected))
		if err != nil {
			return fmt.Errorf("exit 断言的期望 %q 不是整数退出码", a.Expected)
		}
		if code < 0 || code > 255 {
			return fmt.Errorf("exit 断言的期望 %d 超出退出码域 0-255（负值是执行层失败哨兵，不可断言）", code)
		}
	case tasktypes.AssertionTypeContains, tasktypes.AssertionTypeNotContains:
		if a.Arg != "" {
			return fmt.Errorf("%s 断言不支持 arg（期望子串写在 :: 右侧），got arg=%q", a.Type, a.Arg)
		}
		if a.Expected == "" {
			return fmt.Errorf("%s 断言需要期望子串（:: 右侧不能为空）", a.Type)
		}
	case tasktypes.AssertionTypeFileChanged, tasktypes.AssertionTypeFileUntouched:
		if a.Arg == "" {
			return fmt.Errorf("%s 断言需要 glob（如 \"file-untouched:internal/freeze/**\"）", a.Type)
		}
		if a.Expected != "" {
			return fmt.Errorf("%s 断言不消费 expected（glob 写在 type: 右侧即可），got expected=%q", a.Type, a.Expected)
		}
		// globstar 边界（审查 P2-1）：只有尾缀 "/**" 有递归翻译；路径中间的
		// "**"（如 internal/**/x.go）path.Match 不支持、也无翻译——声明期拒绝，
		// 不静默降级成单层匹配造成假保护。
		if strings.Contains(a.Arg, "**") && !strings.HasSuffix(a.Arg, "/**") {
			return fmt.Errorf("%s 断言的 glob %q 含路径中间的 **——仅支持尾缀 \"/**\"（目录递归）；中间 globstar 请改用目录前缀", a.Type, a.Arg)
		}
		if narrativeArg(a.Arg) {
			return fmt.Errorf("%s 断言的 glob %q 看起来是叙述性约束而非文件 glob——断言必须机械可判；叙述性约束请用 forge task checklist add 或 forge task intent 承载", a.Type, a.Arg)
		}
	}
	return nil
}

// ValidateAssertions is the criterion-level declaration gate: every assertion
// passes ValidateAssertion, and the run-binding rules hold — a Run-less
// criterion may carry ONLY file-* assertions (nothing else can be judged
// without executing), and output-consuming assertions require a Run.
//
// ValidateAssertions 是 criterion 级声明门：每条断言过 ValidateAssertion，且
// Run 绑定规则成立——无 Run 的条目只能挂 file-* 断言（不执行命令无从判定其他），
// 消费输出的断言必须有 Run。
func ValidateAssertions(cs []AcceptanceCriterion) error {
	for i := range cs {
		c := cs[i]
		for _, a := range c.Assertions {
			if err := ValidateAssertion(a); err != nil {
				return fmt.Errorf("验收 #%d（%s）的断言 %s:%s：%w", i+1, c.Run, a.Type, a.Arg, err)
			}
			if c.Run == "" && tasktypes.AssertionTypeNeedsRun(a.Type) {
				return fmt.Errorf("无 Run 的验收条目只能挂 file-changed/file-untouched 断言（%s 需要命令执行），got %s", a.Type, a.Type)
			}
		}
		if c.Run == "" && len(c.Assertions) == 0 {
			return fmt.Errorf("验收 #%d 既无 Run 也无断言——空条目不可判定", i+1)
		}
		if c.Run == "" && c.Expected != "" {
			return fmt.Errorf("验收 #%d 无 Run 却带 expected %q——无命令执行的期望子串无从比对", i+1, c.Expected)
		}
	}
	return nil
}

// AttachAssertions binds declared assertions to criteria per the CLI binding
// rule: a non-file-* assertion attaches to the LAST criterion (the preceding
// --accept in the same command); file-* assertions may stand alone — with no
// criteria yet they create Run-less criteria of their own. Returns the
// (possibly extended) slice; caller validates the result.
//
// AttachAssertions 按 CLI 绑定规则把声明的断言挂到条目上：非 file-* 断言挂到
// 最后一条 criterion（同命令中 preceding --accept）；file-* 断言可独立——尚无
// 条目时自建无 Run 条目。返回（可能扩展后的）切片；调用方对结果做校验。
func AttachAssertions(criteria []AcceptanceCriterion, asserts []Assertion) ([]AcceptanceCriterion, error) {
	for _, a := range asserts {
		if tasktypes.AssertionTypeNeedsRun(a.Type) {
			if len(criteria) == 0 {
				return nil, fmt.Errorf("%s 断言须附属于同命令中 preceding --accept 的条目（file-changed/file-untouched 可独立出现）", a.Type)
			}
			last := &criteria[len(criteria)-1]
			last.Assertions = append(last.Assertions, a)
			continue
		}
		// file-*：已有条目也挂最后一条（同一命令的就近绑定）；无条目自建。
		if len(criteria) == 0 {
			criteria = append(criteria, AcceptanceCriterion{Assertions: []Assertion{a}})
		} else {
			last := &criteria[len(criteria)-1]
			last.Assertions = append(last.Assertions, a)
		}
	}
	return criteria, nil
}

// ParseAcceptanceYAML parses an --accept-file document into criteria. Schema
// (keys mirror the persisted JSON tags): top-level `criteria:` list, each entry
// `run` / `expected` / `assertions: [{type, arg, expected, negate}]`. A Run-less
// entry is legal only with file-* assertions (ValidateAssertions enforces).
// YAML is chosen over JSON for hand-authored exam papers (comments, no quote
// noise); yaml.v3 is already a direct dependency.
//
// ParseAcceptanceYAML 把 --accept-file 文档解析成条目。Schema（键与持久化 JSON
// tag 一致）：顶层 `criteria:` 列表，每条 `run` / `expected` /
// `assertions: [{type, arg, expected, negate}]`。无 Run 的条目只在挂 file-* 断言
// 时合法（ValidateAssertions 执法）。选手写考卷用 YAML 而非 JSON（可注释、无引号
// 噪声）；yaml.v3 已是直接依赖。
func ParseAcceptanceYAML(data []byte) ([]AcceptanceCriterion, error) {
	type yamlAssertion struct {
		Type     string `yaml:"type"`
		Arg      string `yaml:"arg"`
		Expected string `yaml:"expected"`
		Negate   bool   `yaml:"negate"`
	}
	type yamlCriterion struct {
		Run        string          `yaml:"run"`
		Expected   string          `yaml:"expected"`
		Assertions []yamlAssertion `yaml:"assertions"`
	}
	var doc struct {
		Criteria []yamlCriterion `yaml:"criteria"`
	}
	// KnownFields(true)（审查 P3-6）：yaml.Unmarshal 默认忽略未知键——拼错
	// `assertions:` 会静默丢掉整个断言集，考卷静默变弱。严格模式让拼错键在
	// 声明期报错，而不是跑时才发现断言没生效。
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("accept-file 解析失败（顶层键 criteria，条目字段 run/expected/assertions[type/arg/expected/negate]；未知键被拒——检查拼写）: %w", err)
	}
	out := make([]AcceptanceCriterion, 0, len(doc.Criteria))
	for _, yc := range doc.Criteria {
		c := AcceptanceCriterion{Run: strings.TrimSpace(yc.Run), Expected: strings.TrimSpace(yc.Expected)}
		for _, ya := range yc.Assertions {
			c.Assertions = append(c.Assertions, Assertion{
				Type:     strings.TrimSpace(ya.Type),
				Arg:      strings.TrimSpace(ya.Arg),
				Expected: strings.TrimSpace(ya.Expected),
				Negate:   ya.Negate,
			})
		}
		out = append(out, c)
	}
	return out, nil
}

// narrativeArg applies the same CJK-dominance heuristic as clitask.ValidateInvariant
// to a file-* glob argument: command/path languages are naturally ASCII-dominant,
// a CJK-majority glob is a narrative constraint in disguise. Expected substrings
// are deliberately NOT checked — matching Chinese tool output is legitimate.
//
// narrativeArg 对 file-* 的 glob 参数施加与 clitask.ValidateInvariant 相同的 CJK
// 主导启发式：命令/路径语言天然 ASCII 主导，CJK 占多数的 glob 是伪装的叙述性约束。
// 刻意不检查期望子串——匹配中文工具输出是合法的。
func narrativeArg(arg string) bool {
	cjk, total := 0, 0
	for _, r := range arg {
		if unicode.Is(unicode.Han, r) {
			cjk++
		}
		if !unicode.IsSpace(r) {
			total++
		}
	}
	return total > 0 && cjk*2 > total
}
