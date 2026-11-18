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

// hasReservedDirSegment 报告路径的【目录段】里是否有保留设备名。
//
// Windows 的保留名指向设备，不是普通文件：`nul\win.go` 这种「保留名当目录」的路径
// 在常规 API 下不可达，git 的 `\\?\` 视角却能建出来并列出——它永远是幻影，
// 哪怕磁盘上真有一个这样的目录。单文件形态（`aux.go`）不同：那可能是一个真实
// 源码文件，要靠磁盘/HEAD 判真假。
func hasReservedDirSegment(p string) bool {
	parts := strings.Split(filepath.ToSlash(strings.TrimSpace(p)), "/")
	if len(parts) < 2 {
		return false
	}
	for _, seg := range parts[:len(parts)-1] {
		if IsReserved(seg) {
			return true
		}
	}
	return false
}

// IsPhantomPath 判断 p 是否是不会被 git 正常管理的保留名幻影路径。
//
// 三层判定：
//   - 名字是保留名（非 Windows 恒假）；
//   - HEAD 里没有跟踪它（跟踪中的文件被删是真改动，不能吞）；
//   - 目录段是保留名（`nul/win.go`）→ 幻影；单文件保留名（`aux.go`）看磁盘是否存在。
//
// 只按名字过滤会把真实文件一起吞掉：Windows 上用 `\\?\` 前缀可以建出 `aux.go`，
// git 照实列出，而它是真源码——把它当幻影会让工作区被误判为干净（假阴性）。
func IsPhantomPath(root, p string) bool {
	if !IsReserved(p) {
		return false
	}
	if trackedInHead(root, p) {
		return false
	}
	if hasReservedDirSegment(p) {
		return true
	}
	return !existsOnDisk(root, p)
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
	for _, p := range paths {
		// 一行里只要有一个非保留名路径，这行就不可能是「整行幻影」——先判名字，
		// 免得为每个普通路径都 spawn 一次 git（complete advisory 会扫完全部脏行）。
		if !IsReserved(p) || !IsPhantomPath(root, p) {
			return false
		}
	}
	return true
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
