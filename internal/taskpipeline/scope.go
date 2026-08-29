package taskpipeline

// scope.go 实现 PlanScope 的 advisory 偏差检测：把"开工前声明要改哪些文件"（规划前置）
// 变成可度量契约——实改文件（TaskChangedFiles）与声明的差集 = scope-drift。
//
// 设计依据：
//   - 声明态（PlanScope）vs 实改态（TaskChangedFiles）的差集即 drift——drift 只报告供 review，
//     不阻断执行（advisory），与 desired/actual state 比对同构。
//   - plan-then-execute 是当前主流范式：plan 存机器可读形态供执行，PlanScope []string 是其轻量版。
//   - 变更影响集预测本质是概率问题（业界召回率约 ~44%）：故 scope 当 prediction 而非 contract，
//     drift 是常态信号而非异常——硬拦会误杀约一半合法改动。
//   - 定位是分层、可修正的——故 scope 支持中途追加（task scope add），不一次锁死。
//
// 全程 advisory：MatchesScope/ScopeDrift 只回答"是否覆盖/偏差何在"，不决策是否放行。
// 调用方（hook.go）记 checklog CheckScopeDrift 供 review/看板，绝不阻断工具调用。

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// MatchesScope reports whether file (repo-relative path, any separator) is covered by the declared PlanScope.
//
// MatchesScope 报告 file（仓库相对路径，分隔符任意）是否被声明的 PlanScope 覆盖。
// 空 scope → 处处为 true（无声明 = 无 drift；调用方短路，但本函数保持 total）。
//
// 覆盖判定故意宽松（scope 是 prediction/superset，非 contract）：
//   - 精确路径匹配：scope 条目 == file
//   - 目录前缀递归：internal/cli 或 internal/cli/ 覆盖其下所有文件
//   - path.Match 的 shell glob：internal/cli/*.go、*.go（'*' 不跨 '/'）
//   - 源文件在 scope 内时其测试文件自动覆盖（声明 a.go 覆盖 a_test.go——只规划源
//     即可；对齐 testcoverage 约定）
//   - 生成/类型/入口文件（testcoverage 白名单）始终覆盖——drift 关注真实源码，
//     不看衍生噪声。复用单一白名单真相源。
func MatchesScope(file string, scope []string) bool {
	file = filepath.ToSlash(file)
	if len(scope) == 0 {
		return true
	}
	// 衍生/类型噪声：永不算 drift（复用 testcoverage 白名单——单一真相源）。
	if isWhitelisted(file) {
		return true
	}
	for _, g := range scope {
		if scopeMatchOne(g, file) {
			return true
		}
	}
	// 测试文件：若其约定源文件在 scope 内则视为覆盖。不依赖 isTestFile——该
	// helper（来自 testcoverage）漏掉 Python 的 test_ 前缀约定，会让 Python
	// 测试永不自动覆盖。sourcePathsForTest 对非测试文件返回 nil，故对普通源
	// 是安全 no-op。
	if testSourceInScope(file, scope) {
		return true
	}
	return false
}

// scopeMatchOne 测试单条 scope glob 对一条规范化文件路径的匹配。path.Match
// （非 filepath.Match）是故意选择：它在所有平台上把 '/' 当作唯一分隔符，故 '*'
// 在 Windows 与 POSIX 上行为一致（filepath.Match 会让 '*' 在 Windows 上跨 '/'，
// 使同一 glob 在不同 OS 上拆分不同——潜在的跨平台 drift 检测 bug）。
func scopeMatchOne(glob, file string) bool {
	glob = strings.TrimSpace(glob)
	if glob == "" {
		return false
	}
	if glob == file {
		return true
	}
	// 目录前缀递归：internal/cli / internal/cli/ → 其下全部。
	prefix := strings.TrimSuffix(glob, "/")
	if prefix != "" && strings.HasPrefix(file, prefix+"/") {
		return true
	}
	if ok, err := path.Match(glob, file); err == nil && ok {
		return true
	}
	return false
}

// testSourceInScope 报告测试文件的约定源文件是否被 scope 覆盖。是
// testcoverage.hasMatchingTest 约定（Go/Rust/TS/JS/Python）的逆运算。让单独声明
// 源文件即覆盖其测试，避免规划良好的源改动把自己的测试文件误报为 drift。
func testSourceInScope(testFile string, scope []string) bool {
	for _, src := range sourcePathsForTest(testFile) {
		for _, g := range scope {
			if scopeMatchOne(g, src) {
				return true
			}
		}
	}
	return false
}

// sourcePathsForTest 返回一个测试文件可能覆盖的候选源路径。覆盖常见约定；漏报
// 是保守侧（测试随即计为 drift），绝不会反向静默假阳性。
func sourcePathsForTest(testFile string) []string {
	f := filepath.ToSlash(testFile)
	dir := path.Dir(f)
	name := path.Base(f)
	// ext includes the leading dot, e.g. `.go`.
	ext := path.Ext(name) // 含前导点 '.'
	if ext == "" {
		return nil
	}
	var cands []string
	join := func(n string) string {
		if dir == "." {
			return n
		}
		return dir + "/" + n
	}
	// foo_test.go → foo.go  (Go/Rust/Zig/Nim 约定)
	if strings.HasSuffix(name, "_test"+ext) {
		cands = append(cands, join(strings.TrimSuffix(name, "_test"+ext)+ext))
	}
	// foo.test.ts → foo.ts；foo.spec.jsx → foo.jsx  (TS/JS 约定)
	for _, marker := range []string{".test", ".spec"} {
		if strings.Contains(name, marker+ext) {
			cands = append(cands, join(strings.Replace(name, marker+ext, ext, 1)))
		}
	}
	// test_foo.py → foo.py  (仅 Python——test_ 前缀是 Python 约定；按 ext 限定
	// 避免对 Go 文件如 test_config.go 误命中：它们不是测试、只是恰好以 test_ 开头)。
	if ext == ".py" && strings.HasPrefix(name, "test_") {
		cands = append(cands, join(strings.TrimPrefix(name, "test_")))
	}
	return cands
}

// ScopeDrift returns source files in changed that are not covered by scope — an
// advisory drift signal.
//
// ScopeDrift 返回 changed 中未被 scope 覆盖的源文件——advisory drift 信号。空
// scope → nil（无声明 → 无 drift）。只算真实源文件（isSourceFile）：源任务中碰
// README/.yaml/config 是探索、不是 drift。源在 scope 内的测试文件不计。顺序保留；
// 不去重（调用方可能已传 set）。
func ScopeDrift(changed, scope []string) []string {
	if len(scope) == 0 {
		return nil
	}
	var drift []string
	for _, f := range changed {
		if !isSourceFile(f) {
			continue
		}
		if !MatchesScope(f, scope) {
			drift = append(drift, filepath.ToSlash(f))
		}
	}
	return drift
}

// ChangedFiles returns files changed within this task — commits after the task's
// HeadCommit (captured at start) plus the working tree.
//
// ChangedFiles 返回本 task 内变更的文件——自 task 的 HeadCommit（开始时捕获）
// 之后的 commit 加上工作树。是 testcoverage.taskChangedFiles 的导出门面，让
// CLI（task scope show）复用与 test-coverage、scoring 同一份 git-diff 真相源，
// 不做二次推导。
func ChangedFiles(root string, state *TaskState) []string {
	return taskChangedFiles(root, state)
}

// scopeDriftDetail 为 scope-drift 判定生成简明的 checklog detail（单行——
// checklog Detail 是摘要，非面向用户的 stderr 消息）。
func scopeDriftDetail(drift []string) string {
	if len(drift) == 0 {
		return "all changed source files within declared PlanScope"
	}
	if len(drift) > 5 {
		return fmt.Sprintf("scope-drift: %d files out-of-scope: %s ...", len(drift), strings.Join(drift[:5], ", "))
	}
	return "scope-drift: out-of-scope: " + strings.Join(drift, ", ")
}
