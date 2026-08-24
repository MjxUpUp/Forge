package skilltrigger

import (
	"os"
	"testing"
	"time"
)

func TestFileNoise_Cooldown(t *testing.T) {
	n := NewFileNoiseController(t.TempDir())
	now := time.Now()
	if !n.ShouldFire("s1", "foo", 60*time.Second, now) {
		t.Fatal("首次应允许")
	}
	if err := n.Mark("s1", "foo", now); err != nil {
		t.Fatalf("Mark 失败: %v", err)
	}
	if n.ShouldFire("s1", "foo", 60*time.Second, now.Add(10*time.Second)) {
		t.Fatal("cooldown 内应拒绝")
	}
	if !n.ShouldFire("s1", "foo", 60*time.Second, now.Add(61*time.Second)) {
		t.Fatal("超 cooldown 应允许")
	}
	// 不同 skill 互不影响
	if !n.ShouldFire("s1", "bar", 60*time.Second, now) {
		t.Fatal("不同 skill 应允许")
	}
}

func TestFileNoise_StopRounds(t *testing.T) {
	n := NewFileNoiseController(t.TempDir())
	now := time.Now()
	for i := 0; i < MaxStopRounds; i++ {
		if !n.StopRoundAllowed("s1", now) {
			t.Fatalf("第 %d 轮应允许", i+1)
		}
		if err := n.IncrStopRound("s1"); err != nil {
			t.Fatalf("IncrStopRound: %v", err)
		}
	}
	if n.StopRoundAllowed("s1", now) {
		t.Fatalf("第 %d 轮应拒绝", MaxStopRounds+1)
	}
	// 不同 session 互不影响
	if !n.StopRoundAllowed("s2", now) {
		t.Fatal("不同 session 应允许")
	}
}

func TestFileNoise_GlobalDisable(t *testing.T) {
	t.Setenv("FORGE_SKILL_TRIGGER", "0")
	n := NewFileNoiseController(t.TempDir())
	if n.ShouldFire("s1", "foo", 60*time.Second, time.Now()) {
		t.Fatal("全局禁用 ShouldFire 应拒绝")
	}
	if n.StopRoundAllowed("s1", time.Now()) {
		t.Fatal("全局禁用 StopRoundAllowed 应拒绝")
	}
}

func TestFileNoise_PerSkillDisable(t *testing.T) {
	t.Setenv("FORGE_SKILL_TRIGGER_DISABLE", "foo,bar")
	n := NewFileNoiseController(t.TempDir())
	if n.ShouldFire("s1", "foo", 60*time.Second, time.Now()) {
		t.Fatal("foo 被禁应拒绝")
	}
	if !n.ShouldFire("s1", "baz", 60*time.Second, time.Now()) {
		t.Fatal("baz 未禁应允许")
	}
}

func TestInMemoryNoise_Cooldown(t *testing.T) {
	m := NewInMemoryNoiseController()
	now := time.Now()
	if !m.ShouldFire("s1", "foo", 60*time.Second, now) {
		t.Fatal("首次应允许")
	}
	m.Mark("s1", "foo", now)
	if m.ShouldFire("s1", "foo", 60*time.Second, now) {
		t.Fatal("cooldown 内应拒绝")
	}
	if !m.ShouldFire("s1", "foo", 60*time.Second, now.Add(61*time.Second)) {
		t.Fatal("超 cooldown 应允许")
	}
}

func TestInMemoryNoise_StopRounds(t *testing.T) {
	m := NewInMemoryNoiseController()
	now := time.Now()
	for i := 0; i < MaxStopRounds; i++ {
		if !m.StopRoundAllowed("s1", now) {
			t.Fatalf("第 %d 轮应允许", i+1)
		}
		m.IncrStopRound("s1")
	}
	if m.StopRoundAllowed("s1", now) {
		t.Fatalf("第 %d 轮应拒绝", MaxStopRounds+1)
	}
}

// TestFileNoise_ClockRollback: F5 回归——marker mtime 在未来（时钟倒退，如写 marker 后系统时钟
// 被回拨）时，ShouldFire 不应因 now.Sub(mtime) 为负 < cooldown 而永久阻塞，应允许 fire。
func TestFileNoise_ClockRollback(t *testing.T) {
	n := NewFileNoiseController(t.TempDir())
	now := time.Now()
	// 先 Mark 建文件，再用 os.Chtimes 显式把 mtime 推到未来——独立于 OS mtime 分辨率。
	// 若靠 Mark(now+100s) 写内容，文件 mtime 仍由 OS 在写时点设为 ~T0；在 HFS+/粗粒度文件系统
	// 上 mtime 可能被截断到 now 整秒，now.Sub(mtime)≈0 < cooldown → 误 block → 测试 flake。
	if err := n.Mark("s1", "foo", now); err != nil {
		t.Fatalf("Mark 失败: %v", err)
	}
	future := now.Add(100 * time.Second)
	if err := os.Chtimes(n.markerPath("s1", "foo"), future, future); err != nil {
		t.Fatalf("Chtimes 失败: %v", err)
	}
	if !n.ShouldFire("s1", "foo", 60*time.Second, now) {
		t.Fatal("时钟回退（marker mtime 在未来）应允许 fire，不应因负 duration 永久阻塞")
	}
}

// TestInMemoryNoise_ClockRollback: F5 内存态版同上。
func TestInMemoryNoise_ClockRollback(t *testing.T) {
	m := NewInMemoryNoiseController()
	now := time.Now()
	m.Mark("s1", "foo", now.Add(100*time.Second))
	if !m.ShouldFire("s1", "foo", 60*time.Second, now) {
		t.Fatal("时钟回退（marker mtime 在未来）应允许 fire，不应因负 duration 永久阻塞")
	}
}

// TestFileNoise_FireCount 钉住 session 硬封顶的计数面：Mark 累计 FireCount，跨 skill /
// session 隔离，无文件 = 0。
//
// TestFileNoise_FireCount pins the counting side of the session hard cap: Mark
// accumulates FireCount, isolated per skill/session, absent file = 0.
func TestFileNoise_FireCount(t *testing.T) {
	n := NewFileNoiseController(t.TempDir())
	now := time.Now()
	if n.FireCount("s1", "foo") != 0 {
		t.Fatal("未注入应计 0")
	}
	for i := 1; i <= 3; i++ {
		if err := n.Mark("s1", "foo", now); err != nil {
			t.Fatalf("Mark: %v", err)
		}
		if got := n.FireCount("s1", "foo"); got != i {
			t.Fatalf("第 %d 次 Mark 后 FireCount=%d", i, got)
		}
	}
	if n.FireCount("s1", "bar") != 0 || n.FireCount("s2", "foo") != 0 {
		t.Fatal("计数应按 skill/session 隔离")
	}
}

func TestInMemoryNoise_FireCount(t *testing.T) {
	m := NewInMemoryNoiseController()
	now := time.Now()
	m.Mark("s1", "foo", now)
	m.Mark("s1", "foo", now)
	if got := m.FireCount("s1", "foo"); got != 2 {
		t.Fatalf("FireCount=%d, want 2", got)
	}
	if m.FireCount("s1", "bar") != 0 {
		t.Fatal("不同 skill 应计 0")
	}
}
