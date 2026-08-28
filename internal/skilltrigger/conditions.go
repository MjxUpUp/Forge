package skilltrigger

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/MjxUpUp/Forge/internal/attribution"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// Conditions 是 condition 词汇注册表。key = trigger.when 值，value = 判定函数（纯逻辑，
// 外部状态经 Context 传入）。与 internal/skillsqa.ValidConditions 必须保持同步——
// drift 守卫 TestValidConditions_MatchEngine 断言两者 key 集合一致。
//
// Conditions is the condition vocabulary registry. key = trigger.when, value = pure
// predicate (external state passed via Context). Must stay in sync with
// internal/skillsqa.ValidConditions (drift guard TestValidConditions_MatchEngine).
var Conditions = map[string]func(Context) bool{
	"source_changed_uncommitted": condSourceChanged,
	"test_command_failed":        condTestCommandFailed,
	"coding_intent":              condCodingIntent,
	"task_active_no_review":      condTaskActiveNoReview,
	"skill_file_touched":         condSkillFileTouched,
}

// sourceExt 源码扩展名白名单（与 auto-compile.sh 的 is_source case-glob 对齐）。
var sourceExt = map[string]bool{
	".go": true, ".rs": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".mjs": true, ".cjs": true, ".py": true, ".java": true, ".rb": true, ".zig": true,
	".nim": true, ".c": true, ".cc": true, ".cpp": true, ".h": true, ".hpp": true,
	".cs": true, ".kt": true, ".swift": true, ".scala": true,
}

// condSourceChanged：git 工作区有未提交源码（命中源码扩展名白名单）。
// 非 git / 无源码变更 / 无 project root → false（优雅降级）。
//
// L3 归属过滤（multi-task-concurrency §6，T3）：命中条件收紧为「本 session 台账触碰集
// ∩ 当前变更源码集」非空——同目录另一窗口的 WIP 不再触发本窗口的 Stop 纪律提示（原
// 全树扫描是跨任务噪音的根源，用户实测痛点 P1）。降级链：无 session id（无身份宿主）
// 或 FORGE_ATTRIBUTION=0 → 回落旧全树行为（advisory 宁多勿漏）。
func condSourceChanged(ctx Context) bool {
	if ctx.ProjectRoot == "" {
		return false
	}
	changed, err := attribution.ChangedFiles(ctx.ProjectRoot)
	if err != nil {
		return false
	}
	if sid := ctx.SessionID; sid != "" && attribution.Enabled() {
		touched := attribution.SessionTouched(ctx.ProjectRoot, sid)
		fmt.Printf("[DBG] sid=%q changed=%v touched-keys=%v\n", sid, changed, touched)
		for _, p := range changed {
			if touched[filepath.ToSlash(filepath.Clean(p))] && isSourcePath(p) {
				return true
			}
		}
		return false
	}
	for _, p := range changed {
		if isSourcePath(p) {
			return true
		}
	}
	return false
}

func isSourcePath(p string) bool {
	return sourceExt[strings.ToLower(filepath.Ext(p))]
}

// testCmdRe 测试命令信号（word-boundary 正则，大小写不敏感）。\b 防 "cargo test" 含
// "go test"、"lngo test"/"go testbed" 误判——要求信号前后是非单词字符（命令首部或
// 空格/管道/&&/; 分隔）。
//
// testCmdRe matches test-command signals with word boundaries (case-insensitive).
// \b prevents "cargo test" matching "go test" or "lngo test" false positives.
var testCmdRe = regexp.MustCompile(`(?i)\b(go test|python -m pytest|pytest|cargo test|npm run test|npm test|yarn test|pnpm test|mvn test|gradle test|jest|vitest|mocha|rake test|deno test|elm-test|stack test|cabal test|dotnet test|xcodebuild test|flutter test)\b`)

// condTestCommandFailed：刚跑的 Bash 是测试命令（command 含测试信号）且失败
// （exit_code≠0 或 interrupted=true）。缺 exit_code（部分宿主如 kimi 不带该字段）
// → 降级为输出文本的失败签名判定（failSignatureRe）；连失败签名也没有 → false
// （保守不触发）。
func condTestCommandFailed(ctx Context) bool {
	cmd, _ := ctx.ToolInput["command"].(string)
	if cmd == "" {
		return false
	}
	if !testCmdRe.MatchString(cmd) {
		return false
	}
	if interrupted, ok := ctx.ToolOutput["interrupted"].(bool); ok && interrupted {
		return true
	}
	code := exitCodeOf(ctx.ToolOutput)
	if code >= 0 {
		return code != 0
	}
	// 缺 exit_code：降级扫输出文本的失败签名（go test 的 --- FAIL/FAIL、pytest 的
	// FAILED、npm ERR!、panic、非零 exit status）。签名按行首锚定，防输出里引用
	// "fail" 一词的误报。
	return failSignatureRe.MatchString(outputTextOf(ctx.ToolOutput))
}

// failSignatureRe 输出文本失败签名（行首锚定；大小写敏感——go test 摘要行 FAIL、
// pytest FAILED 天然大写，小写 error 等常见于成功输出中的无关词，不收录）。
var failSignatureRe = regexp.MustCompile(`(?m)^(--- FAIL|FAIL|FAILED|npm ERR!|panic: |exit status [1-9][0-9]*|Compilation failed|测试失败|编译失败)`)

// outputTextOf 拼接 tool_output 的文本槽位（output/stdout/stderr）。
func outputTextOf(out map[string]any) string {
	var b strings.Builder
	for _, k := range []string{"output", "stdout", "stderr"} {
		if s, ok := out[k].(string); ok {
			b.WriteString(s)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// condSkillFileTouched：PreToolUse Write/Edit 的目标是 SKILL.md（skill 行为契约）。
// 兼容 file_path 与 path 两键（kimi 的文件类工具用 path，经 remap 后两者同值）。
// 驱动 skill-authoring-standard 在编辑时加载——改 SKILL.md 还受 skill-decisions
// guardrail 硬门禁约束，须记 decisions.md。
func condSkillFileTouched(ctx Context) bool {
	p, _ := ctx.ToolInput["file_path"].(string)
	if p == "" {
		p, _ = ctx.ToolInput["path"].(string)
	}
	return strings.HasSuffix(strings.ToLower(p), "skill.md")
}

// exitCodeOf 从 tool_output 提取 exit_code（兼容多种字段命名）。缺失返 -1（无法判定）。
func exitCodeOf(out map[string]any) int {
	for _, k := range []string{"exit_code", "exitCode", "code", "status"} {
		if v, ok := out[k]; ok {
			if n, err := toInt(v); err == nil {
				return n
			}
		}
	}
	return -1
}

func toInt(v any) (int, error) {
	switch n := v.(type) {
	case float64:
		return int(n), nil
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case string:
		return strconv.Atoi(strings.TrimSpace(n))
	}
	return 0, strconv.ErrSyntax
}

// codingIntentCN 中文编码意图关键词（子串匹配——中文多音节，子串误判风险低）。
// codingIntentENRe 英文编码意图词（word-boundary 正则）——英文短词（fix/build/add）必须
// \b 防 "prefix" 含 "fix"、"lumber build workflow" 含 "build" 等误判把 advisory 注入每个
// 含这些短词的解释性问题。
//
// codingIntentCN: Chinese coding-intent keywords (substring match; CJK multi-syllable
// keeps false positives low). codingIntentENRe: English words with word boundaries —
// short words (fix/build/add) need \b to avoid "prefix" matching "fix".
var codingIntentCN = []string{
	"实现", "帮我写", "写一下", "写个", "写一个", "写一段",
	"重构", "修复", "修一下", "修个", "添加", "新增", "加上",
	"开发", "编码",
}

var codingIntentENRe = regexp.MustCompile(`(?i)\b(refactor|fix|add|implement|develop|build|integrate|tdd)\b`)

// condCodingIntent：UserPromptSubmit 的 prompt 含编码意图关键词（中文子串 + 英文 word-boundary）。
func condCodingIntent(ctx Context) bool {
	if ctx.Prompt == "" {
		return false
	}
	lower := strings.ToLower(ctx.Prompt)
	for _, kw := range codingIntentCN {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return codingIntentENRe.MatchString(ctx.Prompt)
}

// condTaskActiveNoReview：当前 session 有 active task 且 ReviewPassed=false（task 流程中未审）。
// 与 review-stop 区别：review-stop 在 task 模式下 PASS 放行（审查由 task-complete 门禁强制），
// 本 condition 是 advisory 注入（驱动 skill 加载，不 block）；仅 task 流程内有效。
func condTaskActiveNoReview(ctx Context) bool {
	if ctx.ProjectRoot == "" {
		return false
	}
	sid := ctx.SessionID
	if sid == "" {
		sid = taskpipeline.CurrentSessionID()
	}
	state, err := taskpipeline.ActiveTaskState(ctx.ProjectRoot, sid)
	if err != nil || state == nil {
		return false
	}
	return !state.ReviewPassed
}
