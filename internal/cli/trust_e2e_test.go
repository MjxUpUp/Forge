package cli

import (
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
			t.Fatalf(`export: %v\n%s`, err, out)
		}
	})
	return bundlePath
}

func TestTrust_SignVerifyFlow(t *testing.T) {
	resetProjectCmdFlags(t)
	id := `fpid_aaaabbbbccccddddeeeeffff00001111`
	key := forgedata.IDKey(id)
	machineA, machineB := newSyncMachine(t), newSyncMachine(t)
	homeA, homeB := t.TempDir(), t.TempDir()
	for _, m := range []string{machineA, machineB} {
		if err := os.WriteFile(filepath.Join(m, forgedata.ProjectIDFileName), []byte(id+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

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
			t.Fatalf(`import: %v\n%s`, err, out)
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
			t.Fatalf(`import after trust add: %v\n%s`, err, out)
		}
		if !strings.Contains(out, `签名验证通过`) {
			t.Errorf("registered signer should verify: %s", out)
		}
	})
}

// TestTrust_TamperedBundleRejected: a bundle whose bytes were modified after signing
// must hard-fail verification (the digest no longer matches the signature) — in ANY
// profile.
//
// TestTrust_TamperedBundleRejected：签名后字节被改的 bundle 必须验签硬失败（摘要
// 与签名不再匹配）——任何 profile 下。
func TestTrust_TamperedBundleRejected(t *testing.T) {
	resetProjectCmdFlags(t)
	id := `fpid_11112222333344445555666677778888`
	key := forgedata.IDKey(id)
	machineA, machineB := newSyncMachine(t), newSyncMachine(t)
	homeA, homeB := t.TempDir(), t.TempDir()
	for _, m := range []string{machineA, machineB} {
		if err := os.WriteFile(filepath.Join(m, forgedata.ProjectIDFileName), []byte(id+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	bundle := exportOnMachine(t, machineA, homeA, key)

	// Flip bytes in the bundle AFTER the sidecar was written.
	raw, err := os.ReadFile(bundle)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)/2] ^= 0xff
	if err := os.WriteFile(bundle, raw, 0644); err != nil {
		t.Fatal(err)
	}

	withMachine(t, machineB, homeB, func() {
		if _, err := runImport(t, map[string]string{}, bundle); err == nil {
			t.Fatal("tampered bundle must be rejected")
		}
	})
}
