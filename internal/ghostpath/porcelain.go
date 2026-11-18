package ghostpath

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

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

// existsOnDisk 报告仓库内相对路径在磁盘上是否存在（目录也算存在）。
func existsOnDisk(root, p string) bool {
	p = strings.TrimSuffix(strings.TrimSpace(p), "/")
	if p == "" {
		return false
	}
	_, err := os.Lstat(filepath.Join(root, filepath.FromSlash(p)))
	return err == nil
}

// trackedInHead 报告 path 是否已提交在 HEAD 里（一次 ls-tree）。
func trackedInHead(root, p string) bool {
	cmd := exec.Command("git", "-C", root, "ls-tree", "-r", "--name-only", "-z", "HEAD", "--", filepath.FromSlash(p))
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\x00") {
		if strings.TrimSpace(line) != "" {
			return true
		}
	}
	return false
}

// IsPhantomPath 判断 p 是否是「git 报出来、但磁盘上不存在、也不在 HEAD 里」的保留名路径。
//
// 只按名字过滤会把真实文件一起吞掉：Windows 上用 `\\?\` 前缀可以建出 `aux.go`，
// git 照实列出，而它是真源码——把它当幻影会让工作区被误判为干净（假阴性）。
// 只看磁盘也不够：删掉一个已提交的 `aux.go` 后它同样「不在磁盘上」，但那是真改动
// （假阴性第二形态）。所以三个条件都要：名字是保留名、磁盘没有、HEAD 里也没有。
func IsPhantomPath(root, p string) bool {
	if !IsReserved(p) || existsOnDisk(root, p) {
		return false
	}
	return !trackedInHead(root, p)
}

// LineHasReservedPath 判断一行 git status 是否【整行】都是不存在的保留名路径。
//
// 一行里只要有一个路径不是保留名、或某个保留名路径在磁盘上真实存在，这一行就是
// 真改动，必须保留。重命名 `R old -> new` 只有两侧都缺失才算幻影。
func LineHasReservedPath(root, line string) bool {
	paths := porcelainPaths(line)
	if len(paths) == 0 {
		return false
	}
	reserved := false
	for _, p := range paths {
		if IsReserved(p) {
			reserved = true
		}
		if existsOnDisk(root, p) || trackedInHead(root, p) {
			return false
		}
	}
	return reserved
}

// FilterReserved 过滤掉幻影路径，保持其余顺序不变。
func FilterReserved(root string, paths []string) []string {
	out := paths[:0:0]
	for _, p := range paths {
		if IsPhantomPath(root, p) {
			continue
		}
		out = append(out, p)
	}
	return out
}
