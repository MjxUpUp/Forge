package nodeid

import (
	"path/filepath"
	"testing"
	"time"
)

// trust_test.go —— trust store（node-identity.md §3）：对端增删查、0600 持久化、
// require-signed 开关、bundle 签名判定矩阵。

func withTrustHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", dir)
	return dir
}

func TestTrustStore_AddLoadRemove(t *testing.T) {
	home := withTrustHome(t)
	id, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	ts := NewTrustStore()
	if err := ts.Add(id.NodeID, id.PublicKey, `工作机`, `team`); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := SaveTrustStore(ts); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// perms 0600 — the trust root deserves the same hygiene as the identity file.
	assertPrivatePerms(t, filepath.Join(home, `trust.json`), "trust.json")

	loaded, err := LoadTrustStore()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p, ok := loaded.Peer(id.NodeID)
	if !ok || p.PublicKey != id.PublicKey || p.Profile != `team` || p.Label != `工作机` {
		t.Fatalf("peer roundtrip = %+v ok=%v", p, ok)
	}
	if err := loaded.Remove(id.NodeID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, ok := loaded.Peer(id.NodeID); ok {
		t.Fatal("peer still present after Remove")
	}
}

func TestTrustStore_RejectsMalformedNodeID(t *testing.T) {
	ts := NewTrustStore()
	if err := ts.Add(`not-a-node`, `AAAA`, ``, `personal`); err == nil {
		t.Fatal("Add accepted malformed node id")
	}
}

func TestTrustStore_RequireSignedToggle(t *testing.T) {
	withTrustHome(t)
	ts := NewTrustStore()
	if ts.RequireSigned {
		t.Fatal("default RequireSigned must be false (personal profile)")
	}
	ts.RequireSigned = true
	if err := SaveTrustStore(ts); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := LoadTrustStore()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded.RequireSigned {
		t.Fatal("RequireSigned did not persist")
	}
}

func TestVerifyBundleSig_VerdictMatrix(t *testing.T) {
	withTrustHome(t)
	signer, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	digest := `9cb3364774469f5023807b47392e0aa2a6f164194606d6e92740eb50ec78839d`
	sig, err := signer.Sign([]byte(digest))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	ts := NewTrustStore()

	// unknown signer, personal default → warn-proceed
	v := ts.VerifyBundleSig(digest, &BundleSig{NodeID: signer.NodeID, PublicKey: signer.PublicKey, Sig: sig})
	if v != SigUnknownSigner {
		t.Fatalf("unknown signer verdict = %v, want SigUnknownSigner", v)
	}
	// team mode: unknown signer → reject
	ts.RequireSigned = true
	if v := ts.VerifyBundleSig(digest, &BundleSig{NodeID: signer.NodeID, PublicKey: signer.PublicKey, Sig: sig}); v != SigRejected {
		t.Fatalf("team-mode unknown signer = %v, want SigRejected", v)
	}
	ts.RequireSigned = false

	// known peer + valid sig → verified
	if err := ts.Add(signer.NodeID, signer.PublicKey, ``, `team`); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if v := ts.VerifyBundleSig(digest, &BundleSig{NodeID: signer.NodeID, PublicKey: signer.PublicKey, Sig: sig}); v != SigVerified {
		t.Fatalf("known peer valid sig = %v, want SigVerified", v)
	}
	// valid sig but WRONG digest → invalid (always, any profile)
	if v := ts.VerifyBundleSig(`0000000000000000000000000000000000000000000000000000000000000000`,
		&BundleSig{NodeID: signer.NodeID, PublicKey: signer.PublicKey, Sig: sig}); v != SigInvalid {
		t.Fatalf("wrong digest = %v, want SigInvalid", v)
	}
	// pubkey in the sig block disagrees with the trust store → invalid (store wins)
	other, _ := Generate()
	if v := ts.VerifyBundleSig(digest, &BundleSig{NodeID: signer.NodeID, PublicKey: other.PublicKey, Sig: sig}); v != SigInvalid {
		t.Fatalf("store/pubkey mismatch = %v, want SigInvalid", v)
	}
	// missing sig entirely: personal → proceed; team → reject
	if v := ts.VerifyBundleSig(digest, nil); v != SigMissing {
		t.Fatalf("missing sig personal = %v, want SigMissing", v)
	}
	ts.RequireSigned = true
	if v := ts.VerifyBundleSig(digest, nil); v != SigRejected {
		t.Fatalf("missing sig team = %v, want SigRejected", v)
	}
}

func TestTrustStore_CreatedAtStamped(t *testing.T) {
	ts := NewTrustStore()
	id, _ := Generate()
	before := time.Now().Add(-time.Second)
	if err := ts.Add(id.NodeID, id.PublicKey, ``, `personal`); err != nil {
		t.Fatalf("Add: %v", err)
	}
	p, _ := ts.Peer(id.NodeID)
	if p.AddedAt.Before(before) {
		t.Fatalf("AddedAt %v not stamped at add time", p.AddedAt)
	}
}
