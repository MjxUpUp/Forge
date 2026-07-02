package scoring

import (
	"os"
	"path/filepath"
	"strings"
)

// assertionMarkers 是跨语言的"断言"标记子串。出现一次即计一次断言（非精确密度，仅作
// 假测试检测信号——有测试文件但 0 命中 = 只有 setup/log 无断言，不是真覆盖）。
// 业界依据：STREW 的 Assertion-McCabe ratio 用断言数度量测试充分性。
//
// 刻意宽松：t.Fatal 覆盖 Fatal/Fatalf，t.Error 覆盖 Error/Errorf（前缀匹配），避免
// 重复计数。密度数值偏高但作为"是否零断言"的信号足够——目的是抓假测试，非精确度量。
var assertionMarkers = []string{
	// Go: testing + testify + panic
	`t.Fatal`, `t.Error`, `require.`, `assert.`, `panic(`,
	// JS/TS: jest / vitest / node:assert
	`expect(`, `toEqual`, `toBe(`, `toThrow`, `strictEqual`, `should(`,
	// Python: unittest / pytest
	`self.assert`, `pytest.raises`,
	// Rust
	`assert!`, `assert_eq!`, `assert_ne!`,
}

// CollectAssertionDensity 统计本任务 changed 测试文件的断言标记总数和测试文件数，
// 供 testing 维度的假测试检测用（C）。
//
// 读测试文件的"当前内容"（全量断言，非仅本次新增）——一个被改动的测试文件，其全部
// 断言都贡献该文件的测试充分性；只数新增断言会漏掉已存在的有效断言，低估充分性。
// 非致命：读失败/无测试文件 → (0, 0)，testing 维度按比例分正常算（不因收集失败崩）。
func CollectAssertionDensity(root, branch, baseCommit string) (count, testFiles int) {
	base := resolveDiffBase(root, branch, baseCommit)
	for _, f := range changedFiles(root, base) {
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

// countAssertions 统计 content 中所有断言标记出现次数（多 marker 求和）。
func countAssertions(content string) int {
	n := 0
	for _, m := range assertionMarkers {
		n += strings.Count(content, m)
	}
	return n
}
