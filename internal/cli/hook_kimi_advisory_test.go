package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// kimi advisory 队列（hook_kimi_advisory.go）存在的理由：kimi 0.35.0 没给
// advisory 任何安全的即时通道——PreToolUse 的 stdout 被当 **deny**（P0 提升的
// 「allowed」文案阻断正是此症），PostToolUse/Stop/SessionStart 的 stdout 被丢
// （生产 checklog：kimi/no-channel advisory 100% 丢失）。本组测试钉住队列契约：
// 不可送达事件上静默入队、UserPromptSubmit 上攒成一条去重注入、同一文本每会话
// 只投一次。

// newKimiAdvisoryFixture 隔离队列（FORGE_DATA_HOME）与 per-session delivered
// 集合（TMPDIR），返回项目根。
func newKimiAdvisoryFixture(t *testing.T) string {
	t.Helper()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	return t.TempDir()
}

func TestKimiAdvisoryEnqueueIsSilentAndQueued(t *testing.T) {
	root := newKimiAdvisoryFixture(t)
	sess := fmt.Sprintf("kimi-q-%d", time.Now().UnixNano())

	stdout, _, err := captureOutput(t, func() error {
		return emitAdvisoryRouted("kimi", "PreToolUse", "task-guard", root, sess, true, "[task-guard] Untracked source edit — no active task. Why: changes outside a task skip verify/review/score gates.")
	})
	if err != nil {
		t.Fatalf("advisory must stay an allow (nil error), got %v", err)
	}
	if stdout != "" {
		t.Errorf("kimi PreToolUse advisory must NOT print stdout (kimi reads it as a deny), got %q", stdout)
	}
	data, rerr := os.ReadFile(kimiAdvisoryQueuePath(root))
	if rerr != nil {
		t.Fatalf("advisory must be queued at %s: %v", kimiAdvisoryQueuePath(root), rerr)
	}
	content := string(data)
	for _, want := range []string{`"hook":"task-guard"`, `"event":"PreToolUse"`, "no active task"} {
		if !strings.Contains(content, want) {
			t.Errorf("queue entry missing %s, got %q", want, content)
		}
	}
}

func TestKimiAdvisoryDrainOnUserPromptSubmit(t *testing.T) {
	root := newKimiAdvisoryFixture(t)
	sess := fmt.Sprintf("kimi-d-%d", time.Now().UnixNano())

	enqueueKimiAdvisory(root, sess, "skill-trigger", "PostToolUse", "[skill-trigger] compile-fix-loop hit")
	enqueueKimiAdvisory(root, sess, "test-nudge", "PostToolUse", "[forge] 3 source writes with no paired test")

	stdout, _, err := captureOutput(t, func() error {
		return emitAdvisoryRouted("kimi", "UserPromptSubmit", "resume-reinject", root, sess, true, "task 已接续：feat/xxx")
	})
	if err != nil {
		t.Fatalf("UserPromptSubmit must return nil, got %v", err)
	}
	// 攒发**前置**于 hook 自己的 detail（头部在 emitter 截尾下存活），且两条
	// advisory 列在同一条消息里。
	if !strings.Contains(stdout, "hook advisory") || !strings.Contains(stdout, "compile-fix-loop hit") || !strings.Contains(stdout, "no paired test") {
		t.Errorf("drained batch missing queued advisories, got %q", stdout)
	}
	if strings.Index(stdout, "hook advisory") > strings.Index(stdout, "task 已接续") {
		t.Errorf("batch must PREPEND the hook's own detail, got %q", stdout)
	}
	// 队列随 drain 消费。
	if _, serr := os.Stat(kimiAdvisoryQueuePath(root)); !os.IsNotExist(serr) {
		t.Errorf("queue file must be consumed after drain, stat err=%v", serr)
	}
}

func TestKimiAdvisoryDrainDedupesAndCaps(t *testing.T) {
	root := newKimiAdvisoryFixture(t)
	sess := fmt.Sprintf("kimi-c-%d", time.Now().UnixNano())

	// 先在小批量上验去重（避开上限干扰）。
	enqueueKimiAdvisory(root, sess, "task-guard", "PreToolUse", "dup advisory")
	enqueueKimiAdvisory(root, sess, "task-guard", "PreToolUse", "dup advisory")
	batch := drainKimiAdvisories(root, sess)
	if strings.Count(batch, "dup advisory") != 1 {
		t.Errorf("identical advisories must dedupe to one, got %q", batch)
	}

	// 上限：7 条不同文本 → 截到 kimiAdvisoryDrainCap 条，保留**最新**窗口
	// （advisory-5 必在，advisory-0 必丢）。换新会话，避免 delivered 集合抑制。
	sess2 := sess + "-cap"
	for i := 0; i < 7; i++ {
		enqueueKimiAdvisory(root, sess2, "auto-compile", "PostToolUse", fmt.Sprintf("advisory-%d", i))
	}
	batch = drainKimiAdvisories(root, sess2)
	if got := strings.Count(batch, "\n"); got > kimiAdvisoryDrainCap {
		t.Errorf("batch must cap at %d entries, got %d lines: %q", kimiAdvisoryDrainCap, got, batch)
	}
	if !strings.Contains(batch, "advisory-5") || strings.Contains(batch, "advisory-0") {
		t.Errorf("cap must keep the newest window, got %q", batch)
	}
}

func TestKimiAdvisoryOncePerSession(t *testing.T) {
	root := newKimiAdvisoryFixture(t)
	sess := fmt.Sprintf("kimi-s-%d", time.Now().UnixNano())

	enqueueKimiAdvisory(root, sess, "task-guard", "PreToolUse", "repeat advisory")
	if batch := drainKimiAdvisories(root, sess); !strings.Contains(batch, "repeat advisory") {
		t.Fatalf("first drain must deliver, got %q", batch)
	}
	// 同文本再次触发（如被节流的 hook 再次命中）：同会话不得二次投递。
	enqueueKimiAdvisory(root, sess, "task-guard", "PreToolUse", "repeat advisory")
	if batch := drainKimiAdvisories(root, sess); batch != "" {
		t.Errorf("same advisory text must deliver once per session, got %q", batch)
	}
	// 新会话是新受众，会再次收到。
	enqueueKimiAdvisory(root, sess, "task-guard", "PreToolUse", "repeat advisory")
	if batch := drainKimiAdvisories(root, sess+"-2"); !strings.Contains(batch, "repeat advisory") {
		t.Errorf("a new session must receive the advisory again, got %q", batch)
	}
}

// TestKimiAdvisoryEmptySessionNoDeliveredMemory 钉空会话隔离：sessionID 为空时
// delivered 记忆整体禁用——SanitizeSessionID 会把 "" 折叠成 "session"，全机所有
// 无 id 会话共享同一个 delivered 文件；那里一次写入将永久静音同文本 advisory
// （全机级抑制）。故每次 drain 都必须重新投递，且绝不落盘。
func TestKimiAdvisoryEmptySessionNoDeliveredMemory(t *testing.T) {
	root := newKimiAdvisoryFixture(t)

	// 直接钉 read/append 入口语义：空 id 视为无历史、不写入。
	// Pin the read/append entry semantics directly: empty id = no history, no write.
	if got := readKimiDeliveredSet(root, ""); len(got) != 0 {
		t.Fatalf("空 session 的 delivered 集合必须为空, got %v", got)
	}
	appendKimiDeliveredSet(root, "", []string{"any text"})
	if _, err := os.Stat(kimiDeliveredSetPath(root, "")); !os.IsNotExist(err) {
		t.Errorf("空 session 绝不写 delivered 文件（共享桶一次写入=全机静音）, stat err=%v", err)
	}

	// 端到端：同文本两轮「入队→drain」，空 session 每轮都必须重新投递。
	// End to end: two enqueue→drain rounds of the same text — an empty
	// session must re-deliver every round.
	for round := 1; round <= 2; round++ {
		enqueueKimiAdvisory(root, "", "task-guard", "PreToolUse", "id-less advisory")
		batch := drainKimiAdvisories(root, "")
		if !strings.Contains(batch, "id-less advisory") {
			t.Fatalf("第 %d 轮 drain 必须投递（无记忆）, got %q", round, batch)
		}
	}
	// 对拍：有 id 的会话维持每会话一次的记忆（同数据第二轮静默）——隔离只放宽
	// 空 id，不侵蚀正常路径。
	// Contrast: an id-ful session keeps the once-per-session memory (second
	// round silent on the same data) — the exemption loosens ONLY the id-less
	// path, never the normal one.
	sess := fmt.Sprintf("kimi-e-%d", time.Now().UnixNano())
	for round := 1; round <= 2; round++ {
		enqueueKimiAdvisory(root, sess, "task-guard", "PreToolUse", "id-less advisory")
		batch := drainKimiAdvisories(root, sess)
		if round == 1 && !strings.Contains(batch, "id-less advisory") {
			t.Fatalf("有 id 会话首轮必须投递, got %q", batch)
		}
		if round == 2 && batch != "" {
			t.Fatalf("有 id 会话同文本不得二次投递, got %q", batch)
		}
	}
}

func TestKimiAdvisoryBlockPathUntouched(t *testing.T) {
	root := newKimiAdvisoryFixture(t)
	sess := fmt.Sprintf("kimi-b-%d", time.Now().UnixNano())

	// 设计内 deny（read-before-edit/hazard-guard/freeze-guard 的 FAIL）仍以
	// stderr + HookBlockError 阻断——队列绝不拦截它们。
	_, stderr, err := captureOutput(t, func() error {
		return emitAdvisoryRouted("kimi", "PreToolUse", "read-before-edit", root, sess, false, "BLOCKED: 未读即改")
	})
	var blockErr *HookBlockError
	if !errors.As(err, &blockErr) {
		t.Fatalf("block must return *HookBlockError (exit 2), got %T %v", err, err)
	}
	if !strings.Contains(stderr, "BLOCKED") {
		t.Errorf("block reason must go to stderr, got %q", stderr)
	}
	if _, serr := os.Stat(kimiAdvisoryQueuePath(root)); !os.IsNotExist(serr) {
		t.Error("a block must never touch the advisory queue")
	}
}

func TestEmitAdvisoryRoutedOtherHostsUnchanged(t *testing.T) {
	root := newKimiAdvisoryFixture(t)

	// Claude 兼容宿主保持裸 hookSpecificOutput 注入——不入队、不静默。
	stdout, _, err := captureOutput(t, func() error {
		return emitAdvisoryRouted("", "PreToolUse", "task-guard", root, "s1", true, "[task-guard] warn")
	})
	if err != nil {
		t.Fatalf("claude allow must return nil, got %v", err)
	}
	if !strings.Contains(stdout, "additionalContext") || !strings.Contains(stdout, "[task-guard] warn") {
		t.Errorf("claude path must inject additionalContext as before, got %q", stdout)
	}
	if _, serr := os.Stat(kimiAdvisoryQueuePath(root)); !os.IsNotExist(serr) {
		t.Error("non-kimi hosts must never write the kimi advisory queue")
	}
}

func TestKimiAdvisoryEmptyDetailStaysSilent(t *testing.T) {
	root := newKimiAdvisoryFixture(t)
	stdout, _, err := captureOutput(t, func() error {
		return emitAdvisoryRouted("kimi", "PostToolUse", "auto-compile", root, "s1", true, "  ")
	})
	if err != nil || stdout != "" {
		t.Errorf("whitespace-only detail must stay silent and unqueued, got stdout=%q err=%v", stdout, err)
	}
	if _, serr := os.Stat(kimiAdvisoryQueuePath(root)); !os.IsNotExist(serr) {
		t.Error("whitespace-only detail must not create the queue file")
	}
}

func TestKimiAdvisoryGlobalHookNoRoot(t *testing.T) {
	newKimiAdvisoryFixture(t)
	// root==""（global hook）：没有项目 DataDir 可入队——静默、不崩（这些 hook
	// 在 kimi 上本就是已文档化的失效行为）。
	stdout, _, err := captureOutput(t, func() error {
		return emitAdvisoryRouted("kimi", "SessionStart", "skill-scan", "", "s1", true, "[skill-scan] advisory")
	})
	if err != nil || stdout != "" {
		t.Errorf("global-hook advisory on a dropped event must stay silent, got stdout=%q err=%v", stdout, err)
	}
	if batch := drainKimiAdvisories("", "s1"); batch != "" {
		t.Errorf("drain with empty root must be a no-op, got %q", batch)
	}
}

// TestKimiTaskGuardE2EQueuesNotBlocks 是本队列要修的 2026-08 事件类的全链路
// 回归：kimi 会话在 main 上无任务改源码。WARN 必须 (a) **不**阻断——引入队列
// 前的提升把它变成了「allowed」文案的 exit-2 deny、拦停编辑；(b) 在
// PreToolUse 上**零**输出——kimi 把该事件的任何 stdout 当 deny；(c) 按无视
// 计数谱系入队（vNext P0-3 取代 NOWARN：第 1 条三段式 + 第 2 条升档 STOP，第 3
// 次起静默）；(d) 经 UserPromptSubmit 攒发浮现一次。
func TestKimiTaskGuardE2EQueuesNotBlocks(t *testing.T) {
	root := newTaskGuardProject(t)
	t.Setenv("TMPDIR", t.TempDir())
	sess := fmt.Sprintf("kimi-e2e-%d", time.Now().UnixNano())

	for i := 1; i <= 3; i++ {
		stdout, stderr, err := runTaskGuardHookOnce(t, `"forge_agent":"kimi",`, sess)
		if err != nil {
			t.Fatalf("edit #%d must be ALLOWED on kimi (queue, not block), got %T %v (stderr=%q)", i, err, err, stderr)
		}
		if stdout != "" {
			t.Errorf("edit #%d must print nothing on kimi PreToolUse (stdout = deny), got %q", i, stdout)
		}
	}
	data, rerr := os.ReadFile(kimiAdvisoryQueuePath(root))
	if rerr != nil || !strings.Contains(string(data), "no active task") {
		t.Fatalf("WARN must be queued, got %q err=%v", data, rerr)
	}
	if !strings.Contains(string(data), "Second untracked source edit") {
		t.Errorf("ignore counter must queue the escalation copy on edit #2, got %q", data)
	}
	if n := strings.Count(strings.TrimRight(string(data), "\n"), "\n"); n != 1 {
		t.Errorf("spectrum must queue exactly two entries (advisory + escalation), got %d extra lines", n-1)
	}
	batch := drainKimiAdvisories(root, sess)
	if !strings.Contains(batch, "no active task") {
		t.Errorf("UserPromptSubmit drain must surface the queued WARN, got %q", batch)
	}
	if batch2 := drainKimiAdvisories(root, sess); batch2 != "" {
		t.Errorf("second drain must be empty (consumed + once-per-session), got %q", batch2)
	}
}
