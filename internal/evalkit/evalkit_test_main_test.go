package evalkit

// evalkit_test_main_test.go — 包级测试隔离（test-home-leak-sweep，2026-09-09）：
// telemetry/runner/golden 测试经 checklog.Record(t.TempDir(), ...) 落用户级
// store——Record 的 root 只决定 path-key，存储位置始终是 DataDirFor 解析的
// GlobalHome。FORGE_DATA_HOME 未隔离时，每次全量 go test 泄 3 个孤儿目录
// （p* 前缀 path-key，单文件 checklog.jsonl，本机累计 171 个孤儿目录的增量主源
// 之一）。HOME 与 FORGE_DATA_HOME 指向包级共享临时目录后，测试数据零落真实
// store。范式同 hookdispatch/hook_test_main_test.go（审计遗留 #3 根治）。

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "evalkit-test-*")
	if err != nil {
		os.Exit(1)
	}
	os.Setenv("HOME", tmp)
	os.Setenv("FORGE_DATA_HOME", tmp)
	// 显式清理再退出：os.Exit 跳过 defer，defer os.RemoveAll 是死代码
	// （只读子 agent 审查 SUGGEST-1，连范式 hookdispatch 一并根治）。
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}

// TestTestingDataHomeIsolated 钉死 TestMain 的密闭性契约：包内任何测试执行时
// HOME/FORGE_DATA_HOME 必须已指向非真实用户数据的隔离目录。删掉上面的隔离逻辑、
// 本测试即红。测试结果不得随开发机状态漂移（同 hookdispatch 契约测试）。
func TestTestingDataHomeIsolated(t *testing.T) {
	// Windows 上 USERPROFILE 未被 TestMain 改写，可对照真实 home 判"指向真实数据
	// 目录"；unix 上 HOME 已被改写，该对照退化为恒过——非空检查是主防线，此处
	// 尽力而为不假装更强。
	real := os.Getenv("USERPROFILE")
	for _, key := range []string{"HOME", "FORGE_DATA_HOME"} {
		got := os.Getenv(key)
		if got == "" {
			t.Fatalf("%s 未设置——TestMain 的密闭隔离失效，包内测试将写真实用户级 store，结果随开发机漂移", key)
		}
		if real != "" && filepath.Join(got, "projects.json") == filepath.Join(real, ".forge", "projects.json") {
			t.Fatalf("%s=%q 指向真实用户数据目录——隔离失效", key, got)
		}
	}
}
