package scoring

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// assertionCollectWarningPrefix is the stderr marker for a failed changed-files
// collection — one greppable shape shared with taskpipeline's git-diff warning.
//
// assertionCollectWarningPrefix 是 changed-files 采集失败的 stderr 标记——与
// taskpipeline 的 git diff 告警共享同一可 grep 形态。
const assertionCollectWarningPrefix = "[forge] warning:"

// assertionMarkers is the cross-language "assertion" marker substring. Each
// occurrence counts as one assertion (not a precise density, only a fake-test
// signal: a test file with zero hits = setup/log only, no real coverage).
// Industry basis: STREW's Assertion-McCabe ratio measures test sufficiency by
// assertion count.
//
// Deliberately lenient: t.Fatal covers Fatal/Fatalf, t.Error covers
// Error/Errorf (prefix match) to avoid double counting. Density runs high but
// suffices as a "zero-assertion" signal—the goal is to catch fake tests, not
// to measure precisely.
//
// assertionMarkers 是跨语言的"断言"标记子串。出现一次即计一次断言（非精确密度，仅作
// 假测试检测信号——有测试文件但 0 命中 = 只有 setup/log 无断言，不是真覆盖）。
// 业界依据：STREW 的 Assertion-McCabe ratio 用断言数度量测试充分性。
//
// 刻意宽松：t.Fatal 覆盖 Fatal/Fatalf，t.Error 覆盖 Error/Errorf（前缀匹配），避免
// 重复计数。密度数值偏高但作为"是否零断言"的信号足够——目的是抓假测试，非精确度量。
var assertionMarkers = []string{
	// Go language: testing + testify + panic.
	//
	// Go 语言：testing + testify + panic
	`t.Fatal`, `t.Error`, `require.`, `assert.`, `panic(`,
	// JS/TS language: jest / vitest / node:assert.
	//
	// JS/TS 语言：jest / vitest / node:assert
	`expect(`, `toEqual`, `toBe(`, `toThrow`, `strictEqual`, `should(`,
	// Python language: unittest / pytest.
	//
	// Python 语言：unittest / pytest
	`self.assert`, `pytest.raises`,
	// Rust language.
	//
	// Rust 语言
	`assert!`, `assert_eq!`, `assert_ne!`,
}

// CollectAssertionDensity tallies assertion-marker totals and test-file counts
// for this task's changed test files, feeding the testing dimension's fake-test
// detection (grade C).
//
// Reads the "current content" of test files (all assertions, not just newly
// added)—a changed test file's full assertion set contributes to that file's
// test sufficiency; counting only new assertions would miss pre-existing valid
// ones and understate sufficiency. Non-fatal on per-file read failure (skipped,
// not counted). COLLECTION failure (all git probes dead — non-git dir,
// unreachable base) returns (0, 0) with a stderr warning: the caller cannot
// distinguish "no test files" from "probe dead" through the count pair, so the
// warning is the contract — scoreTesting additionally requires testFiles>0
// before applying the ×0.6 fake-test penalty, which makes a dead probe skip
// the penalty instead of punishing the task for files it never saw
// (fix/cleanup-batch, 2026-08-29).
//
// CollectAssertionDensity 统计本任务 changed 测试文件的断言标记总数和测试文件数，
// 供 testing 维度的假测试检测用（C）。
//
// 读测试文件的"当前内容"（全量断言，非仅本次新增）——一个被改动的测试文件，其全部
// 断言都贡献该文件的测试充分性；只数新增断言会漏掉已存在的有效断言，低估充分性。
// 单文件读失败非致命（跳过、不计入）。【采集】失败（全部 git 探测死掉——非 git
// 目录、base 不可达）返回 (0, 0) 并打 stderr 警告：调用方无法从计数对区分「无测试
// 文件」与「探测死了」，警告即契约——scoreTesting 另外要求 testFiles>0 才应用
// ×0.6 假测试惩罚，使死探测跳过惩罚、而非为从未见过的文件惩罚任务
// （fix/cleanup-batch，2026-08-29）。
func CollectAssertionDensity(root, branch, baseCommit string) (count, testFiles int) {
	base := resolveDiffBase(root, branch, baseCommit)
	files, err := changedFiles(root, base)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s assertion-density collection failed (%v) — fake-test penalty skipped (testing dimension not punished on a dead probe)\n", assertionCollectWarningPrefix, err)
		return 0, 0
	}
	for _, f := range files {
		if !isTestPath(f) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil {
			continue // 读失败跳过，不计入 testFiles（避免假惩罚）
		}
		testFiles++
		count += countAssertions(string(data))
	}
	return count, testFiles
}

// countAssertions tallies occurrences of all assertion markers in content
// (summed across markers).
//
// countAssertions 统计 content 中所有断言标记出现次数（多 marker 求和）。
func countAssertions(content string) int {
	n := 0
	for _, m := range assertionMarkers {
		n += strings.Count(content, m)
	}
	return n
}
