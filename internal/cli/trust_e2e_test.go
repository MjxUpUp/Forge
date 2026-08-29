package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/nodeid"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// trust_e2e_test.go — the sign→verify flow across two machines (node-identity.md §3):
// export stamps a .sig sidecar; import verifies against the importer's trust store
// (unknown signer → warn-proceed in personal profile; require-signed → hard reject;
// registered signer → verified).
//
// trust_e2e_test.go —— 双机签名→验签流程（node-identity.md §3）：导出产 .sig
// sidecar；导入对照导入方 trust store 判定（未知签名者 → 个人档告警放行；
// require-signed → 硬拒；已登记签名者 → verified）。

// exportOnMachine seeds a task and exports a bundle as the given machine/home.
//
// exportOnMachine 以指定机器/home 落一条任务并导出 bundle。
func exportOnMachine(t *testing.T, machine, home, key string) string {
	t.Helper()
	var bundlePath string
	withMachine(t, machine, home, func() {
		done := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
		seedTaskState(t, machine, `feat/trust`, func(s *taskpipeline.TaskState) {
			s.CompletedAt = &done
			s.History = []taskpipeline.TaskGateResult{{Gate: `task-implement`, Passed: true}}
		})
		bundlePath = filepath.Join(t.TempDir(), `signed.tar.gz`)
		if out, err := runExport(t, map[string]string{`out`: bundlePath}); err != nil {
			t.Fatalf(`export: %v`+"\n"+`%s`, err, out)
		}
	})
	return bundlePath
}

// twoMachineFixture stands up two sync machines sharing one project id, plus
// isolated homes and the id-derived key — the A/B preamble of every
// sign→verify E2E.
//
// twoMachineFixture 建两台共享同一 project id 的同步机、隔离 home 与 id 派生
// key——所有签名→验签 E2E 的 A/B 前置。
func twoMachineFixture(t *testing.T, id string) (machineA, machineB, homeA, homeB, key string) {
	t.Helper()
	machineA, machineB = newSyncMachine(t), newSyncMachine(t)
	homeA, homeB = t.TempDir(), t.TempDir()
	for _, m := range []string{machineA, machineB} {
		if err := os.WriteFile(filepath.Join(m, forgedata.ProjectIDFileName), []byte(id+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return machineA, machineB, homeA, homeB, forgedata.IDKey(id)
}

// registerSignerOnB loads A's node identity under homeA and registers it into
// B's trust store as a team signer — so the import verdict is digest/signature
// driven, not unknown-signer (which personal profile would only warn about).
//
// registerSignerOnB 在 homeA 下加载 A 的节点身份并登记进 B 的 trust store 为
// team signer——使导入判定由摘要/签名驱动，而非未知签名者（个人档对它只警告）。
func registerSignerOnB(t *testing.T, homeA, machineB, homeB string) {
	t.Helper()
	t.Setenv("FORGE_DATA_HOME", homeA)
	idA, err := nodeid.LoadOrCreate()
	if err != nil {
		t.Fatal(err)
	}
	withMachine(t, machineB, homeB, func() {
		ts, terr := nodeid.LoadTrustStore()
		if terr != nil {
			t.Fatal(terr)
		}
		if err := ts.Add(idA.NodeID, idA.PublicKey, ``, `team`); err != nil {
			t.Fatal(err)
		}
		if err := nodeid.SaveTrustStore(ts); err != nil {
			t.Fatal(err)
		}
	})
}

// flipBundleByte writes a byte-flipped copy of bundle (breaking gzip) into a
// fresh temp dir, CARRYING the original .sig sidecar — the digest then no
// longer matches the signature, isolating the signature layer as the rejecter.
//
// flipBundleByte 把 bundle 的翻字节副本（破坏 gzip）写进全新 temp dir，并携带
// 原 .sig sidecar——摘要与签名失配，从而把拒收隔离到签名层。
func flipBundleByte(t *testing.T, bundle string) string {
	t.Helper()
	raw, err := os.ReadFile(bundle)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)/2] ^= 0xff
	flipped := filepath.Join(t.TempDir(), `flipped.tar.gz`)
	if err := os.WriteFile(flipped, raw, 0644); err != nil {
		t.Fatal(err)
	}
	sigRaw, err := os.ReadFile(bundle + `.sig`)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(flipped+`.sig`, sigRaw, 0644); err != nil {
		t.Fatal(err)
	}
	return flipped
}

func TestTrust_SignVerifyFlow(t *testing.T) {
	resetProjectCmdFlags(t)
	id := `fpid_aaaabbbbccccddddeeeeffff00001111`
	machineA, machineB, homeA, homeB, key := twoMachineFixture(t, id)

	// A exports → .sig sidecar exists and carries A's node identity.
	bundle := exportOnMachine(t, machineA, homeA, key)
	sig, err := readBundleSig(bundle)
	if err != nil || sig == nil {
		t.Fatalf("export must produce a parseable .sig sidecar: %v", err)
	}
	t.Setenv("FORGE_DATA_HOME", homeA)
	idA, err := nodeid.LoadOrCreate()
	if err != nil {
		t.Fatalf("node identity A: %v", err)
	}
	if sig.NodeID != idA.NodeID || sig.PublicKey != idA.PublicKey {
		t.Fatalf("sig identity %+v != A %+v", sig, idA)
	}

	// B imports without registering A: personal profile → warn but proceed.
	withMachine(t, machineB, homeB, func() {
		out, err := runImport(t, map[string]string{}, bundle)
		if err != nil {
			t.Fatalf(`import: %v`+"\n"+`%s`, err, out)
		}
		if !strings.Contains(out, `不在 trust store`) {
			t.Errorf("unknown signer should warn: %s", out)
		}
	})

	// B flips require-signed with A still unregistered → hard reject (fresh export
	// so the ledger does not short-circuit before the trust check).
	bundle2 := exportOnMachine(t, machineA, homeA, key)
	withMachine(t, machineB, homeB, func() {
		ts, terr := nodeid.LoadTrustStore()
		if terr != nil {
			t.Fatal(terr)
		}
		ts.RequireSigned = true
		if err := nodeid.SaveTrustStore(ts); err != nil {
			t.Fatal(err)
		}
		if _, err := runImport(t, map[string]string{}, bundle2); err == nil {
			t.Fatal("require-signed must reject an unregistered signer")
		}

		// Register A (TOFU) → the same bundle verifies and imports.
		ts2, _ := nodeid.LoadTrustStore()
		if err := ts2.Add(idA.NodeID, idA.PublicKey, `A 的机器`, `team`); err != nil {
			t.Fatalf("trust add: %v", err)
		}
		if err := nodeid.SaveTrustStore(ts2); err != nil {
			t.Fatal(err)
		}
		out, err := runImport(t, map[string]string{`force`: `true`}, bundle2)
		if err != nil {
			t.Fatalf(`import after trust add: %v`+"\n"+`%s`, err, out)
		}
		if !strings.Contains(out, `签名验证通过`) {
			t.Errorf("registered signer should verify: %s", out)
		}
	})
}

// TestTrust_TamperedBundleRejected: two rejection layers, both pinned:
//  1. naive byte-flip (sidecar carried): the digest no longer matches the
//     signature → SigInvalid rejects BEFORE unpacking (the signature layer fires
//     first since the verify-before-unpack reorder; without a sidecar the flip is
//     still caught by Unpack's per-file sha256 — the integrity layer);
//  2. a REPACKED bundle (valid manifest, passes Unpack) carrying ANOTHER bundle's
//     sidecar → same digest mismatch → SigInvalid hard-rejects. The earlier
//     revision only tested layer 1 while claiming layer 2 — a classic fake test
//     (green for the wrong reason).
//
// TestTrust_TamperedBundleRejected：钉死两条拒收防线：
//  1. 朴素翻字节（携带 sidecar）：摘要与签名不再匹配 → SigInvalid 在解包前拒
//     （验签前置重排后签名层先触发；无 sidecar 时翻字节仍由 Unpack 逐文件
//     sha256 兜住——完整性层）；
//  2. 重打包 bundle（manifest 合法、过 Unpack）挂别的 bundle 的 sidecar → 同样
//     的摘要失配 → SigInvalid 硬拒。早期版本只测了防线 1 却声称在测防线 2——
//     经典假测试（绿得名不副实）。
func TestTrust_TamperedBundleRejected(t *testing.T) {
	resetProjectCmdFlags(t)
	id := `fpid_11112222333344445555666677778888`
	machineA, machineB, homeA, homeB, key := twoMachineFixture(t, id)
	bundle := exportOnMachine(t, machineA, homeA, key)

	// Register A on B so ONLY the signature mismatch can reject (unknown-signer and
	// require-signed paths are taken off the table).
	//
	// 在 B 上登记 A，使唯一可能的拒收原因是签名不匹配（未知签名者与
	// require-signed 路径被排除）。
	registerSignerOnB(t, homeA, machineB, homeB)

	// Layer 1: naive byte-flip → Unpack rejects. The sidecar is carried so the
	// sig layer alone isn't the (first) rejecter.
	//
	// 防线 1：朴素翻字节 → Unpack 拒收。携带 sidecar，使 sig 层不是（第一个）拒收方。
	flipped := flipBundleByte(t, bundle)
	withMachine(t, machineB, homeB, func() {
		if _, err := runImport(t, map[string]string{}, flipped); err == nil {
			t.Fatal("byte-flipped bundle must be rejected (integrity layer)")
		}
	})

	// Layer 2: a DIFFERENT valid bundle (repacked — passes Unpack) wearing the first
	// bundle's sidecar → SigInvalid.
	bundle2 := exportOnMachine(t, machineA, homeA, key) // same content, new bundle_id+timestamp → different bytes
	sig1, err := readBundleSig(bundle)
	if err != nil || sig1 == nil {
		t.Fatal("missing first sidecar")
	}
	raw1, _ := json.Marshal(sig1)
	if err := os.WriteFile(bundle2+`.sig`, raw1, 0644); err != nil {
		t.Fatal(err)
	}
	withMachine(t, machineB, homeB, func() {
		_, err := runImport(t, map[string]string{}, bundle2)
		if err == nil {
			t.Fatal("repacked bundle with a stale sidecar must be rejected (signature layer)")
		}
		if !strings.Contains(err.Error(), `签名验证失败`) {
			t.Fatalf("rejection should come from the SIGNATURE layer, got: %v", err)
		}
	})
}
