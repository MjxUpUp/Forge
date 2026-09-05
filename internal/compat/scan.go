package compat

// scan.go — 阻断位点源扫描与 schema 种子接缝（mechanism-hardening P1-1 的
// 面 5/6 支撑）。种子接缝经函数变量注入，防 compat import taskpipeline/
// toolusage 的依赖环。

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// blockingRe 匹配 Go 源里的阻断位点调用（GateBlocked( / LevelBlocked 显式使用；
// LevelFail 不算——advisory 体系里的 fail 不一定是门禁阻断）。
var blockingRe = regexp.MustCompile(`GateBlocked\(|checklog\.LevelBlocked`)

// scanBlockings 扫 internal/ 下 Go 源（非测试）的阻断位点，返回 file→count
// 排序列表。确定性：目录遍历排序 + 行序。
func scanBlockings(root string) ([]BlockingSite, error) {
	var out []BlockingSite
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // best-effort：单目录读失败跳过（快照面刻意的容错边界）
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		count := 0
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if blockingRe.MatchString(sc.Text()) {
				count++
			}
		}
		if count > 0 {
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				rel = path
			}
			out = append(out, BlockingSite{File: filepath.ToSlash(rel), Count: count})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out, nil
}

// schema 种子接缝（函数变量——cli 侧注入真实现，防 compat import
// taskpipeline/toolusage 的依赖扩张；未注入时返回 nil 使对应 schema 面为空——
// compat 包测试钉注入完成，cli 测试钉非 nil）。
var (
	// SeedTaskStateForSchema 返回全填充 TaskState（键集合提取用）。
	SeedTaskStateForSchema = func() any { return nil }
	// SeedToolCallForSchema 返回全填充 ToolCall。
	SeedToolCallForSchema = func() any { return nil }
)
