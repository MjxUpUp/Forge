package ghostpath

import "strings"

// porcelainPaths 取出一行 `git status --porcelain` 里的路径（重命名取两侧）。
//
// porcelain v1 的格式是固定两列状态 + 一个空格 + 路径；重命名/复制是
// `old -> new`。这里只做形态提取，不做转义还原——保留名判定不依赖转义。
func porcelainPaths(line string) []string {
	if len(line) < 4 {
		return nil
	}
	rest := strings.TrimSpace(line[3:])
	if rest == "" {
		return nil
	}
	if i := strings.Index(rest, " -> "); i >= 0 {
		return []string{strings.Trim(rest[:i], "\""), strings.Trim(rest[i+4:], "\"")}
	}
	return []string{strings.Trim(rest, "\"")}
}

// LineHasReservedPath 判断一行 git status 是否涉及 Windows 保留名路径。
//
// 工作区状态探测里出现 `?? nul/` 这类幻影目录时，整行都不该算「工作区有改动」：
// 它既不是源码也不是文档，只是文件系统的事实噪声（git 能写 index，Win32 API 建不出来）。
// 判定走 IsReserved，所以在非 Windows 上恒为 false——`aux.go` 之类正常文件不受影响。
func LineHasReservedPath(line string) bool {
	for _, p := range porcelainPaths(line) {
		if IsReserved(p) {
			return true
		}
	}
	return false
}

// FilterReserved 过滤掉保留名路径，保持其余顺序不变。
func FilterReserved(paths []string) []string {
	out := paths[:0:0]
	for _, p := range paths {
		if IsReserved(p) {
			continue
		}
		out = append(out, p)
	}
	return out
}
