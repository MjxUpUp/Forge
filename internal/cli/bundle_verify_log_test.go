package cli

// bundle_verify_log_test.go —— 每个导入侧信任判定都以正确的 Level/Passed 与结构化
// Meta（verdict + signer）落 checklog（bundle-verify）：信任决策此前只到达导入
// 终端，看板对多机信任活动全盲。钉死全部五种 verdict——含硬拒（invalid/rejected）
// 既返回错误又照样落章（拒收本身就是最值得看见的事件）。

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/nodeid"
)

// bundleVerifyFixture 搭隔离 home（身份 + trust store）+ 非 git 项目 root（其
// checklog 落在同一 FORGE_DATA_HOME 下）。
func bundleVerifyFixture(t *testing.T) (root, home, bundle, digestHex string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("FORGE_DATA_HOME", home)
	root = t.TempDir()
	bundle = filepath.Join(t.TempDir(), `bundle.tar.gz`)
	if err := os.WriteFile(bundle, []byte(`payload-bytes`), 0644); err != nil {
		t.Fatal(err)
	}
	digestHex = fmt.Sprintf(`%x`, sha256.Sum256([]byte(`payload-bytes`)))
	return root, home, bundle, digestHex
}

// writeTrustStore peers 直接写 trust.json（绕过 CLI 层，夹具要的是 store 本体）。
func writeTrustStore(t *testing.T, home string, peers map[string]nodeid.TrustedPeer, requireSigned bool) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"peers":          peers,
		"require_signed": requireSigned,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, `trust.json`), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

// lastBundleVerifyEntry 读回 root 的 checklog 里最后一条 bundle-verify 条目。
func lastBundleVerifyEntry(t *testing.T, root string) checklog.Entry {
	t.Helper()
	entries, err := checklog.LoadAllAll(root)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Check == checklog.CheckBundleVerify {
			return entries[i]
		}
	}
	t.Fatal("无 bundle-verify 条目落盘")
	return checklog.Entry{}
}

func TestVerifyBundleForImport_RecordsVerdicts(t *testing.T) {
	t.Run("missing→advisory", func(t *testing.T) {
		root, _, bundle, digest := bundleVerifyFixture(t) // 无 .sig、无 trust.json = 个人档
		if err := verifyBundleForImport(root, bundle, digest, io.Discard, false); err != nil {
			t.Fatal(err)
		}
		e := lastBundleVerifyEntry(t, root)
		if !e.Passed || e.Level != checklog.LevelAdvisory || e.Meta[checklog.MetaKeyVerdict] != `missing` {
			t.Errorf("missing verdict 落章异常: %+v", e)
		}
		if _, ok := e.Meta[checklog.MetaKeySigner]; ok {
			t.Errorf("无 sidecar 不应带 signer 键: %+v", e.Meta)
		}
	})

	t.Run("unknown-signer→warn", func(t *testing.T) {
		root, _, bundle, digest := bundleVerifyFixture(t)
		if _, err := writeBundleSig(bundle); err != nil { // 本机身份签名，但 trust store 为空
			t.Fatal(err)
		}
		if err := verifyBundleForImport(root, bundle, digest, io.Discard, false); err != nil {
			t.Fatal(err)
		}
		e := lastBundleVerifyEntry(t, root)
		if !e.Passed || e.Level != checklog.LevelWarn || e.Meta[checklog.MetaKeyVerdict] != `unknown-signer` {
			t.Errorf("unknown-signer verdict 落章异常: %+v", e)
		}
		if e.Meta[checklog.MetaKeySigner] == `` {
			t.Error("unknown-signer 必须带 signer 键（面板归因靠它）")
		}
	})

	t.Run("verified→pass", func(t *testing.T) {
		root, home, bundle, digest := bundleVerifyFixture(t)
		if _, err := writeBundleSig(bundle); err != nil {
			t.Fatal(err)
		}
		id, err := nodeid.LoadOrCreate()
		if err != nil {
			t.Fatal(err)
		}
		writeTrustStore(t, home, map[string]nodeid.TrustedPeer{
			id.NodeID: {PublicKey: id.PublicKey, Profile: `personal`, AddedAt: time.Now()},
		}, false)
		if err := verifyBundleForImport(root, bundle, digest, io.Discard, false); err != nil {
			t.Fatal(err)
		}
		e := lastBundleVerifyEntry(t, root)
		if !e.Passed || e.Level != checklog.LevelPass || e.Meta[checklog.MetaKeyVerdict] != `verified` || e.Meta[checklog.MetaKeySigner] != id.NodeID {
			t.Errorf("verified verdict 落章异常: %+v", e)
		}
	})

	t.Run("invalid→blocked+error", func(t *testing.T) {
		root, home, bundle, _ := bundleVerifyFixture(t)
		if _, err := writeBundleSig(bundle); err != nil {
			t.Fatal(err)
		}
		id, err := nodeid.LoadOrCreate()
		if err != nil {
			t.Fatal(err)
		}
		writeTrustStore(t, home, map[string]nodeid.TrustedPeer{
			id.NodeID: {PublicKey: id.PublicKey, Profile: `personal`, AddedAt: time.Now()},
		}, false)
		// 篡改摘要：验签内容与被签内容不一致 → invalid，任何档位都硬拒。
		tampered := fmt.Sprintf(`%x`, sha256.Sum256([]byte(`tampered`)))
		if err := verifyBundleForImport(root, bundle, tampered, io.Discard, false); err == nil {
			t.Fatal("invalid 必须返回错误（拒绝导入）")
		}
		e := lastBundleVerifyEntry(t, root)
		if e.Passed || e.Level != checklog.LevelBlocked || e.Meta[checklog.MetaKeyVerdict] != `invalid` {
			t.Errorf("invalid verdict 落章异常: %+v（硬拒也要落章——拒收本身就是事件）", e)
		}
	})

	t.Run("rejected→blocked+error", func(t *testing.T) {
		root, home, bundle, digest := bundleVerifyFixture(t)
		writeTrustStore(t, home, map[string]nodeid.TrustedPeer{}, true) // 团队档 + 无 sidecar
		if err := verifyBundleForImport(root, bundle, digest, io.Discard, false); err == nil {
			t.Fatal("团队档未签名必须返回错误")
		}
		e := lastBundleVerifyEntry(t, root)
		if e.Passed || e.Level != checklog.LevelBlocked || e.Meta[checklog.MetaKeyVerdict] != `rejected` {
			t.Errorf("rejected verdict 落章异常: %+v", e)
		}
	})
}

// TestVerifyBundleForImport_DryRunNoRecord pins the --dry-run contract ("校验并列出
// 将执行的动作，不落盘"——project_import.go flag 文档)：验签判定照跑（校验是
// dry-run 的本职），但 checklog 落章是写侧效应，必须跳过。
//
// TestVerifyBundleForImport_DryRunNoRecord 钉死 --dry-run 契约（「校验并列出将执行
// 的动作，不落盘」——project_import.go flag 文档）：验签判定照跑（校验是 dry-run
// 的本职），但 checklog 落章是写侧效应，必须跳过。
func TestVerifyBundleForImport_DryRunNoRecord(t *testing.T) {
	root, _, bundle, digest := bundleVerifyFixture(t)
	if err := verifyBundleForImport(root, bundle, digest, io.Discard, true); err != nil {
		t.Fatal(err)
	}
	entries, err := checklog.LoadAllAll(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Check == checklog.CheckBundleVerify {
			t.Fatalf("dry-run 不得落 bundle-verify 章: %+v", e)
		}
	}
}
