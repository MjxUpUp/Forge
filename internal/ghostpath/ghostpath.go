package ghostpath

import (
	"path/filepath"
	"runtime"
	"strings"
)

// reservedDeviceNames 是 Windows 上的保留设备名。它们无法用普通 Win32 API 创建，
// 但能借 `\\?\` 前缀（以及 git 直接写 index/树的路径）出现——评审的文件枚举若照单
// 全收，就会把 `nul/win.go` 这种幻影路径当成源码树，进而让审查快照与工作区状态
// 对不上（实测：删掉幻影目录后 gate 报「审查通过后源码变更」）。
var reservedDeviceNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"conin$": true, "conout$": true,
	"com0": true, "com1": true, "com2": true, "com3": true, "com4": true,
	"com5": true, "com6": true, "com7": true, "com8": true, "com9": true,
	"lpt0": true, "lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true,
	"lpt5": true, "lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// IsReserved 判断 p 的某一段是否是 Windows 保留设备名。
//
// 只在 Windows 上返回 true：其他平台上 `aux.go`、`internal/aux/helpers.go` 是完全
// 正常的源码文件名，过滤它们会让审查快照漏掉真实改动（fail-closed 的门禁就会
// fail-open）。`\\?\` 幻影路径也只可能出现在 Windows。
func IsReserved(p string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	return HasReservedSegment(p)
}

// HasReservedSegment 只看路径形态、不看平台，便于测试与文档说明。
// 判定按「去掉扩展名后的段名」比较：CON、con.txt、NUL 都算。
func HasReservedSegment(p string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(p), "/") {
		if seg == "" || seg == "." || seg == ".." {
			continue
		}
		// `\\?\E:\x\nul \win.go` 这类尾随空格变体：Windows 会忽略尾随空格。
		seg = strings.TrimRight(seg, " ")
		if i := strings.IndexByte(seg, '.'); i >= 0 {
			seg = seg[:i]
		}
		if reservedDeviceNames[strings.ToLower(seg)] {
			return true
		}
	}
	return false
}
