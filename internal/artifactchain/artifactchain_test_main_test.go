package artifactchain

// artifactchain_test_main_test.go — 包级测试隔离（test-home-leak-sweep，
// 2026-09-09）：artifactchain 测试经 schemaPath（forgedata.DataDirFor(root)
// 下的 schemas/schema.yaml）落用户级 store——root 是 t.TempDir() 时派生
// path-key 目录仍建在真实 GlobalHome 下。FORGE_DATA_HOME 未隔离时，每次全量
// go test 泄 2 个孤儿目录（p* 前缀，仅含 schemas/ 子目录）。HOME 与
// FORGE_DATA_HOME 指向包级共享临时目录后，测试数据零落真实 store。范式同
// hookdispatch/hook_test_main_test.go（审计遗留 #3 根治）。

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "artifactchain-test-*")
	if err != nil {
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)
	os.Setenv("HOME", tmp)
	os.Setenv("FORGE_DATA_HOME", tmp)
	os.Exit(m.Run())
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
