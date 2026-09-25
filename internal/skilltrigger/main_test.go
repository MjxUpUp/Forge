package skilltrigger

import (
	"os"
	"testing"
)

// TestMain redirects the user-level DataDir to an isolated temp dir.
//
// TestMain 把用户级 DataDir 重定向到隔离临时目录。本包条件测试经
// taskpipeline.SaveTaskState/SetActiveTaskRef 写 runtime state——它们经
// forgedata.DataDirFor 解析到 ~/.forge/projects/<key>/；不重定向的话每次跑测试
// 都按临时目录路径 hash 泄漏一个新目录进真实 ~/.forge（2026-08 曾实测累积 50 个）。
// 每进程新建 MkdirTemp 避免跨次、跨包泄漏。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "forge-skilltrigger-datahome-")
	if err != nil {
		panic(err)
	}
	os.Setenv("FORGE_DATA_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir) // defer won't run before os.Exit — clean up explicitly
	os.Exit(code)
}
