package cli

import (
	"path/filepath"
)

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// toRelPath converts an absolute file path to a project-root-relative, slash-separated
// relative path. This way, patterns like .forge/* in shell scripts match correctly regardless of OS path format.
// On failure, returns the input unchanged.
// toRelPath returns the path of absPath relative to root, slash-separated. Both inputs are symlink-resolved first:
// on macOS, paths like t.TempDir() directories arrive via symlinks
// (/var/folders/... -> /private/var/folders/...), while findProjectRoot's
// os.Getwd() returns the physical form and tool_input's file_path arrives in symlink form.
// Without resolving both sides, filepath.Rel would cross the link boundary and produce ../../... paths that no longer
// match the hook glob patterns (.forge/*, .claude/settings*) — this is the root cause of task-guard uniquely
// failing to block .forge/state.json writes on macOS.
//
// toRelPath 把绝对文件路径转换为以 project root 为基准、用正斜杠分隔的相对
// 路径。这样 shell 脚本里的 .forge/* 等模式才能无视 OS 路径格式正确匹配。
// 转换失败时原样返回。
// toRelPath 返回 absPath 相对 root 的路径、用正斜杠分隔。两个入参都先做
// symlink 解析：在 macOS 上，类似 t.TempDir() 目录的路径会经由 symlink 到达
// （/var/folders/... → /private/var/folders/...），而 findProjectRoot 用的
// os.Getwd() 返回 physical 形式，tool_input 的 file_path 却以 symlink 形式
// 到达。不先解析两侧，filepath.Rel 会跨 link 边界产出 ../../... 路径，不再
// 匹配 hook glob 模式（.forge/*、.claude/settings*）——这是 task-guard 在
// macOS 上独有地拦不住 .forge/state.json 写入的根因。
func toRelPath(root, absPath string) string {
	if root == "" || absPath == "" {
		return absPath
	}
	root = resolveSymlinks(root)
	absPath = resolveSymlinks(absPath)
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return filepath.ToSlash(absPath)
	}
	return filepath.ToSlash(rel)
}

// resolveSymlinks resolves symlinks on path. If path does not yet exist (e.g. a PreToolUse
// Write target before the file is created), it walks UP to the longest existing ancestor
// directory, resolves that, and joins the not-yet-existing tail back — so a new file deep
// under non-existent directories still gets the physical prefix (macOS /var symlinks;
// Windows 8.3 short names, where EvalSymlinks also expands ADMINI~1 → Administrator).
// The one-level climb was enough when .forge/ always existed, but after
// user-level-assets (zero project writes) .forge/ typically does NOT exist — the tail
// is two segments (`.forge/state.json`) and one level no longer reaches an existing
// dir, which silently broke the .forge/* self-protection glob (task-guard approved
// .forge/state.json writes). When no segment of path can be resolved,
// returns path unchanged, preserving the original fallback behavior on symlink-free systems.
//
// resolveSymlinks 对 path 求值 symlink。若 path 尚不存在（例如 PreToolUse
// Write 目标在文件创建之前），向上爬到最长已存在祖先目录、解析之、再把未存在的
// 尾部拼回——让深层新文件也能拿到 physical 前缀（macOS /var symlink；Windows
// 8.3 短名，EvalSymlinks 同时会把 ADMINI~1 展开为 Administrator）。爬一级在
// .forge/ 恒存在时够用，但 user-level-assets（零项目写入）后 .forge/ 通常不存在，
// 尾部有两段（`.forge/state.json`），一级上爬够不到已存在目录——曾静默击穿
// .forge/* 自保护 glob（task-guard 放行 .forge/state.json 写入）。当 path 上没有
// 任何可解析段时原样返回，保留无 symlink 系统上原有的 fallback 行为。
func resolveSymlinks(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	// Walk up to the longest existing ancestor, collecting the unresolved tail.
	//
	// 向上爬到最长已存在祖先，沿途收集未解析的尾部。
	var tail []string
	d := path
	for {
		parent := filepath.Dir(d)
		if parent == d {
			return path // 到卷根仍不可解析——原样返回
		}
		tail = append([]string{filepath.Base(d)}, tail...)
		d = parent
		if resolved, err := filepath.EvalSymlinks(d); err == nil {
			for _, seg := range tail {
				resolved = filepath.Join(resolved, seg)
			}
			return resolved
		}
	}
}
