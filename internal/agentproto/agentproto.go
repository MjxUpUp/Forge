// Package agentproto 生成 agent 投递面（AGENTS.md 托管块）里可以从命令树机械导出的部分。
//
// 为什么要生成而不是手写：手写协议块会随二进制漂移——命令删了块里还留着，命令改了
// 块里还是旧的，而读块的 agent 会照着漂移的协议干活。这里把两件事变成机械事实：
//
//  1. 命令面附录：只列**有 agent 语义注解**的命令；注解与命令树双向校验。
//  2. 块头指纹：<!-- forge:proto version=… sha=… -->，sha 覆盖正文（不含本行，否则自指）。
//
// 散文本身仍然手写（它教判断，不教事实）——生成物只负责「说的命令还在不在」。
package agentproto

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Command 是一条命令在投递面上的表面（路径 + flag 名，均不含前导 --）。
type Command struct {
	Path  string
	Flags []string
}

const (
	// AppendixTitle 是命令面附录的标题；测试与 --check 靠它定位。
	AppendixTitle = "## 命令面（生成）"
	// ProtoOpen 是块头指纹行的前缀。
	ProtoOpen = "<!-- forge:proto version="
	// shaLen 是指纹长度：12 位十六进制，够区分又不喧宾夺主。
	shaLen = 12
)

var (
	protoLine = regexp.MustCompile(`^<!-- forge:proto version=(\S+) sha=([0-9a-f]+) -->$`)
	forgeSpan = regexp.MustCompile("forge ([^`\\n]+)")
	cmdToken  = regexp.MustCompile("^[a-zA-Z][a-zA-Z0-9-]*$")
	flagToken = regexp.MustCompile("^--([a-z][a-z0-9-]*)$")
)

// SHA 返回正文的短指纹（前 12 位十六进制）。
func SHA(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])[:shaLen]
}

// Stamp 在正文开头放/替换 proto 行，返回完整块体（不含 begin/end 标记）。
//
// 幂等：正文里已有的 proto 行先摘掉再重算，故同一正文 + 同一版本永远得到同一结果。
func Stamp(body, version string) string {
	// 版本号必须是单个 token：proto 行是 `version=(\S+) sha=…` 的一条可解析记录，
	// 而 cli 注入的版本串带空格与括号（如 `v3.0.1-1-gabc (commit: …, built: …)`）。
	// 不归一化的话块头永远解析不回来，--check 会把「格式不对」误报成「块陈旧」。
	if f := strings.Fields(version); len(f) > 0 {
		version = f[0]
	} else {
		version = "dev"
	}
	lines := strings.Split(body, "\n")
	for i, ln := range lines {
		if protoLine.MatchString(strings.TrimSpace(ln)) {
			lines = append(lines[:i], lines[i+1:]...)
			break
		}
	}
	clean := strings.TrimSpace(strings.Join(lines, "\n"))
	head := fmt.Sprintf("%s%s sha=%s -->", ProtoOpen, version, SHA(clean))
	if clean == "" {
		return head
	}
	return head + "\n\n" + clean
}

// ParseProto 从一段文本里找出第一个 proto 行。
func ParseProto(text string) (version, sha string, ok bool) {
	for _, ln := range strings.Split(text, "\n") {
		if m := protoLine.FindStringSubmatch(strings.TrimSpace(ln)); m != nil {
			return m[1], m[2], true
		}
	}
	return "", "", false
}

// RenderAppendix 生成「命令面（生成）」附录：只列 Annotations 里有注解的命令。
//
// 注解指向的命令不在树里是硬错误——那正是手写块最常见的漂移形态（命令删了、
// 注解还留着），让它显式失败，而不是悄悄渲染一份读不到的列表。
func RenderAppendix(cmds []Command) (string, error) {
	if len(Annotations) == 0 {
		return "", nil
	}
	known := make(map[string]Command, len(cmds))
	for _, c := range cmds {
		known[c.Path] = c
	}
	paths := make([]string, 0, len(Annotations))
	for p := range Annotations {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var missing []string
	for _, p := range paths {
		if _, ok := known[p]; !ok {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		return "", fmt.Errorf("agentproto: 注解指向的命令不在命令树里（命令已删或改名？）：%s", strings.Join(missing, "、"))
	}

	var b strings.Builder
	b.WriteString(AppendixTitle + "\n\n")
	b.WriteString("> 本节由 `forge change agents` 从运行中的命令树生成，不要手改。\n")
	b.WriteString("> 只列有 agent 语义注解的命令；注解在 internal/agentproto/annotations.go。\n")
	b.WriteString("> 块头 `forge:proto` 行的 sha 覆盖正文（不含本行）。\n\n")
	for _, p := range paths {
		fmt.Fprintf(&b, "- `forge %s` — %s\n", p, Annotations[p])
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// Check 校验散文对命令树的引用，返回问题清单（空 = 无漂移）。
//
// 两类问题：
//   - 幻影命令：`forge …` 的路径段在树里不存在（命令删了/改名了，散文还在教它）；
//   - 幽灵 flag：散文里跟随命令的 --flag 在整个树里都不存在。
//
// flag 只做**全局**存在性检查，不追究它挂在哪条命令上——散文里一处 flag 常被多种
// 入口共用，追究归属会造出一堆误报，而误报会让这条检查失去信誉。
func Check(cmds []Command, prose string) []string {
	byPath := make(map[string]Command, len(cmds))
	for _, c := range cmds {
		byPath[c.Path] = c
	}
	knownFlags := map[string]bool{}
	for _, c := range cmds {
		for _, f := range c.Flags {
			knownFlags[f] = true
		}
	}

	var issues []string
	seen := map[string]bool{}
	for _, m := range forgeSpan.FindAllStringSubmatch(prose, -1) {
		span := strings.TrimSpace(m[1])
		toks := strings.Fields(span)
		// 首个 token 不是 ASCII 命令名，说明这是散文里提到 forge（如「由 forge 维护」），
		// 不是命令引用——不追究，否则整段中文都会被当成幻影命令。
		if len(toks) == 0 || !cmdToken.MatchString(toks[0]) {
			continue
		}
		path := ""
		rest := toks
		for i, t := range toks {
			if strings.HasPrefix(t, "--") || !cmdToken.MatchString(t) {
				rest = toks[i:]
				break
			}
			cand := t
			if path != "" {
				cand = path + " " + t
			}
			if _, ok := byPath[cand]; !ok {
				rest = toks[i:]
				break
			}
			path = cand
			rest = toks[i+1:]
		}
		if path == "" {
			key := "cmd:" + span
			if !seen[key] {
				seen[key] = true
				issues = append(issues, fmt.Sprintf("幻影命令：`forge %s` 在命令树里不存在", span))
			}
			continue
		}
		for _, t := range rest {
			fm := flagToken.FindStringSubmatch(t)
			if fm == nil {
				continue
			}
			if !knownFlags[fm[1]] {
				key := "flag:" + fm[1]
				if !seen[key] {
					seen[key] = true
					issues = append(issues, fmt.Sprintf("幽灵 flag：--%s（出现在 `forge %s`）在整棵命令树里都不存在", fm[1], path))
				}
			}
		}
	}
	return issues
}
