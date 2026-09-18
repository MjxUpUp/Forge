package e2e

// taskverify_stopblock_test.go —— task-verify Stop 有界阻断(escape-hatch-
// hardening P1)的端到端契约:窄条件(活跃任务 + 未提交代码变更)首次 Stop
// exit 2;60s 节流使紧随的再停直接放行(死循环结构性不可能);会话限额 3 次
// 耗尽后回落 advisory;FORGE_TASK_VERIFY_STOP=0 逃生;提交后(工作已落盘)零阻断。
// 与 taskverify_skillreach_test.go 同 harness:真实二进制 + DataDir 里的
// 真实 hook 脚本(verification surface = 脚本 exit code)。

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// runStopBlockHook 直接执行 DataDir 里的 task-verify.sh,注入 Go 层在生产中
// 提供的 FORGE_* env;清除 60s 节流戳(节流交互由独立断言钉住,其余步骤
// 不受节流干扰)。返回 (combined output, exit err)。
func runStopBlockHook(t *testing.T, dir, sid, taskRef string, extraEnv ...string) (string, error) {
	t.Helper()
	hookPath := filepath.Join(forgedata.DataDirFor(dir), "hooks", "task-verify.sh")
	_ = os.Remove(filepath.Join(forgedata.DataDirFor(dir), ".task-verify-throttle.last"))
	cmd := exec.Command("bash", hookPath)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"CLAUDE_CODE_SESSION_ID="+sid,
		"FORGE_SESSION_ID="+sid,
		"FORGE_TASK_REF="+taskRef,
		"PATH="+filepath.Dir(forgeBin)+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func stopBlockMarker(dir, sid string) string {
	return filepath.Join(forgedata.DataDirFor(dir), "markers", "taskverify-stop-blocks-"+sid)
}

// loadChecklogEntries 读 DataDir checklog.jsonl 的原始行(轻量;e2e 只做
// 子串断言,不反序列化)。
func loadChecklogEntries(t *testing.T, dir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(forgedata.DataDirFor(dir), "checklog.jsonl"))
	if err != nil {
		t.Fatalf("read checklog: %v", err)
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

func TestTaskVerifyStopHook_BoundedBlockOnUncommittedTaskWork(t *testing.T) {
	dir := freshProjectOnBranch(t, "feature/stopblock")
	const sid = "sess-stopblock"
	const ref = "STOPBLOCK"

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(forgeBin, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"CLAUDE_CODE_SESSION_ID="+sid,
			"PATH="+filepath.Dir(forgeBin)+string(os.PathListSeparator)+os.Getenv("PATH"),
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("forge %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("task", "start", "--ref", ref, "--title", "stop block e2e")
	writeFile(t, dir, "internal/widget/tap.go", "package widget\n\nfunc Tap() {}\n")
	git(t, dir, "add", "internal/widget/tap.go")
	// 活动痕迹(过 read-before-edit/activity 门)。
	s := hookStdin(t, sid, "PostToolUse", "Read", map[string]any{"file_path": "internal/widget/tap.go"})
	_, _, _ = forgeHook(t, dir, "tool-track", s)
	run("task", "gate", "task-implement", "--ref", ref)

	// 1) 首次 Stop:未提交代码 + 活跃任务 → 有界阻断(exit 2)。
	out, err := runStopBlockHook(t, dir, sid, ref)
	if err == nil || !strings.Contains(out, "未提交代码变更") || !strings.Contains(out, "1/3") {
		t.Fatalf("uncommitted task work must bounded-block (exit 2, reason, 1/3), err=%v\n---OUT---\n%s", err, out)
	}
	if _, statErr := os.Stat(stopBlockMarker(dir, sid)); statErr != nil {
		t.Fatalf("block counter marker must exist: %v", statErr)
	}

	// 2) 连续阻断语义(审查修正后):block 清 60s 节流戳——紧随再停**继续拦**
	// (2/3),死循环由会话限额封顶而非节流削弱;每次 block 落一行 fail checklog。
	out, err = runStopBlockHook(t, dir, sid, ref)
	if err == nil || !strings.Contains(out, "2/3") {
		t.Fatalf("consecutive re-stop must block again (2/3), err=%v\n%s", err, out)
	}
	failRows := 0
	for _, e := range loadChecklogEntries(t, dir) {
		if strings.Contains(e, "bounded stop block") {
			failRows++
		}
	}
	if failRows < 2 {
		t.Fatalf("each block must leave a fail checklog row, got %d\n", failRows)
	}

	// 3) 限额:计数已到 2 → 第 3 次仍拦(3/3);第 4 次 → 放行 + 一次性说明。
	out, err = runStopBlockHook(t, dir, sid, ref)
	if err == nil || !strings.Contains(out, "3/3") {
		t.Fatalf("third block must fire (3/3), err=%v\n%s", err, out)
	}
	out, err = runStopBlockHook(t, dir, sid, ref)
	if err != nil || !strings.Contains(out, "限额已耗尽") {
		t.Fatalf("exhausted cap must fall back to advisory pass, err=%v\n%s", err, out)
	}

	// 4) 逃生:计数清零 + FORGE_TASK_VERIFY_STOP=0 → 不拦。
	_ = os.WriteFile(stopBlockMarker(dir, sid), []byte("0"), 0o644)
	out, err = runStopBlockHook(t, dir, sid, ref, "FORGE_TASK_VERIFY_STOP=0")
	if err != nil || strings.Contains(out, "有界阻断") {
		t.Fatalf("env escape must disable the block, err=%v\n%s", err, out)
	}

	// 5) 窄条件:工作落盘(commit)后同样的任务态零阻断。
	_ = os.WriteFile(stopBlockMarker(dir, sid), []byte("0"), 0o644)
	git(t, dir, "add", "-A")
	git(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "land work")
	out, err = runStopBlockHook(t, dir, sid, ref)
	if err != nil || strings.Contains(out, "有界阻断") {
		t.Fatalf("committed workspace must not block, err=%v\n%s", err, out)
	}

	// 6) untracked 新代码文件同样算未落盘(审查必改项:只看 diff 会漏新建未
	// add 的最常见形态)——计数归零后新建 .go 不 add → 仍拦(1/3)。
	writeFile(t, dir, "internal/widget/newfile.go", "package widget\n\nfunc New() {}\n")
	out, err = runStopBlockHook(t, dir, sid, ref)
	if err == nil || !strings.Contains(out, "1/3") {
		t.Fatalf("untracked new code file must block (1/3), err=%v\n%s", err, out)
	}
}

// TestTaskVerifyStopHook_SurfacesPendingHazardHITL 钉住 P1 等人态:本会话存在
// 未确认高危拦截(block 无对应 release/confirm)时,Stop 面必须出现 HITL 提醒
// ——人不在拦截现场,会话结束面是第一回看落点(取证 sess_7e05f7e1 的 9 秒
// 绕行缺口)。确认事件补齐后提醒消失。
func TestTaskVerifyStopHook_SurfacesPendingHazardHITL(t *testing.T) {
	dir := freshProjectOnBranch(t, "feature/hitl")
	const sid = "sess-hitl"
	const ref = "HITLSURF"

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(forgeBin, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"CLAUDE_CODE_SESSION_ID="+sid,
			"PATH="+filepath.Dir(forgeBin)+string(os.PathListSeparator)+os.Getenv("PATH"),
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("forge %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("task", "start", "--ref", ref, "--title", "hitl surface e2e")
	// 无未提交代码(不触发有界阻断),使 HITL 行是唯一消息面。
	writeFile(t, dir, "internal/widget/quiet.go", "package widget\n\nfunc Quiet() {}\n")
	git(t, dir, "add", "-A")
	git(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "quiet")

	hazDir := filepath.Join(forgedata.DataDirFor(dir), "hazards")
	if err := os.MkdirAll(hazDir, 0o755); err != nil {
		t.Fatal(err)
	}
	events := filepath.Join(hazDir, "events.jsonl")
	blockLine := `{"ts":"2026-09-18T00:00:00Z","type":"block","fingerprint":"abc","command":"rm -rf dbg","session_id":"` + sid + `"}` + "\n"
	if err := os.WriteFile(events, []byte(blockLine), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runStopBlockHook(t, dir, sid, ref)
	if err != nil {
		t.Fatalf("clean workspace must not block, err=%v\n%s", err, out)
	}
	if !strings.Contains(out, "HITL 等人") || !strings.Contains(out, "1 个高危拦截未经人工确认") {
		t.Fatalf("unconfirmed hazard block must surface at Stop, got:\n%s", out)
	}

	// 确认事件补齐 → 提醒消失。
	confirmLine := `{"ts":"2026-09-18T00:01:00Z","type":"confirm","fingerprint":"abc","command":"rm -rf dbg","session_id":"` + sid + `"}` + "\n"
	if err := os.WriteFile(events, []byte(blockLine+confirmLine), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = runStopBlockHook(t, dir, sid, ref)
	if err != nil {
		t.Fatalf("confirmed state must not block, err=%v\n%s", err, out)
	}
	if strings.Contains(out, "HITL 等人") {
		t.Fatalf("confirmed hazard must not surface, got:\n%s", out)
	}
}
