package hookdispatch

// hook_test_main_test.go — 包级测试隔离（审计遗留 #3 根治，2026-09-08）：
// hook 测试 chdir 进带 .forge/ 的 fixture 后，RunHook→checklog.Record→
// DataDirFor(root) 会解析到真实 ~/.forge/projects/<pathkey>/——历史累计 1159 个
// 孤儿项目目录（TestHookOutput_StructuredJSON 等家族）。HOME 与 FORGE_DATA_HOME
// 指向包级共享临时目录后，测试数据零落真实 store。清理脚本见 v1.53.0 发版审计
// 报告（分类规则：PathKey 前缀 p + 未注册/死路径）。

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "hookdispatch-test-*")
	if err != nil {
		os.Exit(1)
	}
	os.Setenv("HOME", tmp)
	os.Setenv("FORGE_DATA_HOME", tmp)
	// 显式清理再退出：os.Exit 跳过 defer，defer os.RemoveAll 是死代码
	// （只读子 agent 审查 SUGGEST-1 顺带根治范式本体）。
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}

// TestTestingDataHomeIsolated 钉死 TestMain 的密闭性契约：包内任何测试执行时
// HOME/FORGE_DATA_HOME 必须已指向非真实用户数据的隔离目录。删掉上面的隔离逻辑、
// 本测试即红。双事故注记：①FORGE_DATA_HOME 裸读真实注册表——home 目录被注册成
// 项目后（2026-09 本机实例），前缀匹配把一切临时目录判成"在项目内"，hook 在测试
// 里开火，测试结果随开发机注册表内容漂移；②HOME 裸读真实用户目录——checklog 经
// DataDirFor 落真实 store（2026-09-08 审计遗留 #3，1159 个孤儿目录）。测试结果
// 不得随开发机状态漂移——「Windows 跑通、换环境就挂」的机制性防线。
func TestTestingDataHomeIsolated(t *testing.T) {
	// Windows 上 USERPROFILE 未被 TestMain 改写，可对照真实 home 判"指向真实数据
	// 目录"；unix 上 HOME 已被改写，该对照退化为恒过——非空检查是主防线，此处
	// 尽力而为不假装更强。
	real := os.Getenv("USERPROFILE")
	for _, key := range []string{"HOME", "FORGE_DATA_HOME"} {
		got := os.Getenv(key)
		if got == "" {
			t.Fatalf("%s 未设置——TestMain 的密闭隔离失效，包内 hook 测试将读取真实用户级状态，结果随开发机漂移", key)
		}
		if real != "" && filepath.Join(got, "projects.json") == filepath.Join(real, ".forge", "projects.json") {
			t.Fatalf("%s=%q 指向真实用户数据目录——隔离失效", key, got)
		}
	}
}
