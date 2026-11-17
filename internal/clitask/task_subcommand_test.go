package clitask

// task_subcommand_test.go — 已删除的门禁命令不能「静默成功」：task/eval 父命令在
// 收到未知子命令时必须返回错误（review-ruthless-3 的 P1-3）。

import (
	"io"
	"strings"
	"testing"
)

func TestRoot_UnknownSubcommandErrors(t *testing.T) {
	Root.SetOut(io.Discard)
	Root.SetErr(io.Discard)
	Root.SetArgs([]string{"artifact"})
	defer Root.SetArgs(nil)
	err := Root.Execute()
	if err == nil {
		t.Fatal("forge task artifact 已删除，未知子命令必须报错而不是 exit 0 打印帮助")
	}
	if !strings.Contains(err.Error(), "unknown command") && !strings.Contains(err.Error(), "未知的 task 子命令") {
		t.Fatalf("错误文案应指明未知子命令，got: %v", err)
	}
}

func TestRoot_NoArgsShowsHelp(t *testing.T) {
	Root.SetOut(io.Discard)
	Root.SetErr(io.Discard)
	Root.SetArgs([]string{})
	defer Root.SetArgs(nil)
	if err := Root.Execute(); err != nil {
		t.Fatalf("无参数应打印帮助并返回 nil: %v", err)
	}
}
