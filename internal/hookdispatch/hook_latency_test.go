package hookdispatch

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// PreToolUse 耗时预算硬门（W0.2 的 CI 侧交付；W0 宪法「慢钩子=被卸载的钩子」）。
//
// Claude Code 给 hook 的总预算是 60s，但用户可感知的阈值远低于此——METR 实测
// AI 协作摩擦感在秒级即成立，guard 被卸载的第一原因是「重」。本测试把当前
// PreToolUse/Bash matcher 组（bash-guard → hazard-guard → gate-cmd-form →
// skill-trigger，2 个 bash embed + 2 个 Go 内 hook）端到端跑 N 次取分布，钉住：
//   - 单次上限 hard budget：防任何一次病态拖慢（hook 超时会被宿主按 fail 处理）；
//   - 均值 budget：防慢性膨胀（新增检查/依赖加载把 P95 一点点推高）。
//
// 预算依据（宽松到 CI runner 噪音之下，但远紧于宿主 60s）：本地实测单次
// 全组 mean ~102-109ms / max ~221-246ms（bash 拉起占大头；两轮独立复测一致）；
// CI 容器冷启动方差大，取 mean ≤ 2s / max ≤ 5s。收紧预算时改这里的常量并在
// commit message 里附实测分布——数字只许有记录地变动（与 LOC 棘轮同一纪律）。
func TestPreToolUseBash_LatencyBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过耗时预算（本地/夜间跑全量时生效）")
	}
	t.Setenv("FORGE_PROFILE", "standard")
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	// cwd 恢复：泄漏的 chdir 会级联搞挂同包/并行包的相对路径测试（首跑实证）。
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	// 有 .forge 目录（项目判定成立）+ 良性只读命令：走完 bash-guard 快照、
	// hazard-guard 语义分词、gate-cmd-form、skill-trigger 全组，但全部放行——
	// 量的是【放行路径】的成本（拦截路径的 confirm 链有宿主 IO，不进预算）。
	stdin := `{"hook_event_name":"PreToolUse","tool_name":"Bash","session_id":"lat","tool_input":{"command":"ls -la"}}`

	const runs = 10
	const maxPerRun = 5 * time.Second
	const meanBudget = 2 * time.Second
	durations := make([]time.Duration, 0, runs)
	for i := 0; i < runs; i++ {
		start := time.Now()
		out, hookErr := runHookCapture(t, "bash-guard", stdin)
		_ = out
		if hookErr != nil {
			t.Fatalf("run %d: 放行路径不得报错: %v", i, hookErr)
		}
		d := time.Since(start)
		durations = append(durations, d)
		if d > maxPerRun {
			t.Fatalf("run %d 耗时 %v 超单次预算 %v——检查最近是否给 PreToolUse/Bash 组加了重活（预算见测试头注，收紧/放宽都要附实测分布）", i, d, maxPerRun)
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	var total time.Duration
	for _, d := range durations {
		total += d
	}
	mean := total / time.Duration(runs)
	if mean > meanBudget {
		t.Errorf("PreToolUse/Bash 全组均值 %v 超预算 %v——hook 组在慢性变重（这正是要拦的）；先跑 `forge eval dead-checks` 找该删的检查，再谈调预算", mean, meanBudget)
	}
	t.Logf("PreToolUse/Bash 放行路径：mean=%v max=%v（runs=%d，预算 mean %v / max %v）",
		mean, durations[len(durations)-1], runs, meanBudget, maxPerRun)
}
