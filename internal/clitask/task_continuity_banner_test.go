package clitask

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// task_continuity_banner_test.go — SessionStart 通道钉子：`forge task resume --hook` 在 managed
// 项目只输出 [forge-session] 横幅一行（按需拉取方案 REQ-1/NFR-1/INV-1/INV-3）。横幅是用户级
// 指令段（~/.claude/CLAUDE.md 等）与 forge-quality skill 的激活判据，文本是契约；横幅之外
// 不注入接续视图、候选清单或待认领推送，也不写任何任务状态。

// snapshotTaskFiles reads every task state file under root's DataDir, keyed by file name.
//
// snapshotTaskFiles 读出 root 的 DataDir 下全部任务状态文件（按文件名键控），供「hook 前后
// 字节相同」断言（INV-1）。
func snapshotTaskFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(forgedata.DataDirFor(root), `tasks`, `*.json`))
	if err != nil {
		t.Fatalf(`glob task files: %v`, err)
	}
	out := make(map[string]string, len(paths))
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf(`read %s: %v`, p, err)
		}
		out[filepath.Base(p)] = string(b)
	}
	return out
}

// TestHookResumeBannerOnly: with an active task, a second incomplete task and a task offered to the current agent, the SessionStart hook prints exactly PASS + the banner and leaves every task file byte-identical (no session anchoring, no NotifiedAt); with no tasks it prints the same.
//
// TestHookResumeBannerOnly：有活跃任务、另一个未完成任务、以及一个派给当前 agent 的任务时，
// SessionStart hook 只输出 PASS + 横幅，所有任务文件前后字节相同（不锚定 session、不写
// NotifiedAt）；零任务时输出完全一样。
func TestHookResumeBannerOnly(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir := t.TempDir()
	initGitProject(t, dir)
	if out, _, code := runForge(t, dir, `task`, `start`, `--ref`, `feat/other`, `--title`, `另一个`); code != 0 {
		t.Fatalf(`task start feat/other: %s`, out)
	}
	if out, _, code := runForge(t, dir, `task`, `assign`, `--ref`, `feat/other`, `--to`, `claude-code`); code != 0 {
		t.Fatalf(`task assign: %s`, out)
	}
	if out, _, code := runForge(t, dir, `task`, `start`, `--ref`, `feat/banner`, `--title`, `横幅`); code != 0 {
		t.Fatalf(`task start feat/banner: %s`, out)
	}

	t.Setenv(`CLAUDE_CODE_SESSION_ID`, `sid-banner`)
	t.Setenv(`FORGE_AGENT`, `claude-code`)
	want := renderHookBanner()
	if want != "PASS\n"+managedSessionBanner+"\n" {
		t.Fatalf(`renderHookBanner 应为 PASS + 横幅一行，实得 %q`, want)
	}
	// INV-3：横幅是用户级指令段的激活判据，按字面钉死——改常量也会被这里拦下。
	const bannerLiteral = "[forge-session] 本项目由 forge 管理（managed）——forge 质量协议生效（用户级指令段以本行为激活信号）；退出运行 forge off。"
	if managedSessionBanner != bannerLiteral {
		t.Fatalf("横幅文本变了（INV-3：用户级指令段按 [forge-session] 字面激活）\nwant %q\ngot  %q", bannerLiteral, managedSessionBanner)
	}

	before := snapshotTaskFiles(t, dir)
	if len(before) != 2 {
		t.Fatalf(`前置：应有 2 个任务文件，实得 %d`, len(before))
	}
	stdout, stderr, code := runForgeStreams(t, dir, `task`, `resume`, `--hook`)
	if code != 0 {
		t.Fatalf("resume --hook exit %d: %s\n%s", code, stdout, stderr)
	}
	if stdout != want {
		t.Fatalf("有任务时 hook stdout 应只有横幅（NFR-1：PASS 之外 1 行）\nwant %q\ngot  %q", want, stdout)
	}
	after := snapshotTaskFiles(t, dir)
	for name, b := range before {
		if after[name] != b {
			t.Errorf("SessionStart hook 改写了任务文件 %s（INV-1：不锚定、不写 NotifiedAt）\nbefore %s\nafter  %s", name, b, after[name])
		}
	}

	// 零任务：同一输出。
	for _, ref := range []string{`feat/banner`, `feat/other`} {
		if out, _, code := runForge(t, dir, `task`, `abort`, `--ref`, ref); code != 0 {
			t.Fatalf(`task abort %s: %s`, ref, out)
		}
	}
	stdout2, _, code := runForgeStreams(t, dir, `task`, `resume`, `--hook`)
	if code != 0 || stdout2 != want {
		t.Fatalf("零任务时 hook stdout 应同为横幅（exit %d）\nwant %q\ngot  %q", code, want, stdout2)
	}
}
