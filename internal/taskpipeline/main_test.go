package taskpipeline

import (
	"os"
	"testing"
)

// TestMain redirects the user-level DataDir (where checklog, task state, sessions
// and other runtime state now live after the user-level-assets migration) to an
// isolated temp dir. Tests in this package create real git repos via runGit and
// record runtime state; without redirection, the stores would resolve a real
// DataDir under ~/.forge/projects/<key>/ and pollute the developer's machine.
// A fresh MkdirTemp per process avoids cross-run and cross-package leakage.
//
// TestMain 把用户级 DataDir（user-level-assets 迁移后 checklog、task state、
// session 等 runtime state 的所在地）重定向到隔离临时目录。本包测试经 runGit
// 建真实 git 仓库并写 runtime state；不重定向的话 store 会解析到真实
// ~/.forge/projects/<key>/ 污染开发者机器。每进程新建 MkdirTemp 避免跨次、
// 跨包泄漏。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "forge-taskpipeline-datahome-")
	if err != nil {
		panic(err)
	}
	os.Setenv("FORGE_DATA_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir) // defer won't run before os.Exit — clean up explicitly
	os.Exit(code)
}
