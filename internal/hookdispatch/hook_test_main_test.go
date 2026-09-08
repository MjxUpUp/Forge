package hookdispatch

// hook_test_main_test.go — 包级测试隔离（审计遗留 #3 根治，2026-09-08）：
// hook 测试 chdir 进带 .forge/ 的 fixture 后，RunHook→checklog.Record→
// DataDirFor(root) 会解析到真实 ~/.forge/projects/<pathkey>/——历史累计 1159 个
// 孤儿项目目录（TestHookOutput_StructuredJSON 等家族）。HOME 与 FORGE_DATA_HOME
// 指向包级共享临时目录后，测试数据零落真实 store。清理脚本见 v1.53.0 发版审计
// 报告（分类规则：PathKey 前缀 p + 未注册/死路径）。

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "hookdispatch-test-*")
	if err != nil {
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)
	os.Setenv("HOME", tmp)
	os.Setenv("FORGE_DATA_HOME", tmp)
	os.Exit(m.Run())
}
