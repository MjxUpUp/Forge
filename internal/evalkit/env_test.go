package evalkit

// env_test.go — drillEnv 键唯一性契约（chore/evalkit-subproc-env-dedup）：
// drill 子进程 env 必须对 HOME/FORGE_DATA_HOME 恰有一条且为注入值——重复键下
// Go 的 getenv 取首个、shell 取末个，两类消费者解析分叉（只读子 agent 审查
// SUGGEST-2，2026-09-09）。包 TestMain 已设两键，恰好把"宿主残留键导致重复"
// 的场景常态化的兜底契约钉在这里。

import (
	"strings"
	"testing"
)

func TestDrillEnvSingleOwnerKeys(t *testing.T) {
	// TestMain 已注入包级 HOME/FORGE_DATA_HOME——drillEnv 必须替换而非追加。
	env := drillEnv("/drill-tmp", "/drill-data")
	var home, dataHome int
	for _, e := range env {
		switch {
		case strings.HasPrefix(e, "HOME="):
			home++
			if e != "HOME=/drill-tmp" {
				t.Fatalf("HOME 应为注入值: %q", e)
			}
		case strings.HasPrefix(e, "FORGE_DATA_HOME="):
			dataHome++
			if e != "FORGE_DATA_HOME=/drill-data" {
				t.Fatalf("FORGE_DATA_HOME 应为注入值: %q", e)
			}
		}
	}
	if home != 1 || dataHome != 1 {
		t.Fatalf("HOME/FORGE_DATA_HOME 应各恰 1 条（实际 %d/%d）——重复键导致 Go getenv 首键优先与 shell 末键优先解析分叉", home, dataHome)
	}
	// 基础 env 其余键必须保留（PATH 等 drill 子进程依赖）。
	if len(env) < 3 {
		t.Fatalf("基础环境键不应被丢弃: %d 条", len(env))
	}
}

func TestDrillEnvKeepsUnrelatedKeys(t *testing.T) {
	t.Setenv("EVALKIT_DRILL_MARKER", "keep-me")
	found := false
	for _, e := range drillEnv("/t", "/d") {
		if e == "EVALKIT_DRILL_MARKER=keep-me" {
			found = true
		}
	}
	if !found {
		t.Fatal("无关键被误滤")
	}
}
