package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestVersionCommand 钉 `forge version` 子命令：与 --version 同源（rootCmd.Version），
// 且输出同一行格式。dogfood 反馈它曾报 unknown command。
func TestVersionCommand(t *testing.T) {
	if versionCmd.Parent() == nil {
		t.Fatal("version 子命令必须挂在 root 上")
	}
	saved := rootCmd.Version
	rootCmd.Version = "9.9.9 (commit: abc, built: now)"
	defer func() { rootCmd.Version = saved }()

	buf := &bytes.Buffer{}
	rootCmd.SetOut(buf)
	defer rootCmd.SetOut(nil)

	if err := versionCmd.RunE(versionCmd, nil); err != nil {
		t.Fatalf("version RunE: %v", err)
	}
	got := buf.String()
	if got != "forge version 9.9.9 (commit: abc, built: now)\n" {
		t.Fatalf("version 输出不符：%q", got)
	}
	if !strings.HasPrefix(got, "forge version ") {
		t.Fatalf("version 输出前缀不符：%q", got)
	}
}
