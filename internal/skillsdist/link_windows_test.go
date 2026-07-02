//go:build windows

package skillsdist

import "testing"

// TestMakeDirLink_RejectsPercentInPath：mklink /J 经 cmd.exe，% 会被当环境变量扩展。
// 含 % 的路径必须显式拒绝（路径来自用户 flag/env 是自伤向量，但会让 junction 指向
// 意外的展开结果）。
func TestMakeDirLink_RejectsPercentInPath(t *testing.T) {
	err := makeDirLink(`C:\forge-test-target\%USERNAME%\x`, `C:\forge-test-src`)
	if err == nil {
		t.Fatal("makeDirLink should reject path with % (cmd.exe env-var expansion in mklink)")
	}
}
