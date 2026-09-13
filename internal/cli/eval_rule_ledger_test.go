package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRuleLedgerRoundTrip：台账追加/读取往返 + claim/verify 两形态字段 +
// 损坏行 fail-closed（台账是治理证据，静默跳过=篡改不可见）。
func TestRuleLedgerRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rule-ledger.jsonl")
	entries, err := loadRuleLedger(path)
	if err != nil || entries != nil {
		t.Fatalf("不存在应返回 nil,nil，got %+v %v", entries, err)
	}

	ts := time.Now()
	pass := true
	if err := appendRuleLedger(path, ruleLedgerEntry{TS: ts, Kind: "claim", Rule: "cheat-scan/weak-assert", Claim: "拦截弱断言家族变体", Probe: "go test ./internal/hooks/ -run TestHazardGuardScript_GuardFallQuoteMerge"}); err != nil {
		t.Fatal(err)
	}
	if err := appendRuleLedger(path, ruleLedgerEntry{TS: ts, Kind: "verify", Rule: "cheat-scan/weak-assert", Probe: "同上", Pass: &pass, Verge: "ok PASS"}); err != nil {
		t.Fatal(err)
	}

	entries, err = loadRuleLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Kind != "claim" || entries[1].Kind != "verify" {
		t.Fatalf("往返不符: %+v", entries)
	}
	if entries[0].Claim == "" || entries[0].Probe == "" {
		t.Errorf("claim 条目缺字段: %+v", entries[0])
	}
	if entries[1].Pass == nil || !*entries[1].Pass {
		t.Errorf("verify 结论应解析为 true，got %+v", entries[1].Pass)
	}

	// 损坏行 fail-closed：台账静默吞坏行 = 篡改不可见。
	if err := os.WriteFile(path, []byte("{corrupt}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRuleLedger(path); err == nil {
		t.Error("损坏台账应报错（fail-closed），不应静默跳过")
	}
}
