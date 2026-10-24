package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/attribution"
)

// recordLedger 写一条归属事件——Stop 判定的会话作用域真相源。
func recordLedger(t *testing.T, root, sid, path string, ts time.Time) {
	t.Helper()
	attribution.Record(root, attribution.Event{Ts: ts, Sid: sid, Kind: attribution.KindWrite, Path: path})
}

// pinEnv 钉死本文件依赖的两个环境变量：ambient FORGE_ATTRIBUTION=0 会让「外来不阻断」
// 类用例翻转成需审，ambient 放大窗口会让「窗口过期」翻绿。测试不该随 shell 状态变色。
func pinEnv(t *testing.T) {
	t.Helper()
	t.Setenv("FORGE_ATTRIBUTION", "1")
	t.Setenv(foreignWindowEnv, "")
}

// TestEvaluateFor_ForeignOnlyDoesNotBlock 是 2026-10-24 事故的回归夹具：// 共享检出里只有别的会话的未提交变更时，本会话的 Stop 必须放行（advisory），
// 且不得消耗本会话的 block 预算。
func TestEvaluateFor_ForeignOnlyDoesNotBlock(t *testing.T) {
	pinEnv(t)
	root := initGitRepo(t)
	write(t, root, "b.go", "package b\n")
	recordLedger(t, root, "sess-b", "b.go", time.Now())

	dec, reason, err := EvaluateFor(root, "sess-a")
	if err != nil {
		t.Fatalf("EvaluateFor: %v", err)
	}
	if dec != DecisionPassAdvisory {
		t.Fatalf("只有他会话变更时应 advisory 不阻断，得 %v（%s）", dec, reason)
	}
	if !strings.Contains(reason, "其他活跃会话") {
		t.Errorf("reason 应点名其他活跃会话，得 %q", reason)
	}

	// 一次都不该累加：旧行为下这里会走到 block N/3 再兜底放行。
	for i := 0; i < MaxReviewRounds+2; i++ {
		if d, r, e := EvaluateFor(root, "sess-a"); e != nil || d != DecisionPassAdvisory {
			t.Fatalf("第 %d 次重复判定应为 advisory（不消耗预算），得 %v（%s, err=%v）", i+1, d, r, e)
		}
	}
}

// TestEvaluateFor_OwnChangeNeedsReviewAndExcludesForeign：混合场景——本会话的变更
// 仍必须审，别人的变更离开指纹；盖章范围与判定范围一致（否则盖章后下次 Stop 再被拦）。
func TestEvaluateFor_OwnChangeNeedsReviewAndExcludesForeign(t *testing.T) {
	pinEnv(t)
	root := initGitRepo(t)
	write(t, root, "a.go", "package a\n")
	write(t, root, "b.go", "package b\n")
	recordLedger(t, root, "sess-a", "a.go", time.Now())
	recordLedger(t, root, "sess-b", "b.go", time.Now())

	dec, reason, err := EvaluateFor(root, "sess-a")
	if err != nil {
		t.Fatalf("EvaluateFor: %v", err)
	}
	if dec != DecisionNeedReview {
		t.Fatalf("本会话有变更时必须需审，得 %v（%s）", dec, reason)
	}
	if !strings.Contains(reason, "本会话 1 个文件") || !strings.Contains(reason, "其他活跃会话的 1 个") {
		t.Errorf("reason 应点名本会话/他会话数量，得 %q", reason)
	}

	// 盖章-放行闭环：MarkPassedFor 用的范围必须与 EvaluateFor 相同。
	if err := MarkPassedFor(root, "sess-a", "夹具审查结论"); err != nil {
		t.Fatalf("MarkPassedFor: %v", err)
	}
	if d, r, e := EvaluateFor(root, "sess-a"); e != nil || d != DecisionPass {
		t.Fatalf("盖章后同范围应放行，得 %v（%s, err=%v）", d, r, e)
	}
}

// TestEvaluateFor_OrphanStillBlocks：台账解释不了的变更是无主的——必须留在指纹里
// 并阻断（fail-safe：解释不了就审，绝不静默丢弃）。
func TestEvaluateFor_OrphanStillBlocks(t *testing.T) {
	pinEnv(t)
	root := initGitRepo(t)
	write(t, root, "c.go", "package c\n") // 没有台账事件（如 bash 直接写）

	dec, reason, err := EvaluateFor(root, "sess-a")
	if err != nil {
		t.Fatalf("EvaluateFor: %v", err)
	}
	if dec != DecisionNeedReview {
		t.Fatalf("无主变更必须阻断，得 %v（%s）", dec, reason)
	}
}

// TestEvaluateFor_SilentSessionBlocks：那个会话在窗口内**没有任何**台账事件（已静默）→
// 它的旧写入落回无主 → 回到需审（保守方向：宁可多审）。
func TestEvaluateFor_SilentSessionBlocks(t *testing.T) {
	pinEnv(t)
	root := initGitRepo(t)
	write(t, root, "b.go", "package b\n")
	recordLedger(t, root, "sess-b", "b.go", time.Now().Add(-2*time.Hour))

	dec, reason, err := EvaluateFor(root, "sess-a")
	if err != nil {
		t.Fatalf("EvaluateFor: %v", err)
	}
	if dec != DecisionNeedReview {
		t.Fatalf("静默会话的旧写入不应被排除，得 %v（%s）", dec, reason)
	}
}

// TestEvaluateFor_AliveSessionOldWriteStillForeign：与上一条相反——B 两小时前写过 b.go，
// 但一分钟前还在改别的文件（会话活着），b.go 必须继续被排除。存活是**会话**属性：
// 只按单路径的新鲜度判定，会让长期并行的邻居像事故那样反复触发本会话门禁。
func TestEvaluateFor_AliveSessionOldWriteStillForeign(t *testing.T) {
	pinEnv(t)
	root := initGitRepo(t)
	write(t, root, "b.go", "package b\n")
	write(t, root, "c.go", "package c\n")
	recordLedger(t, root, "sess-b", "b.go", time.Now().Add(-2*time.Hour))
	recordLedger(t, root, "sess-b", "c.go", time.Now().Add(-time.Minute))

	dec, reason, err := EvaluateFor(root, "sess-a")
	if err != nil {
		t.Fatalf("EvaluateFor: %v", err)
	}
	if dec != DecisionPassAdvisory {
		t.Fatalf("活跃邻居的旧写入仍应算外来（advisory），得 %v（%s）", dec, reason)
	}
}

// TestEvaluateFor_NoSessionKeepsWholeTree：无主体（sid=""）保持历史整树语义——
// 所有既有调用方与测试不受影响。
func TestEvaluateFor_NoSessionKeepsWholeTree(t *testing.T) {
	pinEnv(t)
	root := initGitRepo(t)
	write(t, root, "b.go", "package b\n")
	recordLedger(t, root, "sess-b", "b.go", time.Now())

	dec, reason, err := EvaluateFor(root, "")
	if err != nil {
		t.Fatalf("EvaluateFor: %v", err)
	}
	if dec != DecisionNeedReview {
		t.Fatalf("无主体时必须整树判定（需审），得 %v（%s）", dec, reason)
	}
	if strings.Contains(reason, "其他活跃会话") {
		t.Errorf("无主体不应出现归属措辞，得 %q", reason)
	}
}

// TestEvaluateFor_OwnEarlierTouchKeepsPathInScope：双条件——本会话先改、别人后改同一文件时，
// last-writer 是别人，但文件里仍有本会话未审的改动，**不得**排除（否则漏审）。
func TestEvaluateFor_OwnEarlierTouchKeepsPathInScope(t *testing.T) {
	pinEnv(t)
	root := initGitRepo(t)
	write(t, root, "shared.go", "package shared\n")
	recordLedger(t, root, "sess-a", "shared.go", time.Now().Add(-10*time.Minute))
	recordLedger(t, root, "sess-b", "shared.go", time.Now().Add(-time.Minute))

	dec, reason, err := EvaluateFor(root, "sess-a")
	if err != nil {
		t.Fatalf("EvaluateFor: %v", err)
	}
	if dec != DecisionNeedReview {
		t.Fatalf("本会话碰过的文件不得因他人续写被排除，得 %v（%s）", dec, reason)
	}
}

// TestForeignWindow_EnvOverride：窗口只接受正数分钟；0/负值/非法一律回落默认。
// 0 必须回落而不是「关闭过滤」——不限时会让几小时前的他人写入仍被排除（漏审方向）。
func TestForeignWindow_EnvOverride(t *testing.T) {
	pinEnv(t)
	t.Setenv(foreignWindowEnv, "5")
	if got := foreignWindow(); got != 5*time.Minute {
		t.Errorf("FORGE_REVIEW_FOREIGN_WINDOW_MIN=5 应得 5m，得 %v", got)
	}
	for _, raw := range []string{"0", "-1", "abc", "  "} {
		t.Setenv(foreignWindowEnv, raw)
		if got := foreignWindow(); got != attribution.DefaultForeignWindow {
			t.Errorf("%q 应回落默认 %v，得 %v", raw, attribution.DefaultForeignWindow, got)
		}
	}
}

// TestEvaluateFor_AttributionDisabledSaysWholeTree：归属层关闭时必须把「按整树判定」
// 写进 reason——否则用户以为门禁已按会话收窄，实际又回到整树。
func TestEvaluateFor_AttributionDisabledSaysWholeTree(t *testing.T) {
	pinEnv(t)
	root := initGitRepo(t)
	write(t, root, "a.go", "package a\n")
	t.Setenv("FORGE_ATTRIBUTION", "0")

	dec, reason, err := EvaluateFor(root, "sess-a")
	if err != nil {
		t.Fatalf("EvaluateFor: %v", err)
	}
	if dec != DecisionNeedReview {
		t.Fatalf("应需审，得 %v（%s）", dec, reason)
	}
	if !strings.Contains(reason, "整树") {
		t.Errorf("归属层关闭时 reason 应标注按整树判定，得 %q", reason)
	}
}

// TestEvaluateFor_SymmetricOwnership：判据 1 的对称半边——A 只看 a.go，B 只看 b.go。
// 只测一侧会漏掉「排除方向写反」这类错误（谁被排除取决于 sid，不是取决于文件）。
func TestEvaluateFor_SymmetricOwnership(t *testing.T) {
	pinEnv(t)
	root := initGitRepo(t)
	write(t, root, "a.go", "package a\n")
	write(t, root, "b.go", "package b\n")
	recordLedger(t, root, "sess-a", "a.go", time.Now())
	recordLedger(t, root, "sess-b", "b.go", time.Now())

	for _, tc := range []struct{ sid, own, other string }{
		{"sess-a", "a.go", "b.go"},
		{"sess-b", "b.go", "a.go"},
	} {
		dec, reason, err := EvaluateFor(root, tc.sid)
		if err != nil {
			t.Fatalf("%s: EvaluateFor: %v", tc.sid, err)
		}
		if dec != DecisionNeedReview {
			t.Fatalf("%s 应需审，得 %v（%s）", tc.sid, dec, reason)
		}
		if !strings.Contains(reason, "本会话 1 个文件") || !strings.Contains(reason, "其他活跃会话的 1 个") {
			t.Errorf("%s 的 reason 应点名各 1 个文件，得 %q", tc.sid, reason)
		}
	}
}

// TestEvaluateFor_WholeTreeStampSatisfiesScopedGate 是 M1 的回归夹具：`forge review pass`
// 作为独立 CLI 进程可能拿不到会话 id（dsh 只在 hook 子进程注入），于是盖的是整树戳。
// 整树审查是作用域审查的超集——混合场景下盖章后 Stop 必须放行，不能落入
// 「盖章 → 再被拦 → 3/3 兜底」的循环。
func TestEvaluateFor_WholeTreeStampSatisfiesScopedGate(t *testing.T) {
	pinEnv(t)
	root := initGitRepo(t)
	write(t, root, "a.go", "package a\n")
	write(t, root, "b.go", "package b\n")
	recordLedger(t, root, "sess-a", "a.go", time.Now())
	recordLedger(t, root, "sess-b", "b.go", time.Now())

	if d, r, _ := EvaluateFor(root, "sess-a"); d != DecisionNeedReview {
		t.Fatalf("前置：混合场景应需审，得 %v（%s）", d, r)
	}
	// 模拟无身份的 CLI 盖章：整树范围。
	if err := MarkPassedFor(root, "", "整树审查结论"); err != nil {
		t.Fatalf("MarkPassedFor(整树): %v", err)
	}
	if d, r, e := EvaluateFor(root, "sess-a"); e != nil || d != DecisionPass {
		t.Fatalf("整树审查后作用域门禁应放行，得 %v（%s, err=%v）", d, r, e)
	}
}

// TestEvaluateFor_NonASCIIPathExcluded：中文名文件必须能被归属排除命中。git 默认把路径
// 转义成 "\344\270\255..."，而台账是原生 UTF-8——不做 quotepath=off 时两侧永不相等：
// 排除集失效（多审），内容指纹也取不到内容（漏审）。本机仓库路径就是中文，必然踩到。
func TestEvaluateFor_NonASCIIPathExcluded(t *testing.T) {
	pinEnv(t)
	root := initGitRepo(t)
	write(t, root, "中文.go", "package cn\n")
	recordLedger(t, root, "sess-b", "中文.go", time.Now())

	dec, reason, err := EvaluateFor(root, "sess-a")
	if err != nil {
		t.Fatalf("EvaluateFor: %v", err)
	}
	if dec != DecisionPassAdvisory {
		t.Fatalf("中文名外来变更应被排除（advisory），得 %v（%s）", dec, reason)
	}
}

// TestEvaluateFor_MaxRoundsAdvisory：本会话自己的变更连续撞满 MaxReviewRounds 后必须
// advisory 放行（防 Stop 死循环）——这是「兜底比漏审更安全」的唯一定义处，不能被
// 静默去掉。同时钉住：轮次计数必须绑定当前指纹，换了内容就重置。
func TestEvaluateFor_MaxRoundsAdvisory(t *testing.T) {
	pinEnv(t)
	root := initGitRepo(t)
	write(t, root, "a.go", "package a\n")
	recordLedger(t, root, "sess-a", "a.go", time.Now())

	for i := 1; i <= MaxReviewRounds; i++ {
		dec, _, err := EvaluateFor(root, "sess-a")
		if err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
		if dec != DecisionNeedReview {
			t.Fatalf("round %d 应为需审，得 %v", i, dec)
		}
	}
	dec, reason, err := EvaluateFor(root, "sess-a")
	if err != nil {
		t.Fatalf("超限轮: %v", err)
	}
	if dec != DecisionPassAdvisory {
		t.Fatalf("撞满 %d 轮应 advisory 放行，得 %v（%s）", MaxReviewRounds, dec, reason)
	}
}

// TestScopeAndWholeFingerprint_UnreachableBase：基线不可达（amend/rebase 后旧 commit 消失）
// 必须返回错误让调用方 fail-open，而不是把 git 的失败当成「无变更」静默放行。
func TestScopeAndWholeFingerprint_UnreachableBase(t *testing.T) {
	pinEnv(t)
	root := initGitRepo(t)
	write(t, root, "a.go", "package a\n")
	if _, _, _, err := scopeAndWholeFingerprint(root, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", nil); err == nil {
		t.Fatal("不可达基线应返回错误，而不是当成无变更")
	}
}

// TestEvaluateFor_MaxRoundsWithWholeTreeLegacyStamp：真实生产循环的完整形态——
// dsh 上 `forge review pass` 是无身份 CLI，盖的是整树戳；随后 hook 里的会话带来外来
// 路径，作用域戳与整树戳不同；连续撞满 MaxReviewRounds 后落兜底必须 advisory 放行，
// 不能因为「戳的 hash 是整树、判定的 hash 是作用域」就判不出已是最后一轮而永久阻断。
//
// 这条夹具钉住撞轮条件里的第二个析取（stamp.DiffHash == wholeHash）：把它改成 != 后，
// 本用例从 advisory 变回需审，测试即失败。
func TestEvaluateFor_MaxRoundsWithWholeTreeLegacyStamp(t *testing.T) {
	pinEnv(t)
	root := initGitRepo(t)
	write(t, root, "a.go", "package a\n")
	write(t, root, "b.go", "package b\n")
	recordLedger(t, root, "sess-a", "a.go", time.Now())
	recordLedger(t, root, "sess-b", "b.go", time.Now())

	_, whole, _, err := scopeAndWholeFingerprint(root, "", nil)
	if err != nil || whole == "" {
		t.Fatalf("整树指纹: hash=%q err=%v", whole, err)
	}
	// 前提守卫：本夹具的杀灭力依赖「排除 b.go 后 scope 与 whole 不同」。归属过滤若静默
	// 失效，两个指纹会相等，真实现与变异体都会绿——杀灭力无声消失而无人察觉。
	scoped, _, _, err := scopeAndWholeFingerprint(root, "", map[string]bool{"b.go": true})
	if err != nil {
		t.Fatalf("作用域指纹: %v", err)
	}
	if scoped == whole {
		t.Fatal("前提不成立：排除 b.go 后作用域指纹仍等于整树指纹，外来路径未被真正排除")
	}
	// 旧式分支戳（只读兼容路径）：整树 hash + 已撞满轮次，但未审。
	legacy := Stamp{DiffHash: whole, Reviewed: false, BlockCount: MaxReviewRounds}
	data, err := json.MarshalIndent(&legacy, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(stampPath(root)), 0o755); err != nil {
		t.Fatalf("mkdir stamps: %v", err)
	}
	if err := os.WriteFile(stampPath(root), data, 0o644); err != nil {
		t.Fatalf("write legacy stamp: %v", err)
	}

	dec, reason, err := EvaluateFor(root, "sess-a")
	if err != nil {
		t.Fatalf("EvaluateFor: %v", err)
	}
	if dec != DecisionPassAdvisory {
		t.Fatalf("整树戳 + 外来路径 + 撞满轮次应 advisory 放行，得 %v（%s）", dec, reason)
	}
	if !strings.Contains(reason, "max review rounds") {
		t.Errorf("兜底理由应点名轮次上限，得 %q", reason)
	}
}
