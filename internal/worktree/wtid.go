// Package worktree owns the Workspace identity of the Task/Session/Workspace
// triad (multi-task-concurrency design §4, L1).
//
// 包 worktree 承载 Task/Session/Workspace 三元组中的 Workspace 身份
// （multi-task-concurrency 设计 §4，L1）。一个 Workspace = 一个工作树——主检出或某个
// linked git worktree——其 ID（wtid）是 EvalSymlinks 解析后绝对路径的 hash12。
// 绑定存储与归属台账都按 wtid 键控：一切路径形状的东西按构造即机器本地。
package worktree

import (
	"fmt"
	"hash/fnv"
	"path/filepath"
)

// ID derives the workspace identity of root: hash12 (fnv-64a, %012x) of the
// EvalSymlinks-resolved absolute path.
//
// ID 推导 root 的 workspace 身份：EvalSymlinks 解析后绝对路径的 hash12（fnv-64a，
// %012x）。与 forgedata.Key 的路径 hash 同族推导（fnv-64a → 12 hex），生态读起来一
// 致；刻意不用 project key——同一 repo 的所有 worktree 共享 project key 但必须有不同
// wtid，这个区分正是三元组的意义。解析失败（路径消失）回落到清洗后的绝对路径：
// 身份必须是全函数。
func ID(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = filepath.Clean(root)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(abs))
	return fmt.Sprintf("%012x", h.Sum64())
}
