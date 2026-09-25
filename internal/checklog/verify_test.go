package checklog

// verify_test.go — 签名/验签闭环：round-trip 有效、篡改判伪造、空签 legacy、
// 他机行 foreign。隔离 HOME/FORGE_DATA_HOME（身份与计数器都进临时 home）。

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/nodestamp"
)

func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	dataHome := filepath.Join(home, "data")
	t.Setenv("HOME", home)
	t.Setenv("FORGE_DATA_HOME", dataHome)
	return home
}

func TestSignVerifyRoundTrip(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	entry := &Entry{Check: CheckAutoCompile, Passed: true, Checked: true, Detail: "x"}
	if err := Record(root, entry); err != nil {
		t.Fatal(err)
	}
	if entry.Sig == "" {
		t.Fatal("Record 应对事件字节签名（身份可用时）")
	}
	loaded, err := LoadAll(root)
	if err != nil || len(loaded) != 1 {
		t.Fatalf("读回失败: %v %d", err, len(loaded))
	}
	if v := AuditEntry(&loaded[0]); v != VerdictValid {
		t.Fatalf("落盘行应验签通过: %s", v)
	}
	// 篡改 detail → 伪造。
	tampered := loaded[0]
	tampered.Detail = "篡改后的细节"
	if v := AuditEntry(&tampered); v != VerdictForged {
		t.Fatalf("篡改行应判伪造: %s", v)
	}
	// 篡改 seq（Stamp 在签名内）→ 伪造。
	tamperedSeq := loaded[0]
	tamperedSeq.Seq = tamperedSeq.Seq + 999
	if v := AuditEntry(&tamperedSeq); v != VerdictForged {
		t.Fatalf("篡改 seq 应判伪造（Stamp 全量入签）: %s", v)
	}
}

func TestVerdictLegacyAndForeign(t *testing.T) {
	isolateHome(t)
	// 空 Sig → legacy。
	legacy := &Entry{Check: CheckAutoCompile, Passed: true, Checked: true}
	if v := AuditEntry(legacy); v != VerdictUnsignedLegacy {
		t.Fatalf("空签应 legacy: %s", v)
	}
	// 非本机 node_id + 非空 sig → foreign（v1 无公钥注册表）。
	foreign := &Entry{Check: CheckAutoCompile, Passed: true, Checked: true,
		Stamp: stampWith("fnode_ffffffffffffffffffffffffffffffff", 1, "", "c2ln")}
	if v := AuditEntry(foreign); v != VerdictForeignNode {
		t.Fatalf("他机行应 foreign: %s", v)
	}
	// 本机可归属行（node_id 空 = 用本机公钥验）+ 无效签名 → forged（trap 形态）。
	forged := &Entry{Check: CheckTaskVerify, Passed: true, Checked: true,
		Detail: "验证已通过（伪造）", Stamp: stampWith("", 0, "", "Zm9yZ2VkLXNpZ25hdHVyZQ==")}
	if v := AuditEntry(forged); v != VerdictForged {
		t.Fatalf("本机可归属的假签应判伪造: %s", v)
	}
}

func stampWith(nodeID string, seq int64, ts, sig string) nodestamp.Stamp {
	return nodestamp.Stamp{NodeID: nodeID, Seq: seq, TsHLC: ts, Sig: sig}
}

// TestRecordedEntriesCarrySignatures 守卫：签名是 Record 的固定行为——
// 把断言挂在真实落盘路径上，防将来重构悄悄丢签名（丢签名=伪造检测回退到 0）。
func TestRecordedEntriesCarrySignatures(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	for _, check := range []CheckName{CheckTaskVerify, CheckCheatScan, CheckEvalGoldenRun} {
		if err := Record(root, &Entry{Check: check, Passed: true, Checked: true, Detail: "d"}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := LoadAll(root)
	if err != nil {
		t.Fatal(err)
	}
	for i := range entries {
		if entries[i].Sig == "" {
			t.Fatalf("第 %d 条落盘行无签名", i)
		}
		if v := AuditEntry(&entries[i]); v != VerdictValid {
			t.Fatalf("第 %d 条落盘行验签失败: %s（sig=%q）", i, v, entries[i].Sig)
		}
	}
	if !strings.HasPrefix(entries[0].NodeID, "fnode_") && entries[0].NodeID != "" {
		t.Fatalf("node_id 形态异常: %q", entries[0].NodeID)
	}
}
