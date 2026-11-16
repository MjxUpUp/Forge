package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/clitask"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/spf13/cobra"
)

// next_wild_test.go 钉住 vNext P1 两个新命令：forge next（单命令引导——决策表）
// 与 forge task wild（野外申报——落盘契约）。

// TestNextJSONOutput 钉住 --json 契约：{"next","reason","state"} 三字段、next 恰一条
// 命令。在临时 git 仓库（干净树、无任务）上走 runNext 全链路。
func TestNextJSONOutput(t *testing.T) {
	root := newWildGitRepo(t)
	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	cmd.Flags().Bool("json", true, "")
	if err := runNext(cmd, nil); err != nil {
		t.Fatalf("runNext: %v", err)
	}
	var got nextResult
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output must be a nextResult JSON, got %q: %v", buf.String(), err)
	}
	if got.Next == "" || got.Reason == "" || got.State == nil {
		t.Errorf("json 契约三字段须齐备, got %+v", got)
	}
	if _, ok := got.State["dirty"]; !ok {
		t.Errorf("state 应含 dirty 键（决策可追溯）, got %v", got.State)
	}
}

// TestTaskWildDeclaresAndCounts E2E：申报两次，断言落盘字段（note/branch/head/
// task_active）与本会话计数输出递增。
func TestTaskWildDeclaresAndCounts(t *testing.T) {
	root := newWildGitRepo(t)
	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	run := func(note string) string {
		var buf bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&buf)
		if err := clitask.RunTaskWild(cmd, []string{note}); err != nil {
			t.Fatalf("clitask.RunTaskWild(%q): %v", note, err)
		}
		return buf.String()
	}
	out1 := run("一次性修复 README 错字")
	out2 := run("紧急热修 CI 脚本")
	if !strings.Contains(out1, "第 1 条") || !strings.Contains(out2, "第 2 条") {
		t.Errorf("会话计数应递增（1→2），got %q / %q", out1, out2)
	}
	// 本测试环境无宿主注入的 session id——空会话必须如实标注"本机累计"而非冒称
	// 本会话（P2 收尾 F7：所有匿名申报共享计数，文案不得误导）。
	if !strings.Contains(out1, "本机累计") {
		t.Errorf("空会话计数的 scope 文案应为「本机累计（无会话身份…）」, got %q", out1)
	}

	data, err := os.ReadFile(filepath.Join(forgedata.DataDirFor(root), "wild", "declarations.jsonl"))
	if err != nil {
		t.Fatalf("wild 申报必须落盘: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("应恰有 2 条申报, got %d lines", len(lines))
	}
	var e clitask.WildDeclaration
	if err := json.Unmarshal([]byte(lines[0]), &e); err != nil {
		t.Fatalf("申报行须为合法 JSON: %v", err)
	}
	if e.Note != "一次性修复 README 错字" || e.Branch == "" || e.Branch == "?" || e.TaskActive {
		t.Errorf("申报字段不符: %+v", e)
	}
	if e.Head == "" || e.Head == "?" {
		t.Errorf("Head 应记录提交哈希, got %q", e.Head)
	}
}

// TestTaskWildRequiresNote 空说明必须拒绝（无说明的申报没有审计价值）。
func TestTaskWildRequiresNote(t *testing.T) {
	root := newWildGitRepo(t)
	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	if err := clitask.RunTaskWild(&cobra.Command{}, []string{"   "}); err == nil {
		t.Error("空白说明必须被拒绝")
	}
}

// newWildGitRepo 建一个带一次提交的临时 git 仓库 + 隔离数据根 + forge 项目标记
// （.forge/state.json——findProjectRoot 的项目判据，同 newTaskGuardProject）。
func newWildGitRepo(t *testing.T) string {
	t.Helper()
	// wild 测试断言「空会话」文案与 anonymous owner——须隔离宿主注入的会话身份。
	isolateSessionIdentity(t)
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".forge", "state.json"),
		[]byte(`{"pipeline_version":"2.0","mode":"small"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
		{"commit", "-q", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", root}, cmd...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", cmd, err, out)
		}
	}
	return root
}

// TestTaskWildEscalatesOverQuotaAndRecordsEscapeHatch 钉住 AC-3(10) 受审计
// 逃生补强(escape-hatch-hardening P2):①每条申报落 checklog escape-hatch 行
// (gate=task-wild/reason=declarative/owner,进统一漏斗);②超会话限额(3)后
// 输出升级提示(不拒绝——逃生舱永在);③有活跃任务时的更强异常信号行。
func TestTaskWildEscalatesOverQuotaAndRecordsEscapeHatch(t *testing.T) {
	root := newWildGitRepo(t)
	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	run := func(note string) string {
		var buf bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		if err := clitask.RunTaskWild(cmd, []string{note}); err != nil {
			t.Fatalf("RunTaskWild(%q): %v", note, err)
		}
		return buf.String()
	}

	within := run("第1条")
	if strings.Contains(within, "超限额") {
		t.Errorf("quota 内不得升级, got %q", within)
	}
	run("第2条")
	run("第3条")
	over := run("第4条")
	if !strings.Contains(over, "超限额") || !strings.Contains(over, "forge task start") {
		t.Errorf("第 4 条(>3)必须升级提示建任务, got %q", over)
	}

	hatch := 0
	all, err := checklog.LoadAllAll(root)
	if err != nil {
		t.Fatalf("LoadAllAll: %v", err)
	}
	for _, e := range all {
		if e.Check == checklog.CheckEscapeHatch && e.Meta["gate"] == "task-wild" {
			hatch++
			if e.Meta["reason"] != "declarative" || e.Meta["owner"] != "anonymous" {
				t.Errorf("escape row meta = %v, want reason=declarative owner=anonymous(空会话)", e.Meta)
			}
		}
	}
	if hatch != 4 {
		t.Errorf("每条申报须落一行 escape-hatch, got %d/4", hatch)
	}
}
