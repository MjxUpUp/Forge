package taskpipeline

import (
	"bufio"
	"os"
	"strings"
)

// acceptance_artifact.go —— 产物→验收提取（artifact-chain-workflow.md §4）：把
// spec/plan 产物里的验收标准行编译成 AcceptanceCriterion，走与 --accept 完全
// 相同的实跑/freshness 管道。产物由此成为硬门禁的源码：不写产物提不出验收，
// 写了假产物提出的验收必须真实跑过。
//
// 三种行形态（v1，全机械可判）：
//  1. `Run: <cmd>` / `Expected: <out>` 行对——与 ParseAcceptanceFromPlan 语义逐字一致；
//  2. 列表行 `accept: <cmd> :: <expected>`（`验收:` 同义；裸 `accept: <cmd>` = 只看退出码 0），
//     允许 "- "/"* " 列表前缀；
//  3. ```accept 围栏块：块内每个非空行都是 `<cmd> :: <expected>`。
//
// 边界：fenced 围栏（```/~~~）内的 Run:/Expected: 是代码示例，跳过（沿 plan
// 解析器的防误提语义）；accept 行与 accept 围栏不受普通代码围栏影响（它们
// 本身就是验收的声明形态）。
const (
	acceptLinePrefixEN = "accept:"
	acceptLinePrefixZH = "验收:"
)

// ParseAcceptanceFromArtifactFile reads the artifact file at path and extracts
// its acceptance criteria.
func ParseAcceptanceFromArtifactFile(path string) ([]AcceptanceCriterion, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseAcceptanceFromArtifact(string(data)), nil
}

// ParseAcceptanceFromArtifact extracts acceptance criteria from artifact
// markdown (superset of ParseAcceptanceFromPlan's Run:/Expected: scan).
func ParseAcceptanceFromArtifact(text string) []AcceptanceCriterion {
	var out []AcceptanceCriterion
	var pendingRun string // 上一个 Run: 命令，尚未被 Expected: 配对；""=无待配对
	type fenceState int
	const (
		fenceNone fenceState = iota
		fenceGeneric
		fenceAccept
	)
	fence := fenceNone
	scanner := bufio.NewScanner(strings.NewReader(text))
	// 对齐 plan 解析器的 scanner 纪律：单行上限扩到 1MB + Err 后返回已扫描部分
	//（超长行只丢自身，不吞前面的合法条目）。
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if isFenceMarker(line) {
			// 围栏边界：none→按语言开（accept 专用围栏或普通代码围栏）；已开→关
			//（accept 围栏的闭 marker 是裸 ```，不带语言）。
			if fence == fenceNone {
				if fenceLanguage(line) == "accept" {
					fence = fenceAccept
				} else {
					fence = fenceGeneric
				}
			} else {
				fence = fenceNone
			}
			continue
		}
		switch fence {
		case fenceAccept:
			// accept 围栏内每非空行 = 一条验收声明（裸命令 = 只看退出码 0，
			// 与裸 accept: 行同规）；空行跳过。
			if line != "" {
				out = append(out, parseOneAcceptance(line))
			}
		case fenceGeneric:
			// 普通代码围栏内的 Run:/Expected: 是示例，跳过。
			continue
		case fenceNone:
			switch {
			case strings.HasPrefix(line, `Run:`):
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
			default:
				if cmd, ok := acceptListCommand(line); ok {
					out = append(out, parseOneAcceptance(cmd))
				}
			}
		}
	}
	if pendingRun != "" {
		out = append(out, parseOneAcceptance(pendingRun))
	}
	return out
}

// fenceLanguage returns the info string after the fence marker ("" for plain
// ```/~~~); "accept" marks an acceptance-declaration fence.
func fenceLanguage(markerLine string) string {
	lang := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(markerLine, "```"), "~~~"))
	return lang
}

// acceptListCommand strips the optional "- "/"* " bullet and the accept/验收
// prefix, returning the command part ("cmd :: expected" or bare "cmd").
func acceptListCommand(line string) (string, bool) {
	rest := line
	if strings.HasPrefix(rest, "- ") || strings.HasPrefix(rest, "* ") {
		rest = strings.TrimSpace(rest[2:])
	}
	for _, p := range []string{acceptLinePrefixEN, acceptLinePrefixZH} {
		if strings.HasPrefix(rest, p) {
			return strings.TrimSpace(rest[len(p):]), true
		}
	}
	return "", false
}
