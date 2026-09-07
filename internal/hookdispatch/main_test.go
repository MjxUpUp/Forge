package hookdispatch

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMain 把包内全部测试与开发机的真实用户状态隔离：FORGE_DATA_HOME 未显式设置
// 时指向一次性临时目录。
//
// 根因（2026-09，环境性测试失败类的治本卡点）：hook 测试经 projectroot.Find 读取
// **真实**全局注册表（~/.forge/projects.json）。一旦注册表出现能前缀覆盖系统临时
// 目录的条目（实例：Windows 开发机把用户 home 目录 C:\Users\Administrator 注册成
// 了项目），每个新建的 t.TempDir 都被判成"在项目内"——项目级 hook 在测试里开火、
// 本应静默的 allow 分支带出 stdout。表现为：同一份代码，注册表脏的机器确定性红、
// 干净机器与 CI 绿——「Windows 跑通、换个环境就挂」的一类根因。
//
// 密闭性默认化（一个卡点，而非散落在每个测试里）：新测试天然免疫开发机状态；
// 需要自定义隔离的测试仍可用 t.Setenv(FORGE_DATA_HOME, ...) 覆盖（RealProject
// 的幂等 setenv 语义不变）。临时目录留给 OS 清理，进程只设一次。
func TestMain(m *testing.M) {
	if os.Getenv("FORGE_DATA_HOME") == "" {
		dir, err := os.MkdirTemp("", "forge-hookdispatch-test-data")
		if err != nil {
			fmt.Fprintf(os.Stderr, "TestMain: 创建隔离数据目录失败: %v\n", err)
			os.Exit(1)
		}
		os.Setenv("FORGE_DATA_HOME", dir)
	}
	os.Exit(m.Run())
}

// TestTestingDataHomeIsolated 钉死 TestMain 的密闭性契约：包内任何测试执行时
// FORGE_DATA_HOME 必须已指向非真实用户数据的隔离目录。删掉 TestMain 的隔离逻辑、
// 本测试即红——测试结果不得随开发机注册表内容漂移（2026-09 事故的回归钉）。
func TestTestingDataHomeIsolated(t *testing.T) {
	got := os.Getenv("FORGE_DATA_HOME")
	if got == "" {
		t.Fatal("FORGE_DATA_HOME 未设置——TestMain 的密闭隔离失效，包内 hook 测试将读取真实 ~/.forge 注册表，结果随开发机状态漂移")
	}
	if real, err := os.UserHomeDir(); err == nil && real != "" {
		if filepath.Join(got, "projects.json") == filepath.Join(real, ".forge", "projects.json") {
			t.Fatalf("FORGE_DATA_HOME=%q 指向真实用户数据目录——隔离失效", got)
		}
	}
}
