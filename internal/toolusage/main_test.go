package toolusage

import (
	"os"
	"testing"
)

// TestMain redirects the user-level DataDir (where toollog and its archives now
// live after the user-level-assets migration) to an isolated temp dir. Store
// tests in this package call Record/Clear directly; without redirection,
// toolusage.Record would resolve a real DataDir under ~/.forge/projects/<key>/
// and pollute the developer's machine. A fresh MkdirTemp per process avoids
// cross-run and cross-package leakage. Tests needing a fully private home
// (datahome_git_test.go) still override with t.Setenv.
//
// TestMain 把用户级 DataDir（user-level-assets 迁移后 toollog 及其归档的所在地）
// 重定向到隔离临时目录。本包 store 测试直接调 Record/Clear；不重定向的话
// toolusage.Record 会解析到真实 ~/.forge/projects/<key>/ 污染开发者机器。
// 每进程新建 MkdirTemp 避免跨次、跨包泄漏。需要完全私有 home 的测试
// （datahome_git_test.go）仍可用 t.Setenv 覆盖。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "forge-toolusage-datahome-")
	if err != nil {
		panic(err)
	}
	os.Setenv("FORGE_DATA_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir) // defer won't run before os.Exit — clean up explicitly
	os.Exit(code)
}
