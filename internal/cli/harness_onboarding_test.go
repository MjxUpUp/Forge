package cli

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestHarnessOnboarding_StateMachineAndCooldown 钉住 T7（multi-task-concurrency §13）：
// uninitialized → offered（计数 + cooldown——同日第二次触点静默）→ 经
// MarkHarnessInitialized 进入 initialized/linked；提示上限后永久静默。
func TestHarnessOnboarding_StateMachineAndCooldown(t *testing.T) {
	home := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", home)

	if got := readHarnessState(home); got != harnessStateUninitialized {
		t.Fatalf("初始应 uninitialized, got %q", got)
	}
	MaybeOfferHarness("test") // → offered 1
	if got := readHarnessState(home); got != harnessStateOffered {
		t.Fatalf("首次提示后应 offered, got %q", got)
	}
	data, _ := os.ReadFile(harnessStatePath(home))
	if !strings.Contains(string(data), "offered 1") {
		t.Fatalf("offered 计数应 1, got %q", data)
	}
	MaybeOfferHarness("test") // cooldown 窗口内：计数不变
	data, _ = os.ReadFile(harnessStatePath(home))
	if !strings.Contains(string(data), "offered 1") {
		t.Fatalf("cooldown 内二次触点应静默, got %q", data)
	}

	MarkHarnessInitialized(false)
	if got := readHarnessState(home); got != harnessStateInitialized {
		t.Fatalf("init 后应 initialized, got %q", got)
	}
	MarkHarnessInitialized(true)
	if got := readHarnessState(home); got != harnessStateLinked {
		t.Fatalf("配远端应 linked, got %q", got)
	}

	// 上限：重置为未建立并连触 maxHarnessOffers+2 次（伪造过期 mtime 穿透 cooldown）。
	if err := os.Remove(harnessStatePath(home)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxHarnessOffers+2; i++ {
		past := time.Now().Add(-25 * time.Hour)
		if i > 0 {
			_ = os.Chtimes(harnessStatePath(home), past, past)
		}
		MaybeOfferHarness("test")
	}
	data, _ = os.ReadFile(harnessStatePath(home))
	if !strings.Contains(string(data), fmt.Sprintf("offered %d", maxHarnessOffers)) {
		t.Fatalf("提示应停在上限 %d, got %q", maxHarnessOffers, data)
	}
}
