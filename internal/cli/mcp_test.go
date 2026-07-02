package cli

import (
	"testing"
)

// TestMcpServeCommand_Registered：forge mcp serve 必须注册到 root。
// 守护 cobra 初始化——init() 漏 AddCommand 会让 `forge mcp serve` 直接报未知命令，
// 而 MCP 配置里写的正是这个调用，断链后 agent 起不来 server。
func TestMcpServeCommand_Registered(t *testing.T) {
	sub, _, err := rootCmd.Find([]string{"mcp", "serve"})
	if err != nil {
		t.Fatalf("rootCmd.Find(mcp serve): %v（命令未注册）", err)
	}
	if sub == nil || sub.Use != "serve" {
		t.Errorf("Find 返回 Use=%q want serve", func() string {
			if sub != nil {
				return sub.Use
			}
			return ""
		}())
	}

	// mcp 父命令也在
	parent, _, err := rootCmd.Find([]string{"mcp"})
	if err != nil || parent == nil {
		t.Errorf("mcp 父命令未注册: err=%v", err)
	}
}
