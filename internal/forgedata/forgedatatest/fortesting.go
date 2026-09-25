// Package forgedatatest provides helpers for constructing forgedata.Project in tests.
//
// Package forgedatatest 提供 tests 中构造 forgedata.Project 的辅助函数。
//
// 两个 helper 覆盖两层测试：
//   - ForDataDir：lightweight——直接把 DataDir/GitRoot 指向 dir，无 git、无 ProjectFor。
//     适合 store 单测（act/checklog/hazard 的 Append/Load 纯 IO round-trip），不触发 hash 推导。
//   - RealProject：heavyweight——git init + .forge placeholder + FORGE_DATA_HOME 隔离
//   - 真实 ProjectFor。适合集成测试（cli subprocess / dashboard HTTP），被测代码路径自身
//     会调用 ProjectFor；测试进程与 forge 子进程必须解析到同一 DataDir，这只有真实
//     ProjectFor 能保证。
//
// 仅从 _test.go 文件 import。生产代码必须通过 forgedata.ProjectFor 解析 Project
// （其需真实 .git common dir 推导 key）；store 单测通常不需要，改用 ForDataDir 把
// DataDir 指向 temp dir。
package forgedatatest

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// ForDataDir constructs a Project whose DataDir and GitRoot both point at dir, with a stable fake key; ConfigDir = dir/.forge.
//
// ForDataDir 构造一个 DataDir 与 GitRoot 都指向 dir 的 Project，带稳定 fake key，
// ConfigDir = dir/.forge。Runtime-state store 只会触碰 DataDir，故测试把 t.TempDir()
// 作为 dir 传入，直接读写产出的路径。GitRoot 镜像 DataDir，让任何 git-rooted accessor
// 留在 temp tree 内；ConfigDir 沿用 <cwd>/.forge 约定，但 runtime-state store 不会触它。
func ForDataDir(dir string) *forgedata.Project {
	return &forgedata.Project{
		Key:       "test",
		GitRoot:   dir,
		DataDir:   dir,
		ConfigDir: filepath.Join(dir, ".forge"),
	}
}

// RealProject constructs a real resolvable *Project (git init + .forge placeholder + isolated data home).
//
// RealProject 构造一个真实可解析的 *Project：git init + .forge placeholder
// + FORGE_DATA_HOME 隔离 + ProjectFor。返回 (root, p)：
//   - root 传给 runForge 子进程或仍取 root string 的函数（如 appendConclusion）；
//   - p 传给已迁移到 *Project 签名的 store（如 act.Append(p, ...)）。
//
// 当被测代码路径自身调用 forgedata.ProjectFor（dashboard 聚合内部、forge 子进程）
// 时，RealProject 是必需的：在无 git 的 t.TempDir() 上 ProjectFor 失败，写/读落到不同
// DataDir，测试永远看不到数据。纯 store round-trip（路径中无 ProjectFor）继续用
// ForDataDir。
//
// FORGE_DATA_HOME 按 test 设置（t.Setenv），让 DataDir 落在隔离 temp dir，绝非真实
// ~/.forge；子进程通过 os.Environ 继承，跨进程边界保持写/读对齐。
func RealProject(t *testing.T) (root string, p *forgedata.Project) {
	t.Helper()
	root = t.TempDir()
	// git init 让 ProjectFor 的 Key() 能 hash .git common dir。
	// -C 是 git global flag（必须前置于 subcommand）；"git init -C <dir>"会被
	// git init 拒绝，exit 129（usage error）。
	if err := exec.Command("git", "-C", root, "init").Run(); err != nil {
		t.Fatalf("git init %s: %v", root, err)
	}
	// .forge placeholder 让 findForgeConfigDir 的 walk-up 命中（ProjectFor 要求项目已 init）。
	// runForge init 之后会填充它；空目录不冲突。
	if err := os.MkdirAll(filepath.Join(root, ".forge"), 0o755); err != nil {
		t.Fatalf("mkdir .forge: %v", err)
	}
	// FORGE_DATA_HOME 隔离：每个 test 设置一次（幂等）。同一 test 中多次 RealProject 调用
	// （如全局聚合用 rootA + rootB）必须共享一个 DATA_HOME——否则第二次覆盖第一次，
	// 聚合内部的 ProjectFor(rootA) 解析到与 act.Append(pA) 写入位置不同的
	// DataDir，rootA 的数据消失。不同项目的隔离靠 git-root-derived key
	// （<DATA_HOME>/projects/<key>/），不靠分离的 DATA_HOME。
	if os.Getenv("FORGE_DATA_HOME") == "" {
		t.Setenv("FORGE_DATA_HOME", t.TempDir())
	}
	var err error
	p, err = forgedata.ProjectFor(root)
	if err != nil {
		t.Fatalf("ProjectFor %s: %v", root, err)
	}
	return root, p
}
