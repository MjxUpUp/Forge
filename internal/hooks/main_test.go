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
	// W0.3 密闭性：档位默认 standard（个别档位测试用 t.Setenv 覆盖）——防开发机
	// shell 导出的 FORGE_PROFILE=lite 泄漏进镜像/接线测试造成假红。
	os.Setenv("FORGE_PROFILE", "standard")
	code := m.Run()
	os.RemoveAll(dir) // defer won't run before os.Exit — clean up explicitly
	os.Exit(code)
}
