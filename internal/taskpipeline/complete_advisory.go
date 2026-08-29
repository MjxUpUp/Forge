package taskpipeline

// complete_advisory.go 实现 task-complete 的两项 advisory 检查：
//  (a) 分支归属——feature 分支尚未合入主干时完成任务 = 「完成」不等于「交付」；
//  (b) 目标↔产出粗匹配——任务标题与实改文件零关键词交集，是交付错内容的信号。
// 两者都是 advisory（永不阻断）：误报必须被静默吸收，不能拦合法 complete。

import (
	"errors"
	"os/exec"
	"strings"
)

// resolveMainlineRef 在 main 存在时返回 "main"，否则 "master"，再否则 ""（无主干
// ref——调用方 fail-open）。经 rev-parse --verify --quiet 判定，缺失的 ref 只体现
// 在退出码上，不产生 stderr 噪声。
func resolveMainlineRef(root string) string {
	for _, cand := range []string{"main", "master"} {
		if exec.Command("git", "-C", root, "rev-parse", "--verify", "--quiet", cand).Run() == nil {
			return cand
		}
	}
	return ""
}

// uncommittedChanges 返回工作区变更数（已暂存 + 未暂存 + 未跟踪）：
// determinable=false 表示 git 探测本身失败，调用方必须 fail-open——探测失败
// 绝不能编造出一条 advisory。
func uncommittedChanges(root string) (count int, determinable bool) {
	out, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count, true
}

// branchMergedInto 返回 (merged, determinable)：determinable=false 表示 git 探测
// 本身失败（无法判定仓库状态），调用方必须 fail-open——只有 merge-base
// --is-ancestor 干净的 exit 1 才表示「确定未合入」。
func branchMergedInto(root, branch, mainline string) (merged, determinable bool) {
	err := exec.Command("git", "-C", root, "merge-base", "--is-ancestor", branch, mainline).Run()
	if err == nil {
		return true, true
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return false, true
	}
	return false, false
}

// goalKeywords splits a task title into a set of lowercase ASCII words of >=4 chars.
// Splitting is on non-alphanumeric bytes; CJK text acts as a separator and pure-CJK
// tokens are skipped — no segmentation dependency is introduced (a title like
// "修复executor门禁" still contributes "executor").
//
// goalKeywords 把任务标题切成小写 ASCII 词集合（>=4 字符）。按非字母数字字节切分；
// 中文充当分隔符、纯中文词直接跳过——不引入分词依赖（如 "修复executor门禁" 仍
// 贡献 "executor"）。
func goalKeywords(title string) map[string]bool {
	words := map[string]bool{}
	var cur []byte
	flush := func() {
		if len(cur) >= 4 {
			words[strings.ToLower(string(cur))] = true
		}
		cur = cur[:0]
	}
	for i := 0; i < len(title); i++ {
		c := title[i]
		if c < 128 && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			cur = append(cur, c)
		} else {
			flush()
		}
	}
	flush()
	return words
}

// asciiWordTokens 把 s 按非字母数字字节切成小写 token（不限长度——>=4 的约束在
// 标题侧；交集相等性自然过滤）。
func asciiWordTokens(s string) []string {
	var out []string
	var cur []byte
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 128 && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			cur = append(cur, c)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// pathSegmentKeywords collects lowercase word tokens from the path segments of
// changed files (extension stripped) and PlanScope globs (glob metacharacters
// stripped). Each segment is further split on non-alphanumeric boundaries, so
// 'internal/taskpipeline/executor_test.go' yields {internal, taskpipeline, executor,
// test} — splitting (vs whole segments) widens the match surface, and a wider match
// surface means FEWER advisories, which is the safe direction for an advisory-only
// check (宁缺毋滥：误报宁可放过).
//
// pathSegmentKeywords 从变更文件的路径 segment（去扩展名）与 PlanScope glob（去 glob
// 元字符）收集小写词 token。每个 segment 再按非字母数字边界细分，故
// 'internal/taskpipeline/executor_test.go' 产出 {internal, taskpipeline, executor,
// test}——细分（相对整段）扩大匹配面，匹配面越广 advisory 越少，对 advisory-only
// 检查这是安全方向（宁缺毋滥：误报宁可放过）。
func pathSegmentKeywords(files []string, scope []string) map[string]bool {
	words := map[string]bool{}
	addPath := func(p string) {
		p = strings.ReplaceAll(p, "\\", "/")
		for _, seg := range strings.Split(p, "/") {
			seg = strings.Map(func(r rune) rune {
				switch r {
				case '*', '?', '[', ']', '{', '}':
					return -1
				}
				return r
			}, seg)
			if i := strings.LastIndex(seg, "."); i > 0 {
				seg = seg[:i]
			}
			for _, w := range asciiWordTokens(seg) {
				words[w] = true
			}
		}
	}
	for _, f := range files {
		addPath(f)
	}
	for _, s := range scope {
		addPath(s)
	}
	return words
}

// hasIntersection 报告两个词集合是否有至少一个共同词。
func hasIntersection(a, b map[string]bool) bool {
	for w := range a {
		if b[w] {
			return true
		}
	}
	return false
}
