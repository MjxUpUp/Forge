package skilltrigger

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

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
func condSourceChanged(ctx Context) bool {
	if ctx.ProjectRoot == "" {
		return false
	}
	out, err := exec.Command("git", "-C", ctx.ProjectRoot, "status", "--porcelain").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if len(line) < 3 {
			continue
		}
		// porcelain 格式：XY <path>（前 2 字符状态码 + 1 空格 + 路径）；rename 形如 "R  orig -> path"。
		p := line[3:]
		if idx := strings.Index(p, " -> "); idx >= 0 {
			p = p[idx+4:] // rename 取目标路径
		}
		p = strings.Trim(p, `"`)
		if isSourcePath(p) {
			return true
		}
	}
	return false
}

func isSourcePath(p string) bool {
	return sourceExt[strings.ToLower(filepath.Ext(p))]
}

// testCmdSignals 测试命令信号（子串匹配，大小写不敏感）。
var testCmdSignals = []string{
	"go test", "pytest", "python -m pytest", "cargo test",
	"npm test", "npm run test", "yarn test", "pnpm test",
	"mvn test", "gradle test", "jest", "vitest", "mocha",
	"rake test", "deno test", "elm-test", "stack test", "cabal test",
	"dotnet test", "xcodebuild test", "flutter test",
}

// condTestCommandFailed：刚跑的 Bash 是测试命令（command 含测试信号）且失败
//（exit_code≠0 或 interrupted=true）。缺 exit_code → 无法判定 → false（保守不触发）。
func condTestCommandFailed(ctx Context) bool {
	cmd, _ := ctx.ToolInput["command"].(string)
	if cmd == "" {
		return false
	}
	lower := strings.ToLower(cmd)
	isTest := false
	for _, sig := range testCmdSignals {
		if strings.Contains(lower, sig) {
			isTest = true
			break
		}
	}
	if !isTest {
		return false
	}
	if interrupted, ok := ctx.ToolOutput["interrupted"].(bool); ok && interrupted {
		return true
	}
	code := exitCodeOf(ctx.ToolOutput)
	if code < 0 {
		return false // 缺 exit_code，无法判定失败
	}
	return code != 0
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

// codingIntentKeywords 编码意图关键词（中英文）。覆盖"要动手写代码"的意图；
// 纯问问题/读代码（"看看""是什么"）不命中——避免把 advisory 注入泛化到每个 prompt。
var codingIntentKeywords = []string{
	"实现", "实现一下", "实现个", "帮我写", "写一下", "写个", "写一个", "写一段",
	"重构", "refactor",
	"修复", "修一下", "修个", "fix",
	"添加", "新增", "加上", "add ", "implement",
	"开发", "develop", "build", "integrate",
	"编码", "tdd",
}

// condCodingIntent：UserPromptSubmit 的 prompt 含编码意图关键词。
func condCodingIntent(ctx Context) bool {
	if ctx.Prompt == "" {
		return false
	}
	lower := strings.ToLower(ctx.Prompt)
	for _, kw := range codingIntentKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
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
