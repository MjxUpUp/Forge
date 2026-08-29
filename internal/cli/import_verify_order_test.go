package cli

// import_verify_order_test.go — pins the ORDER of the import pipeline:
//   digest → ledger dedup → trust verdict → unpack.
// The attacker-controlled tar.gz must not be parsed before the trust gate (an
// unpack-layer parse bug would fire ahead of a team-profile hard-reject), and a
// re-imported bundle must be skipped on its whole-file digest alone (no tar parse
// at all — repeated pulls are free, as the CLI text promises).
//
// import_verify_order_test.go —— 钉死导入管线的顺序：摘要 → 账本查重 → 信任判定
// → 解包。攻击者可控的 tar.gz 不得在信任闸门之前被解析（Unpack 层解析缺陷会抢在
// 团队档硬拒之前触发），且重复导入必须只凭整文件摘要就跳过（完全不付 tar 解析
// 成本——重复 pull 免费，如 CLI 文案承诺）。

import (
	"strings"
	"testing"
)

// TestImport_TrustGateFiresBeforeUnpack: a byte-flipped (gzip-broken) bundle that
// still carries its original .sig must be rejected BY THE SIGNATURE LAYER — under
// the old order the unpack layer fired first, so any unpack parse bug would have
// run on attacker bytes before the trust verdict.
//
// TestImport_TrustGateFiresBeforeUnpack：翻转字节（gzip 已坏）但保留原 .sig 的
// bundle 必须被签名层拒收——旧顺序下解包层先触发，Unpack 的任何解析缺陷都会在
// 信任判定之前吃下攻击者字节。
func TestImport_TrustGateFiresBeforeUnpack(t *testing.T) {
	resetProjectCmdFlags(t)
	id := `fpid_9999aaaabbbbccccddddeeee00001111`
	machineA, machineB, homeA, homeB, key := twoMachineFixture(t, id)
	bundle := exportOnMachine(t, machineA, homeA, key)

	// Register A on B so the verdict is digest-driven (Verified vs SigInvalid),
	// not unknown-signer (which personal profile would only warn about).
	registerSignerOnB(t, homeA, machineB, homeB)

	// Flip one byte (breaks gzip) and KEEP the original sidecar: the digest no
	// longer matches the signature → the signature layer must be the rejecter.
	flipped := flipBundleByte(t, bundle)

	withMachine(t, machineB, homeB, func() {
		_, err := runImport(t, map[string]string{}, flipped)
		if err == nil {
			t.Fatal("tampered bundle must be rejected")
		}
		if !strings.Contains(err.Error(), `签名验证失败`) {
			t.Fatalf("the SIGNATURE layer must reject before the bundle is unpacked, got: %v", err)
		}
		if strings.Contains(err.Error(), `bundle 校验失败`) {
			t.Fatalf("unpack ran before the trust gate — attacker bytes were parsed by the tar layer first: %v", err)
		}
	})
}

// TestImport_ReimportSkipsOnDigestBeforeUnpack: importing the SAME bundle file a
// second time must short-circuit on the whole-file digest (ledger hit) — never
// paying the tar parse. The skip message names the digest hit.
//
// TestImport_ReimportSkipsOnDigestBeforeUnpack：同一 bundle 文件二次导入必须以
// 整文件摘要短路（账本命中）——绝不付 tar 解析成本。跳过消息注明 digest 命中。
func TestImport_ReimportSkipsOnDigestBeforeUnpack(t *testing.T) {
	resetProjectCmdFlags(t)
	id := `fpid_8888bbbbccccddddeeeeffff00002222`
	machineA, machineB, homeA, homeB, key := twoMachineFixture(t, id)
	bundle := exportOnMachine(t, machineA, homeA, key)

	withMachine(t, machineB, homeB, func() {
		if out, err := runImport(t, map[string]string{}, bundle); err != nil {
			t.Fatalf(`first import: %v`+"\n"+`%s`, err, out)
		}
		out, err := runImport(t, map[string]string{}, bundle)
		if err != nil {
			t.Fatalf(`second import must be a clean skip, got: %v`+"\n"+`%s`, err, out)
		}
		if !strings.Contains(out, `digest 命中`) {
			t.Fatalf("re-import must skip on the whole-file digest BEFORE unpacking, got: %s", out)
		}
	})
}
