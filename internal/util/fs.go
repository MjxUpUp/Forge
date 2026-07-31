package util

import (
	"io/fs"
	"os"
	"path/filepath"
)

// DirEntryIsDir reports whether a ReadDir entry is a directory, following
// junction/symlink (os.Stat semantics). e.IsDir() is Lstat-based and returns false
// for junction/symlink entries — with link-mode installs and externally managed
// junction skills, most entries under a skills dir ARE links, so Lstat semantics
// silently drop them. Broken links / stat errors → false (safe skip).
// parent is the directory the entry was read from.
//
// DirEntryIsDir 判断 ReadDir 条目是否为目录，跟随 junction/symlink（os.Stat 语义）。
// e.IsDir() 基于 Lstat，对 junction/symlink 条目返回 false——link 安装模式（默认）
// 与外部管理的 junction skill 下，skills 目录里大量条目是 link，Lstat 语义会静默漏掉。
// 断链/stat 错误 → false（安全跳过）。parent 是读取该条目的目录。
func DirEntryIsDir(parent string, e fs.DirEntry) bool {
	if e.IsDir() {
		return true
	}
	info, err := os.Stat(filepath.Join(parent, e.Name()))
	return err == nil && info.IsDir()
}
