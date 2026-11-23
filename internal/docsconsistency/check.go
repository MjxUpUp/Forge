// Package docsconsistency detects whether forge commands referenced by backticks in docs actually exist in the cobra command tree.
//
// Package docsconsistency 检测"文档反引号引用的 forge 命令"是否真实存在于 cobra 命令树。
//
// 两个消费方：
//   - cli/docs_consistency_test.go 守卫 A/B（CI 每次 go test 跑，发现已 drift 的文档）
//   - taskpipeline executor.go task-complete advisory（本地提交前提醒，drift 早发现）
//
// 真相源是 rootCmd 的 cobra 命令树（在 cli 包）。本包不能 import cli（main 依赖，防循环），
// 故用 RegisterCommandTree 回调：cli 包 init 时注入 func(){ return rootCmd }，本包通过
// 回调拿命令树。未注册时 ValidateForgePath 放行（返回 ""），advisory/守卫不误报——
// 保证本包被未注册回调的调用方（如单元测试）使用时不报假 drift。
package docsconsistency

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

var (
	// forgeBacktickRef matches forge command references inside backticks, such as
	// `forge experience accept`. The backtick delimiter excludes prose like
	// `forge 是...` from matching, sharply reducing false positives. The character class
	// also excludes \n: without it the class could span lines and splice two independent
	// code spans across a line break into one phantom reference.
	//
	// forgeBacktickRef 匹配反引号内的 forge 命令引用，如 `forge experience accept`。
	// 反引号限定把散文里的 "forge 是…" 排除在外，大幅减 false positive。字符类同时
	// 排除 \n：否则字符类可跨行，把被换行隔开的两个独立 code span 拼成幻影引用。
	forgeBacktickRef = regexp.MustCompile("`forge ([^`\n]+)`")

	// commandNameRe 描述合法 cobra 命令名（Use 的首词）。非命令 token——占位符 <id>、
	// flag --force、方括号 [--mode]、分隔符 small|medium、中文说明——一律不匹配，
	// 用于在逐级验证时判定"命令路径到此结束，剩下都是参数"。
	commandNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]*$`)

	mu        sync.RWMutex
	cmdTreeFn func() *cobra.Command
	versionFn func() string
)

// RegisterCommandTree registers the callback that returns the rootCmd command tree.
//
// RegisterCommandTree 注册"获取 rootCmd 命令树"的回调。cli 包 init 调用，注入
// func(){ return rootCmd }。打破 cli ↔ taskpipeline 循环：本包不 import cli，
// taskpipeline import 本包调 DriftedInProject，cli import 本包注册回调。
func RegisterCommandTree(fn func() *cobra.Command) {
	mu.Lock()
	defer mu.Unlock()
	cmdTreeFn = fn
}

func commandTree() *cobra.Command {
	mu.RLock()
	defer mu.RUnlock()
	if cmdTreeFn == nil {
		return nil
	}
	return cmdTreeFn()
}

// CommandTree 暴露已注册的命令树（未注册时为 nil）。
//
// cli 在 init 里注入 rootCmd；agentproto 的命令面生成与文档漂移检查都经此拿树，
// 这样 cli ↔ 检查包之间不需要 import 环。
func CommandTree() *cobra.Command { return commandTree() }

// RegisterVersion registers the callback returning the running binary's version — same cycle-breaking pattern as RegisterCommandTree (cli init injects func(){ return rootCmd.Version }).
//
// RegisterVersion 注册「返回运行中二进制版本」的回调——与 RegisterCommandTree 同款
// 破循环模式（cli init 注入 func(){ return rootCmd.Version }）。StaleBinaryHint
// 消费它；未注册时提示降级为无版本号的通用句（永不为空，advisory 始终带版本嫌疑提示）。
func RegisterVersion(fn func() string) {
	mu.Lock()
	defer mu.Unlock()
	versionFn = fn
}

// StaleBinaryHint returns the standard suffix for "command does not exist" drift advisories.
//
// StaleBinaryHint 返回「命令不存在」类 drift advisory 的统一后缀。drift 检查拿文档
// 与【运行中二进制】的命令树比对，故文档引用新版 forge 才有的命令时，本地旧二进制
// 会误报 drift——2026-08 实证：README 引用 `skills mine`（当时存在，命令已于 2026-09 清扫删除）在 PATH 全局
// v1.34.0 旧二进制下触发 advisory。先提示查 forge 版本，省得为真实存在的命令做无谓
// 的文档「修复」。
func StaleBinaryHint() string {
	mu.RLock()
	fn := versionFn
	mu.RUnlock()
	v := ""
	if fn != nil {
		v = fn()
	}
	if v == "" {
		return "；若这些命令在新版 forge 已存在，可能是本地 forge 二进制过旧，先重装（npm i -g @agent_forge/forge；旧 forge update 命令已随 core-only 移除）再排查"
	}
	return fmt.Sprintf("；若这些命令在新版 forge 已存在，可能是本地 forge 版本过旧（当前 %s），先重装 forge 二进制（npm i -g @agent_forge/forge；旧 forge update 命令已随 core-only 移除）再排查", v)
}

// findSub 在 parent 的直接子命令里按 Name 找。cobra Commands() 不展开别名，
// 故文档应使用 canonical 命令名（与 Use 首词一致）。
func findSub(parent *cobra.Command, name string) *cobra.Command {
	if parent == nil {
		return nil
	}
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

// ValidateForgePath validates level by level that the command path exists in the cobra tree.
//
// ValidateForgePath 逐级验证命令路径在 cobra 树中存在。ref 是反引号内 "forge " **之后**
// 的内容（"forge " 前缀已由 forgeBacktickRef 剥离），故直接从 rootCmd 起逐级匹配。
// 遇到非命令 token（< / -- / [ / 中文 等）即停——后面都是参数或说明。
// 返回首个断链的子命令名；空串表示路径完整（含 ref 为空的退化情形）。
// 命令树未注册（回调 nil）时返回 ""（放行，不误报）。
func ValidateForgePath(ref string) string {
	cur := commandTree()
	if cur == nil {
		return ""
	}
	for _, p := range strings.Fields(ref) {
		if !commandNameRe.MatchString(p) {
			break
		}
		if sub := findSub(cur, p); sub != nil {
			cur = sub
		} else {
			return p
		}
	}
	return ""
}

// DriftedCommands scans doc text and returns all forge command paths (the part after `forge `, e.g. `experience propose`) that are referenced in backticks but absent from the command tree.
//
// DriftedCommands 扫文档文本，返回所有反引号引用但命令树中不存在的 forge 命令路径
// （"forge " 之后的部分，如 "experience propose"）。守卫 A 和 task-complete advisory 用。
// 命令树未注册时返回 nil（放行）。
func DriftedCommands(doc string) []string {
	// 去重：同一 drift 命令在文档出现 N 次只报一次，避免 advisory stderr 重复刷
	// "experience propose, experience propose"。
	seen := make(map[string]bool)
	var drifted []string
	for _, m := range forgeBacktickRef.FindAllStringSubmatch(doc, -1) {
		if ValidateForgePath(m[1]) != "" && !seen[m[1]] {
			seen[m[1]] = true
			drifted = append(drifted, m[1])
		}
	}
	return drifted
}

// DriftedInProject scans the user project root README.md and returns forge command references that have drifted.
//
// DriftedInProject 扫用户项目根 README.md，返回 drift 的 forge 命令引用。
// task-complete 门禁 advisory 用——提交前发现 README 引用了不存在的 forge 命令
// （比 CI 守卫更早：本地 complete 时就提醒，不用等 push）。
// 无 README 或命令树未注册时返回 nil（静默，不阻塞 gate）。
func DriftedInProject(root string) []string {
	body, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		return nil
	}
	return DriftedCommands(string(body))
}

// homePathRef matches a ~/ path written in a document: ASCII path characters only, so the match stops at whitespace, backticks, brackets, quotes and CJK punctuation.
//
// homePathRef 匹配文档里写的 ~/ 路径：只收 ASCII 路径字符，遇空白、反引号、括号、引号、
// 中文标点即止。
var homePathRef = regexp.MustCompile(`~/[A-Za-z0-9._/-]+`)

// homePathPlaceholderNext lists characters that, right after a match, mark the path as a template (~/.x/{topic}-…, ~/.x/<run>, ~/.x/*.md, ~/.x/$RUN, ~/.x/…) rather than a concrete reference.
//
// homePathPlaceholderNext 是紧跟在匹配之后、表示路径是模板而非具体引用的字符
// （~/.x/{topic}-…、~/.x/<run>、~/.x/*.md、~/.x/$RUN、~/.x/…）。
const homePathPlaceholderNext = "{<*$…"

// EvidenceHomePrefixes are the ~/ prefixes of locally kept evidence (research run directories). Paths outside them — config files created on demand, other tools' settings, migration history — are not evidence and are not checked.
//
// EvidenceHomePrefixes 是本机留存证据的 ~/ 前缀（调研 run 目录）。前缀之外的路径——按需
// 生成的配置文件、其他工具的设置、迁移史里的旧路径——不是证据，不检查（全量检查实测
// 约九成是这类噪音）。
var EvidenceHomePrefixes = []string{"~/.forge/research/", "~/.research-workflow/"}

// danglingMarkedRe matches a line that already marks its path as gone, so the same dead reference is not reported on every edit.
//
// danglingMarkedRe 匹配已就地标注失效的行——同一条已知死引用不在每次改动时重复报告。
var danglingMarkedRe = regexp.MustCompile(`失效|已不存在|已删除|(?i:no longer exists|removed)`)

// DanglingHomePaths returns the ~/ paths referenced in text that start with one of prefixes (nil = any) and do not exist under home, de-duplicated in first-seen order. Template paths (followed by a placeholder character or containing "...") and paths on a line already marked as gone are skipped; trailing sentence punctuation is trimmed. An empty home returns nil.
//
// DanglingHomePaths 返回 text 中以 prefixes 之一开头（nil = 不限）、在 home 下不存在的 ~/
// 路径（去重、保持首次出现顺序）。模板路径（紧跟占位字符或含 "..."）与所在行已标注失效
// 的路径跳过；句末标点裁掉。home 为空返回 nil。只在本机 task complete 时用——本机路径
// 天然不可移植，不进 CI 与 forge docs lint。
func DanglingHomePaths(text, home string, prefixes []string) []string {
	if home == "" || !strings.Contains(text, "~/") {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, loc := range homePathRef.FindAllStringIndex(text, -1) {
		ref := text[loc[0]:loc[1]]
		if rest := text[loc[1]:]; rest != "" && strings.ContainsAny(firstRune(rest), homePathPlaceholderNext) {
			continue
		}
		if strings.Contains(ref, "...") {
			continue
		}
		ref = strings.TrimRight(ref, ".,;:")
		if ref == "~/" || seen[ref] || !hasAnyPrefix(ref, prefixes) || danglingMarkedRe.MatchString(lineAround(text, loc[0])) {
			continue
		}
		seen[ref] = true
		if _, err := os.Stat(filepath.Join(home, filepath.FromSlash(strings.TrimPrefix(ref, "~/")))); err != nil {
			out = append(out, ref)
		}
	}
	return out
}

// hasAnyPrefix reports whether s starts with one of prefixes; nil prefixes match everything.
//
// hasAnyPrefix 判断 s 是否以 prefixes 之一开头；prefixes 为 nil 时全部匹配。
func hasAnyPrefix(s string, prefixes []string) bool {
	if prefixes == nil {
		return true
	}
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// lineAround returns the full line of text containing byte offset i.
//
// lineAround 返回 text 中包含字节偏移 i 的整行。
func lineAround(text string, i int) string {
	start := strings.LastIndexByte(text[:i], '\n') + 1
	end := strings.IndexByte(text[i:], '\n')
	if end < 0 {
		return text[start:]
	}
	return text[start : i+end]
}

// firstRune returns the first UTF-8 character of s ("" for an empty string).
//
// firstRune 返回 s 的首个 UTF-8 字符（空串返回 ""）。
func firstRune(s string) string {
	for _, r := range s {
		return string(r)
	}
	return ""
}
