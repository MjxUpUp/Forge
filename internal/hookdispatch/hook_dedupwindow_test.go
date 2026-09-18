package hookdispatch

// hook_dedupwindow_test.go —— double-fire 去重窗口的 env 旋钮守卫（发布流程排查
// P2-2：e2e 串行跑两次 hook 进程，快机进程启动 <3s 碰巧在窗口内、慢 Windows
// runner 一跑就超窗记两条——测试钉了个依赖进程启动速度的伪契约）。生产默认
// 3s（2026-08-24 证据 0.5~1.9s）不变；e2e 注入宽窗让 double-fire 场景稳定复现
// "窗口内双发只记一条"的真实契约。

import (
	"testing"
	"time"
)

// TestBlockDedupWindowEnvOverride pins the knob: default 3s; the env override
// is clamped to [1s, 60s] — a wider window only under-records duplicate
// deliveries (audit de-noising), never hides a block verdict, so the clamp is
// about sanity, not security.
//
// TestBlockDedupWindowEnvOverride 钉住旋钮：默认 3s；env 覆盖钳制在
// [1s, 60s]——调宽只会少记重复投递（审计去噪），绝不隐藏阻断判定，钳制是
// 理智约束而非安全约束。
func TestBlockDedupWindowEnvOverride(t *testing.T) {
	t.Setenv("FORGE_BLOCK_DEDUP_WINDOW_MS", "")
	if got := blockDedupWindow(); got != 3*time.Second {
		t.Fatalf("default window = %v, want 3s", got)
	}
	t.Setenv("FORGE_BLOCK_DEDUP_WINDOW_MS", "60000")
	if got := blockDedupWindow(); got != 60*time.Second {
		t.Fatalf("env 60000 → %v, want 60s", got)
	}
	t.Setenv("FORGE_BLOCK_DEDUP_WINDOW_MS", "10") // 低于下限
	if got := blockDedupWindow(); got != 3*time.Second {
		t.Fatalf("env 10 (below floor) → %v, want default 3s", got)
	}
	t.Setenv("FORGE_BLOCK_DEDUP_WINDOW_MS", "not-a-number")
	if got := blockDedupWindow(); got != 3*time.Second {
		t.Fatalf("invalid env → %v, want default 3s", got)
	}
}
