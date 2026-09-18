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
// TestBlockDedupWindowEnvOverride 钉住旋钮：默认 3s；env 覆盖**真钳制**到
// [1s, 60s]（越界压到边界而非静默弃用——复审 P2：弃用会让后来者调宽时静默
// 拿回 3s，本旋钮要修的 flaky 换形态回归）；调宽只会少记重复投递（审计去
// 噪），绝不隐藏阻断判定。
func TestBlockDedupWindowEnvOverride(t *testing.T) {
	t.Setenv("FORGE_BLOCK_DEDUP_WINDOW_MS", "")
	if got := blockDedupWindow(); got != 3*time.Second {
		t.Fatalf("default window = %v, want 3s", got)
	}
	t.Setenv("FORGE_BLOCK_DEDUP_WINDOW_MS", "60000")
	if got := blockDedupWindow(); got != 60*time.Second {
		t.Fatalf("env 60000 → %v, want 60s", got)
	}
	t.Setenv("FORGE_BLOCK_DEDUP_WINDOW_MS", "1000") // 恰下界合法
	if got := blockDedupWindow(); got != time.Second {
		t.Fatalf("env 1000 (floor) → %v, want 1s", got)
	}
	t.Setenv("FORGE_BLOCK_DEDUP_WINDOW_MS", "10") // 低于下限 → 真钳制提到下界（复审 P2）
	if got := blockDedupWindow(); got != time.Second {
		t.Fatalf("env 10 (below floor) → %v, want clamped 1s (NOT silent default)", got)
	}
	t.Setenv("FORGE_BLOCK_DEDUP_WINDOW_MS", "90000") // 越上界 → 压到 60s（复审 P2：弃用会让放宽静默失效）
	if got := blockDedupWindow(); got != 60*time.Second {
		t.Fatalf("env 90000 (above ceiling) → %v, want clamped 60s (NOT silent default)", got)
	}
	t.Setenv("FORGE_BLOCK_DEDUP_WINDOW_MS", "99999999999999999999") // 超 int64 → Atoi 拒绝 → 默认（钳制先于乘法，int64 内大数无回绕）
	if got := blockDedupWindow(); got != 3*time.Second {
		t.Fatalf("beyond-int64 env → %v, want default 3s (Atoi rejects; in-range big values clamp before multiply)", got)
	}
	t.Setenv("FORGE_BLOCK_DEDUP_WINDOW_MS", "not-a-number")
	if got := blockDedupWindow(); got != 3*time.Second {
		t.Fatalf("invalid env → %v, want default 3s", got)
	}
}
