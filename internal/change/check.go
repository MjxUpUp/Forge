package change

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// CheckProblem 是一个"证据不成立"的 change：CI 与 pre-commit 据此拦住合并。
//
// 判据只有一条：这个 change 目录里有待交付的东西，就必须有一份与当前代码和
// 判据一致的封印证据。不成立时给出人能直接照着做的理由——不是"检查失败"。
type CheckProblem struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// CheckProblems 检查未归档 change 的证据是否成立。
//
// onlyTouched 为 nil 时检查全部未归档 change（CI 用）；非 nil 时只检查这些 id
// （pre-commit 只关心暂存区碰到的 change）。返回值 checked 是实际检查过的 id，
// 供调用方区分"都合格"与"没有可检查的东西"。
func CheckProblems(root string, onlyTouched []string) (problems []CheckProblem, checked []string, err error) {
	items, err := List(root)
	if err != nil {
		return nil, nil, err
	}
	only := make(map[string]bool, len(onlyTouched))
	for _, id := range onlyTouched {
		only[id] = true
	}
	for _, c := range items {
		if c.Archive {
			continue
		}
		if onlyTouched != nil && !only[c.ID] {
			continue
		}
		checked = append(checked, c.ID)
		if reason := problemReason(root, c); reason != "" {
			problems = append(problems, CheckProblem{ID: c.ID, Reason: reason})
		}
	}
	return problems, checked, nil
}

// problemReason 返回这个 change 为什么会拦下合并；空表示合格。
//
// 顺序不是随意的：最新一次失败比"证据失效"更接近真相（失效是结果，失败是
// 原因），而"从没封印过"放最后——它是最常见的中间态，不该盖住前两者的信息。
func problemReason(root string, c Change) string {
	verify := "forge change verify " + c.ID
	switch {
	case c.Failed:
		return fmt.Sprintf("最新一次 verify 没通过，证据在 %s/evidence/ 里——先读失败命令，别删证据；修好后重跑 %s",
			relTo(root, c.Dir), verify)
	case c.Stale:
		return "证据已失效：判据或其依赖的代码在封印之后变过——重跑 " + verify
	case !c.Sealed:
		return "没有封印的证据：还没跑过通过的整条验证（产物可能是刚生成的模板）——跑 " + verify
	}
	return ""
}

// HasOutsideChanges 报告暂存区里有没有 spec/changes 之外的路径。
//
// pre-commit 不能只看"碰到的 change"：封印的证据绑定的是代码内容，暂存区里
// 只要出现 change 目录之外的改动，某个已封印 change 的证据就可能已经对不上
// 代码了——只检查被碰到的 change 会放行一个注定在 CI 变红的提交。
func HasOutsideChanges(repoRoot, changesRoot string, paths []string) bool {
	prefix := filepath.ToSlash(relTo(repoRoot, changesRoot)) + "/"
	for _, p := range paths {
		p = filepath.ToSlash(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// TouchedChanges 把仓库相对路径映射到 change id：只认 spec/changes/<id>/…
// 与 spec/changes/archive/<…>（归档不该再被改动，跳过）。
//
// 两个根分开传：git 的路径永远相对仓库根，而 spec/changes 可能落在仓库的
// 子目录里（forge 项目根 ≠ git 根）——用项目根推前缀会把所有路径判成不匹配，
// --staged 于是静默跳过。输入是 git 给出的路径（正斜杠、相对仓库根）；
// 重复与非法 id 直接跳过，pre-commit 不该因为一个临时目录名就把提交按住。
func TouchedChanges(repoRoot, changesRoot string, paths []string) []string {
	prefix := filepath.ToSlash(relTo(repoRoot, changesRoot)) + "/"
	seen := map[string]bool{}
	var out []string
	for _, p := range paths {
		p = filepath.ToSlash(strings.TrimSpace(p))
		rest, ok := strings.CutPrefix(p, prefix)
		if !ok || rest == "" {
			continue
		}
		id, _, _ := strings.Cut(rest, "/")
		if id == "" || id == ArchiveDirName || ValidID(id) != nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
