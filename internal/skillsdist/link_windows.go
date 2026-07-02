//go:build windows

package skillsdist

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// makeDirLink 在 Windows 用 mklink /J 创建目录连接点（junction）。
// 选 junction 而非 symlink：symlink 需 SeCreateSymbolicLinkPrivilege（管理员/开发者模式），
// junction 无需任何特权，且可跨越本地盘符（对齐 sync.py 的 mklink /J）。
// junction 不可跨越网络/UNC 路径——失败时显式报错，调用方提示 --mode copy。
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
	// mklink /J 经 cmd.exe 执行，% 在 cmd 下会被当环境变量扩展（%USERNAME% 等）。
	// 路径来自用户 flag/env（自伤而非远程向量），但含 % 的路径会让 junction 指向
	// 意外的展开结果——显式拒绝并提示，不静默产生错误链接。
	if strings.ContainsAny(src, "%") || strings.ContainsAny(tgt, "%") {
		return fmt.Errorf("path contains '%%' (cmd.exe expands as env var in mklink): src=%s tgt=%s", src, tgt)
	}
	cmd := exec.Command("cmd", "/c", "mklink", "/J", tgt, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mklink /J %s %s: %w (%s)", tgt, src, err, string(out))
	}
	return nil
}

// isJunctionOrLink 检测路径是否为 junction/symlink（reparse point）。
// os.Readlink 对 junction（Go 1.x 起）和 symlink 都成功，对真目录失败。
func isJunctionOrLink(path string) bool {
	if _, err := os.Readlink(path); err == nil {
		return true
	}
	return false
}
