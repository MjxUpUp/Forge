package hooks

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// isolateInfraState 把 $TMPDIR 指向测试私有目录，让去重状态（os.TempDir 下）密闭。
func isolateInfraState(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)
	return tmp
}

// TestHookInfraFailureDedup 钉死 REQ-2：同一会话同一原因只认领一次提示；不同原因、
// 不同会话各认领一次；空会话回落到父进程桶；提示带修复办法与逃生口；并发认领恰好
// 一个赢家；状态目录不可写时宁可多提示。
func TestHookInfraFailureDedup(t *testing.T) {
	isolateInfraState(t)
	t.Setenv(infraDedupEnv, "")

	unknownDocLint := InfraFailure{Cause: InfraUnknownHook, Subject: "doc-lint", Detail: "unknown hook: doc-lint"}
	unknownOther := InfraFailure{Cause: InfraUnknownHook, Subject: "future-hook", Detail: "unknown hook: future-hook"}
	panicFail := InfraFailure{Cause: InfraPanic, Subject: "nil map", Detail: "forge: internal panic: nil map"}

	if !ClaimInfraNotice("sess-1", unknownDocLint) {
		t.Fatal("同会话同因第一次必须认领成功")
	}
	for i := 0; i < 5; i++ {
		if ClaimInfraNotice("sess-1", unknownDocLint) {
			t.Fatalf("同会话同因第 %d 次重复不应再次认领", i+2)
		}
	}
	if !ClaimInfraNotice("sess-1", unknownOther) {
		t.Error("同会话不同 subject 是不同原因，应各认领一次")
	}
	if !ClaimInfraNotice("sess-1", panicFail) {
		t.Error("同会话不同 cause 是不同原因，应各认领一次")
	}
	if !ClaimInfraNotice("sess-2", unknownDocLint) {
		t.Error("不同会话同因应各认领一次")
	}
	if ClaimInfraNotice("sess-2", unknownDocLint) {
		t.Error("sess-2 内重复不应再次认领")
	}

	// 空会话 → ppid 桶：本进程内同样只认领一次。
	if !ClaimInfraNotice("", unknownDocLint) {
		t.Error("空会话回落桶的第一次必须认领成功")
	}
	if ClaimInfraNotice("", unknownDocLint) {
		t.Error("空会话回落桶内重复不应再次认领")
	}

	// 提示文案：原因、不阻断声明、修复办法、逃生口。
	notice := FormatInfraNotice(unknownDocLint)
	for _, want := range []string{"unknown-hook", "unknown hook: doc-lint", "已放行、不阻断", "forge sync --force", infraDedupEnv + "=0"} {
		if !strings.Contains(notice, want) {
			t.Errorf("unknown-hook 提示缺 %q:\n%s", want, notice)
		}
	}
	if !strings.Contains(FormatInfraNotice(panicFail), "forge doctor") {
		t.Error("panic 提示应指向 forge doctor")
	}
	if !strings.Contains(FormatInfraNotice(InfraFailure{Cause: InfraHookError, Subject: "x"}), "forge doctor") {
		t.Error("hook-error 提示应指向 forge doctor")
	}

	// 逃生口：env=0 关闭，其余取值保持开启。
	if !InfraDedupEnabled() {
		t.Error("默认应开启去重")
	}
	t.Setenv(infraDedupEnv, "0")
	if InfraDedupEnabled() {
		t.Error("FORGE_HOOK_INFRA_DEDUP=0 应关闭去重")
	}
	t.Setenv(infraDedupEnv, "1")
	if !InfraDedupEnabled() {
		t.Error("非 0 取值应保持开启")
	}

	// 并发：同 (会话, 原因) 的 N 个并发拉起恰好一个赢家（O_EXCL 原子性）。
	var wg sync.WaitGroup
	var mu sync.Mutex
	winners := 0
	race := InfraFailure{Cause: InfraHookError, Subject: "race"}
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ClaimInfraNotice("sess-race", race) {
				mu.Lock()
				winners++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if winners != 1 {
		t.Errorf("并发认领应恰好 1 个赢家，got %d", winners)
	}

	// 状态目录不可写（$TMPDIR 指向一个普通文件）：每次都认领成功——记不下去重时宁可
	// 多提示，不吞失败。
	blocked := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", blocked)
	t.Setenv("TMP", blocked)
	t.Setenv("TEMP", blocked)
	for i := 0; i < 3; i++ {
		if !ClaimInfraNotice("sess-blocked", unknownDocLint) {
			t.Error("状态不可写时必须 fail 向可见（返回 true）")
		}
	}
}

// TestInfraFailureKey 钉死去重键：同 cause+subject 同键、subject 截断、空白归一。
func TestInfraFailureKey(t *testing.T) {
	a := InfraFailure{Cause: InfraHookError, Subject: "  boom  "}
	b := InfraFailure{Cause: InfraHookError, Subject: "boom"}
	if a.Key() != b.Key() {
		t.Errorf("空白应归一：%q vs %q", a.Key(), b.Key())
	}
	if (InfraFailure{Cause: InfraPanic, Subject: "boom"}).Key() == b.Key() {
		t.Error("不同 cause 必须不同键")
	}
	long := InfraFailure{Cause: InfraHookError, Subject: strings.Repeat("x", 500)}
	if got := len([]rune(long.Key())); got > len(string(InfraHookError))+1+infraSubjectMax+1 { // +1：TruncateRunes 的省略号
		t.Errorf("subject 应被截断到 %d，key 长度 %d", infraSubjectMax, got)
	}
}

// writeInfraSettings 写一份含未知 hook、已知 hook、用户自有 hook 的用户级 settings。
func writeInfraSettings(t *testing.T) (home, path string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	path = filepath.Join(home, "settings.json")
	body := `{"model":"claude-opus-4","hooks":{"PostToolUse":[{"matcher":"Write|Edit","hooks":[` +
		`{"type":"command","command":"forge hook doc-lint-from-the-future"},` +
		`{"type":"command","command":"forge hook task-guard"},` +
		`{"type":"command","command":"my-own-script.sh"}]}],` +
		`"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"forge hook batch --event PreToolUse --matcher \"Bash\""}]}]}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return home, path
}

// TestUnknownHookSelfHeal 钉死 REQ-3：settings 引用二进制不认识的 hook 时，能被发现
// （只列未知名，已知名与 batch 不算）、autoSync 的强制同步条件成立、自愈后陈旧条目
// 消失而用户自己的条目与顶层字段保留，且再次自愈是 no-op。
func TestUnknownHookSelfHeal(t *testing.T) {
	_, path := writeInfraSettings(t)

	got := UnknownWiredHooks(path)
	if len(got) != 1 || got[0] != "doc-lint-from-the-future" {
		t.Fatalf("应只发现未知 hook doc-lint-from-the-future（task-guard/batch 已知），got %v", got)
	}
	if !UserSettingsNeedHeal() {
		t.Fatal("存在未知 forge hook 且未装插件时，autoSync 必须被要求强制同步")
	}

	healed, err := HealUnknownUserHooks()
	if err != nil {
		t.Fatalf("HealUnknownUserHooks: %v", err)
	}
	if len(healed) != 1 || healed[0] != "doc-lint-from-the-future" {
		t.Fatalf("应返回被自愈的名字，got %v", healed)
	}
	if left := UserSettingsUnknownHooks(); len(left) != 0 {
		t.Errorf("自愈后不应再有未知 hook，got %v", left)
	}
	if UserSettingsNeedHeal() {
		t.Error("自愈后 autoSync 不应再被强制同步")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{"my-own-script.sh", `"model"`, "forge hook batch --event PreToolUse"} {
		if !strings.Contains(body, want) {
			t.Errorf("自愈后 settings 应保留/重生 %q", want)
		}
	}
	if strings.Contains(body, "doc-lint-from-the-future") {
		t.Error("陈旧的未知 hook 条目应已被剥掉")
	}

	// 幂等：再自愈一次是 no-op。
	if again, err := HealUnknownUserHooks(); err != nil || len(again) != 0 {
		t.Errorf("二次自愈应是 no-op，got %v, %v", again, err)
	}
}

// TestUnknownWiredHooks_Tolerant 钉死发现侧的容错：文件缺失/损坏返回 nil（发现是
// advisory，绝不让一次运行失败）；forge gate 条目与非 forge 命令不算未知 hook。
func TestUnknownWiredHooks_Tolerant(t *testing.T) {
	dir := t.TempDir()
	if got := UnknownWiredHooks(filepath.Join(dir, "missing.json")); got != nil {
		t.Errorf("文件缺失应返回 nil，got %v", got)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := UnknownWiredHooks(bad); got != nil {
		t.Errorf("损坏文件应返回 nil，got %v", got)
	}
	ok := filepath.Join(dir, "ok.json")
	body := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"forge gate task-verify"},{"type":"command","command":"echo forge hook nope"}]}]}}`
	if err := os.WriteFile(ok, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := UnknownWiredHooks(ok); got != nil {
		t.Errorf("forge gate 与非 forge 命令不算未知 hook，got %v", got)
	}
}

// TestInfraStateHardening 钉死评审加固：状态根带 uid 且目录 0700；根是符号链接时按状态
// 不可用处理（每次都提示）；旧标记超过 TTL 视为陈旧并重新认领（PID 复用）；清理只删形如
// 会话桶的陈旧目录，其他条目原样保留。
func TestInfraStateHardening(t *testing.T) {
	tmp := isolateInfraState(t)
	f := InfraFailure{Cause: InfraHookError, Subject: "hardening"}

	root := infraStateRoot()
	if !strings.HasPrefix(filepath.Base(root), InfraStateDirName+"-") {
		t.Errorf("状态根应带 uid 后缀，got %q", root)
	}
	if !ClaimInfraNotice("sess-h", f) {
		t.Fatal("首次应认领成功")
	}
	info, err := os.Stat(filepath.Join(root, "sess-h"))
	if err != nil {
		t.Fatalf("会话目录应存在: %v", err)
	}
	// Windows does not carry Unix permission bits (Perm() reports 0777), so the 0700
	// check only applies elsewhere.
	// Windows 不带 Unix 权限位（Perm() 恒报 0777），0700 断言只在其他平台生效。
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Errorf("会话目录应为 0700，got %v", info.Mode().Perm())
	}

	// 陈旧标记：回拨 mtime 超过 TTL → 重新认领一次，之后又去重。
	old := time.Now().Add(-2 * infraMarkerTTL)
	entries, _ := os.ReadDir(filepath.Join(root, "sess-h"))
	if len(entries) != 1 {
		t.Fatalf("应恰有 1 个 marker，got %d", len(entries))
	}
	marker := filepath.Join(root, "sess-h", entries[0].Name())
	if err := os.Chtimes(marker, old, old); err != nil {
		t.Fatal(err)
	}
	if !ClaimInfraNotice("sess-h", f) {
		t.Error("陈旧标记应被重新认领")
	}
	if ClaimInfraNotice("sess-h", f) {
		t.Error("重新认领后同会话应再次去重")
	}

	// 清理：只删陈旧的桶目录；不合桶名的条目与新鲜目录保留。
	oldBucket := filepath.Join(root, "old-session")
	foreign := filepath.Join(root, "not a bucket!")
	for _, d := range []string{oldBucket, foreign} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(d, old.Add(-infraStateMaxAge), old.Add(-infraStateMaxAge)); err != nil {
			t.Fatal(err)
		}
	}
	ClaimInfraNotice("fresh-trigger", f) // 新会话目录不存在 → 触发 prune
	if _, err := os.Stat(oldBucket); !os.IsNotExist(err) {
		t.Error("陈旧的桶目录应被清理")
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Error("不合桶名的条目不得被清理")
	}

	// 根是符号链接（他人预置的劫持）：按状态不可用处理——每次都认领成功，且不碰链接目标。
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(tmp, "victim")
	if err := os.MkdirAll(filepath.Join(target, "stale-session"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, root); err != nil {
		t.Skipf("无法建符号链接: %v", err)
	}
	for i := 0; i < 3; i++ {
		if !ClaimInfraNotice("sess-link", f) {
			t.Error("根是符号链接时应按不可用处理（返回 true）")
		}
	}
	if _, err := os.Stat(filepath.Join(target, "stale-session")); err != nil {
		t.Error("链接目标里的内容不得被读写或清理")
	}
}
