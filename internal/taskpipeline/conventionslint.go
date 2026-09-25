package taskpipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/conventions"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/toolusage"
)

// conventionslint.go — conventions-profile 层 3 的 task-verify advisory：项目
// 规范档案声明了 lint 命令、本任务有工具遥测（toollog 有记录 = 任务确实动过
// 手）、但任务范围内没有任何 Bash 命令命中该 lint 的签名 → 提醒一次
// （advisory 不阻塞，同 cheat-scan 的噪音定位：机械检测供 review 核查，
// 不替代 agent 判断——lint 可能经 CI/编辑器跑过，toollog 只见宿主 Bash）。
//
// 静默条件（不 fire 也不落 checklog）：FORGE_CONVENTIONS_LINT=disable；无档案
// 或档案无 lint 命令（未采纳/未识别——无从提醒）；toollog 无本任务记录
// （无遥测的宿主上「没跑过」不可判，宁可漏报不误报）。

// lintWrapperTokens 是在取 lint 命令「特征 token」前丢弃的 runner 词：
// `go vet`/`npm run lint`/`cargo clippy` 的语义在第一个非 wrapper token 上
// ——按它匹配，`go test` 才满足不了 `go vet`、`npm run build` 满足不了
// `npm run lint`。
var lintWrapperTokens = map[string]bool{
	"go": true, "npm": true, "pnpm": true, "yarn": true, "npx": true, "bunx": true,
	"cargo": true, "dotnet": true, "mvn": true, "gradle": true, "make": true,
	"sudo": true, "env": true, "python": true, "python3": true, "uv": true,
	"uvx": true, "deno": true, "run": true,
}

// lintSignature 提取 lint 命令的特征 token（第一个非 wrapper 词；全为
// wrapper 时回落首词——如裸 `make`）。对 Bash toollog 记录做子串匹配：
// advisory 启发式，偏精确；偶发子串假阳性只会压掉一次提醒（无害方向）。
func lintSignature(lintCmd string) string {
	tokens := strings.Fields(lintCmd)
	for _, tok := range tokens {
		if !lintWrapperTokens[strings.ToLower(tok)] {
			return tok
		}
	}
	if len(tokens) > 0 {
		return tokens[0]
	}
	return ""
}

// LintCheckOutcome is CheckConventionsLint's verdict: Applicable=false means "nothing to judge" (silent — no checklog row), Applicable=true carries the ran/lint-not-run verdict for both the audit row and the nudge.
//
// LintCheckOutcome 是 CheckConventionsLint 的裁定：Applicable=false 表示
// 「无可判定」（静默——不落 checklog），Applicable=true 携带 跑过/未跑 的
// 判定，供审计行与提醒共用。
type LintCheckOutcome struct {
	Applicable bool
	Ran        bool
	LintCmd    string
	Signature  string
}

// bashCommandOf 从 toollog 的 Bash 行抽取命令文本。ToolInput 是原始
// tool_input JSON（hook.go 记整个 blob——command 与 description 都在），
// 直接对 blob 做子串匹配会让 description 里的「vet the code」满足 `go vet`
// 的签名——审计行不得陈述未发生的事（对抗审查发现 #2）。JSON 解析失败的行
// 回落原始字符串（遗留/异形宿主——对 blob 匹配是旧行为，聊胜于无）。
// 已知漏检（已文档化）：toollog 截断到 500 rune，超长复合命令尾部的 lint
// 可能逃逸——方向是假提醒（advisory、无害），绝不是假「跑过」。
func bashCommandOf(toolInput string) string {
	var fields struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(toolInput), &fields); err == nil && fields.Command != "" {
		return fields.Command
	}
	return toolInput
}

// containsToken 报告 text 是否在**词边界**上含 token（命中两侧的字节不是
// [A-Za-z0-9_-]）：`git log --format=%h` 不得满足 `format` 签名；裸 `lint`
// token 也不被 `golangci-lint` 内部满足（连字符算词字节——那个 lint 命令
// 自己的签名就是完整的 `golangci-lint` token）。纯 Contains 是子串；边界
// 检查不开 regex 依赖就杀掉形近类。
func containsToken(text, token string) bool {
	if token == "" {
		return false
	}
	isWordRune := func(r byte) bool {
		return r == '_' || r == '-' || (r >= '0' && r <= '9') ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
	}
	start := 0
	for {
		i := strings.Index(text[start:], token)
		if i < 0 {
			return false
		}
		i += start
		before, after := byte(0), byte(0)
		if i > 0 {
			before = text[i-1]
		}
		if i+len(token) < len(text) {
			after = text[i+len(token)]
		}
		if !isWordRune(before) && !isWordRune(after) {
			return true
		}
		start = i + len(token)
	}
}

// CheckConventionsLint judges whether the task's Bash history (toollog) shows the profile's lint command having run.
//
// CheckConventionsLint 判定任务 Bash 历史（toollog）里是否出现过档案声明的
// lint 命令。纯判定——落记录与面向 agent 的提醒在 executor 接线处。
func CheckConventionsLint(root string, state *TaskState) LintCheckOutcome {
	if os.Getenv("FORGE_CONVENTIONS_LINT") == "disable" {
		return LintCheckOutcome{}
	}
	profile, err := conventions.LoadProfile(forgedata.DataDirFor(root))
	if err != nil || profile == nil || profile.LintCmd == "" {
		return LintCheckOutcome{}
	}
	calls, err := toolusage.LoadForTask(root, state.TaskRef)
	if err != nil || len(calls) == 0 {
		// 无遥测 = 「没跑过」不可判：宁可漏报不误报（宿主可能根本没接 toollog）。
		return LintCheckOutcome{}
	}
	signature := lintSignature(profile.LintCmd)
	if signature == "" {
		return LintCheckOutcome{}
	}
	ran := false
	for _, c := range calls {
		if c.ToolName == "Bash" && containsToken(bashCommandOf(c.ToolInput), signature) {
			ran = true
			break
		}
	}
	return LintCheckOutcome{Applicable: true, Ran: ran, LintCmd: profile.LintCmd, Signature: signature}
}

// conventionsLintDetail 构造 checklog Detail（executor 审计行的单一真相源
// ——读方绝不解析提醒散文）。
func conventionsLintDetail(o LintCheckOutcome) string {
	if o.Ran {
		return fmt.Sprintf("lint command seen in task Bash history (%s)", o.LintCmd)
	}
	return fmt.Sprintf("declared lint command not seen in task Bash history (%s; signature %q)", o.LintCmd, o.Signature)
}

// recordConventionsLintAudit 在检查可判定时落审计行（Passed = 跑过）。
// 不可判定不落——为每个未采纳项目写「不适用」行是纯噪声。
func recordConventionsLintAudit(root string, state *TaskState, o LintCheckOutcome) {
	if !o.Applicable {
		return
	}
	recordAudit(root, &checklog.Entry{
		Check:   checklog.CheckConventionsLint,
		Passed:  o.Ran,
		Checked: true,
		TaskRef: state.TaskRef,
		Detail:  conventionsLintDetail(o),
	})
}
