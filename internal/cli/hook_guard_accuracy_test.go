package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/spf13/cobra"
)

// 本文件钉住 2026-08 guard-accuracy 轮次（两周生产 usage 日志）的两项
// checklog 准确性修复：
//
//  1. 级别标注：detail 自述 PASS/advisory 的 advisory 记录，在被 host 升格翻转
//     发射裁定时曾记成 level=blocked/passed=false（kimi P0 升格窗口 7 条
//     assertion-check 记录；scoring 把 Passed 当质量裁定，advisory 被翻转
//     AssertionPassed）。现在记录携带【脚本自己】的裁定：升格的 PASS 脚本记
//     Passed=true + Level=advisory；仅脚本 FAIL 才记 Passed=false + Level=blocked。
//  2. 阻断记录去重：host 对同一工具事件双发（kimi 对一次 Edit 98ms 内双发
//     read-before-edit——checklog seq 连续）曾产生重复审计行。3s 窗口内相同的
//     blocked 记录只记一次。

// readChecklogEntries 读取 root 项目的 checklog 条目（FORGE_DATA_HOME 由调用方
// 隔离）。
func readChecklogEntries(t *testing.T, root string) []struct {
	Check  string `json:"check"`
	Passed bool   `json:"passed"`
	Level  string `json:"level"`
	Detail string `json:"detail"`
} {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(forgedata.DataDirFor(root), "checklog.jsonl"))
	if err != nil {
		t.Fatalf("read checklog: %v", err)
	}
	var out []struct {
		Check  string `json:"check"`
		Passed bool   `json:"passed"`
		Level  string `json:"level"`
		Detail string `json:"detail"`
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var e struct {
			Check  string `json:"check"`
			Passed bool   `json:"passed"`
			Level  string `json:"level"`
			Detail string `json:"detail"`
		}
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("checklog line not valid JSON: %v", err)
		}
		out = append(out, e)
	}
	return out
}

// TestHook_PromotedAdvisoryRecordsAdvisoryLevel pins fix 1's promoted branch.
//
// TestHook_PromotedAdvisoryRecordsAdvisoryLevel 钉住修复 1 的提升分支：dsh 上
// task-guard 无任务 advisory 被提升为 exit-2 阻断，但 checklog 条目必须记脚本
// 自身结论——Passed=true + level=advisory——而非 blocked/fail。阻断发射本身
// 不变（由 TestHook_DshTaskGuardNoTaskBlocks 覆盖）。
func TestHook_PromotedAdvisoryRecordsAdvisoryLevel(t *testing.T) {
	root := newTaskGuardProject(t)
	sess := fmt.Sprintf("dsh-level-%d", time.Now().UnixNano())

	_, _, err := runTaskGuardHookOnce(t, `"forge_agent":"dsh",`, sess)
	var blockErr *HookBlockError
	if !errors.As(err, &blockErr) {
		t.Fatalf("dsh no-task edit must still be denied (promotion intact), got %T %v", err, err)
	}

	var matches []string
	for _, e := range readChecklogEntries(t, root) {
		if e.Check != "task-guard" {
			continue
		}
		if !e.Passed {
			matches = append(matches, fmt.Sprintf("passed=false (level=%s)", e.Level))
			continue
		}
		if e.Level != "advisory" {
			matches = append(matches, fmt.Sprintf("level=%q (want advisory)", e.Level))
			continue
		}
	}
	if len(matches) > 0 {
		t.Errorf("promoted advisory must record Passed=true + level=advisory, violations: %v", matches)
	}
	// Positive pin: the entry exists at all (the advisory must stay in the audit trail).
	found := false
	for _, e := range readChecklogEntries(t, root) {
		if e.Check == "task-guard" && e.Passed && e.Level == "advisory" {
			found = true
		}
	}
	if !found {
		t.Error("task-guard advisory entry missing from checklog (promotion must not drop the audit line)")
	}
}

// TestHook_RealBlockStillRecordsBlocked pins the counterfactual of fix 1: a hook whose SCRIPT itself fails (hazard-guard exit 1) is a real block — the record stays Passed=false + level=blocked.
//
// TestHook_RealBlockStillRecordsBlocked 钉住修复 1 的反事实：脚本自身失败
// （hazard-guard exit 1）是真阻断——记录保持 Passed=false + level=blocked。
// 只有提升翻转才走 advisory 重标注。
func TestHook_RealBlockStillRecordsBlocked(t *testing.T) {
	root := newTaskGuardProject(t)
	sess := fmt.Sprintf("realblock-%d", time.Now().UnixNano())

	payload := fmt.Sprintf(`{"hook_event_name":"PreToolUse","tool_name":"Bash","session_id":%q,"tool_input":{"command":"rm -rf ./important-data"}}`, sess)
	oldStdin := os.Stdin
	tmpStdin, err := os.CreateTemp("", "hook-stdin-*.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tmpStdin.WriteString(payload); err != nil {
		t.Fatal(err)
	}
	if _, err = tmpStdin.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	os.Stdin = tmpStdin
	defer func() {
		os.Stdin = oldStdin
		tmpStdin.Close()
		os.Remove(tmpStdin.Name())
	}()

	_, _, _ = captureOutput(t, func() error {
		return runHook(&cobra.Command{}, []string{"hazard-guard"})
	})

	found := false
	for _, e := range readChecklogEntries(t, root) {
		if e.Check != "hazard-guard" {
			continue
		}
		found = true
		if e.Passed {
			t.Errorf("real script FAIL must record passed=false, got passed=true (level=%s)", e.Level)
		}
		if e.Level != "blocked" {
			t.Errorf("real script FAIL must record level=blocked, got %q", e.Level)
		}
	}
	if !found {
		t.Error("hazard-guard block entry missing from checklog")
	}
}

// TestDuplicateBlockRecord covers the check/stamp pair.
//
// TestDuplicateBlockRecord 覆盖 check/stamp 对：未打戳的重复恒报「非重复」
// （Record 失败场景——审计行绝不可被一条从未落盘记录对应的戳抑制）；打戳后
// 窗口内完全相同重复被抑制；任何差异（detail/check/session）或窗口过期都重新
// 记录。I/O 失败方向是宁多记（空 root 返回 false）。
func TestDuplicateBlockRecord(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	root := t.TempDir()

	// Without a stamp (e.g. the Record failed), repeats must keep reporting
	// "not a duplicate" — nothing may suppress the retry.
	if duplicateBlockRecord(root, "sess-1", "read-before-edit", "FAIL detail-x") {
		t.Error("first check must not be a duplicate")
	}
	if duplicateBlockRecord(root, "sess-1", "read-before-edit", "FAIL detail-x") {
		t.Error("unstamped repeat must not be suppressed (Record-failure direction)")
	}

	// After the stamp (Record succeeded), the identical repeat is suppressed.
	stampBlockRecord(root, "sess-1", "read-before-edit", "FAIL detail-x")
	if !duplicateBlockRecord(root, "sess-1", "read-before-edit", "FAIL detail-x") {
		t.Error("identical repeat inside the window must be suppressed (the 98ms double-fire case)")
	}
	if duplicateBlockRecord(root, "sess-1", "read-before-edit", "FAIL detail-y") {
		t.Error("different detail must record")
	}
	if duplicateBlockRecord(root, "sess-1", "hazard-guard", "FAIL detail-x") {
		t.Error("different check must record")
	}
	if duplicateBlockRecord(root, "sess-2", "read-before-edit", "FAIL detail-x") {
		t.Error("different session must record")
	}
	if duplicateBlockRecord("", "sess-1", "read-before-edit", "FAIL detail-x") {
		t.Error("empty root must fail toward recording")
	}

	// Expired window: rewrite the marker with an old timestamp — the same
	// fingerprint must record again (a legit re-block minutes later is a new event).
	h := fnv.New64a()
	_, _ = h.Write([]byte("FAIL detail-x"))
	fp := strconv.FormatUint(h.Sum64(), 16)
	marker := filepath.Join(forgedata.DataDirFor(root), "markers", "forge-block-dedup-"+readsFileKey("sess-1")+"-read-before-edit")
	old := time.Now().Add(-10 * time.Second).Unix()
	if err := os.WriteFile(marker, []byte(strconv.FormatInt(old, 10)+" "+fp+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if duplicateBlockRecord(root, "sess-1", "read-before-edit", "FAIL detail-x") {
		t.Error("expired window must record again")
	}
}
