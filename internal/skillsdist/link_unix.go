//go:build !windows

package skillsdist

import (
	"fmt"
	"os"
	"path/filepath"
)

// makeDirLink 在 Unix 用 symlink 创建目录符号链接。
func makeDirLink(target, source string) error {
	src, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	tgt, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(tgt), 0755); err != nil {
		return err
	}
	if err := os.Symlink(src, tgt); err != nil {
		return fmt.Errorf("symlink %s -> %s: %w", tgt, src, err)
	}
	return nil
}

// isJunctionOrLink 检测路径是否为 symlink。
func isJunctionOrLink(path string) bool {
	if _, err := os.Readlink(path); err == nil {
		return true
	}
	return false
}
