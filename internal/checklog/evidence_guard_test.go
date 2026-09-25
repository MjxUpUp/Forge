package checklog

// evidence_guard_test.go — verificationChecks 白名单 ↔ 源声明对照 guard
//（leverage-points-landing.md L2 P1 要求「guard test 钉 roster 与 deterministic map
// 同步」在现行架构下的落点）：白名单里的 taskpipeline 字面量名字（checklog 是叶子
// 包，import taskpipeline 会成环，故 evidence.go 只能写字面量）必须与 taskpipeline
// 源码里的 `CheckName = "..."` 常量声明对齐——字面量拼错/常量改名时这里先炸，
// 而不是白名单静默失配把验证证据降级成观察。
//
// 与 compat 包的 TestAllCheckNamesSorted（types.go ↔ escape.go roster 双向对齐）
// 互补：那边管 checklog 自有常量，这边管跨包字面量引用。

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestVerificationChecksLiteralsDeclared pins every verificationChecks key to a
// real declaration: either a checklog/types.go constant or a taskpipeline
// `CheckName = "..."` constant. A typo'd literal here would silently stop
// feeding evidence strength (fail-safe but a correctness loss for that check).
//
// TestVerificationChecksLiteralsDeclared 钉住 verificationChecks 的每个键都有真实
// 声明：checklog/types.go 常量或 taskpipeline 的 `CheckName = "..."` 常量。这里的
// 字面量拼错会静默停止喂 evidence strength（fail-safe 但该 check 的正确性损失）。
func TestVerificationChecksLiteralsDeclared(t *testing.T) {
	declared := map[string]bool{}
	// checklog/types.go 声明形如 `CheckFoo CheckName = "foo"`；taskpipeline 侧引用
	// 全名 `checklog.CheckName`——正则须兼容两种形态（guard 首跑即抓到首版正则漏
	// 匹配包前缀形态，静默空集 = 虚设）。
	re := regexp.MustCompile(`Check\w+\s+(?:\w+\.)?CheckName\s*=\s*"([\w-]+)"`)
	for _, src := range []string{
		filepath.Join("types.go"),
		filepath.Join("..", "taskpipeline"), // 目录——ReadDir 扫 *.go
	} {
		info, err := os.Stat(src)
		if err != nil {
			t.Fatalf("stat %s: %v", src, err)
		}
		var files []string
		if info.IsDir() {
			entries, err := os.ReadDir(src)
			if err != nil {
				t.Fatalf("readdir %s: %v", src, err)
			}
			for _, e := range entries {
				if !e.IsDir() && filepath.Ext(e.Name()) == ".go" {
					files = append(files, filepath.Join(src, e.Name()))
				}
			}
		} else {
			files = append(files, src)
		}
		for _, f := range files {
			body, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("read %s: %v", f, err)
			}
			for _, m := range re.FindAllStringSubmatch(string(body), -1) {
				declared[m[1]] = true
			}
		}
	}
	if len(declared) < 50 {
		t.Fatalf("源声明扫描异常少: %d（正则或路径漂移？）", len(declared))
	}
	for c := range verificationChecks {
		if !declared[string(c)] {
			t.Errorf("verificationChecks 里的 %q 在 checklog/types.go 与 taskpipeline 源码均无声明——字面量拼错或常量已改名，白名单将静默失配", c)
		}
	}
}
