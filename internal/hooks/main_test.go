package hooks

import (
	"os"
	"testing"
)

// TestMain redirects the user-level DataDir to an isolated temp dir.
//
// TestMain 把用户级 DataDir 重定向到隔离临时目录。本包测试在临时 git 仓库上跑
// hook 流程，多处路径会 MkdirAll DataDir（~/.forge/projects/<key>/）——即使不落
// 条目也会留下空目录污染真实 ~/.forge（2026-08 实测单次全量跑残留 4 个空目录）。
// 每进程新建 MkdirTemp 避免跨次、跨包泄漏。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "forge-hooks-datahome-")
	if err != nil {
		panic(err)
	}
	os.Setenv("FORGE_DATA_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir) // defer won't run before os.Exit — clean up explicitly
	os.Exit(code)
}
