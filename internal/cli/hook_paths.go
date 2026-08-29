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
