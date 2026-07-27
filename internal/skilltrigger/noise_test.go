package skilltrigger

import (
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
