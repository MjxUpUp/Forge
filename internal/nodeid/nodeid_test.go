package nodeid

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withHome points FORGE_DATA_HOME at a temp dir so the identity store is isolated.
//
// withHome 把 FORGE_DATA_HOME 指向临时目录，隔离身份 store。
func withHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", dir)
	return dir
}

func TestLoadOrCreate_GeneratesValidIdentity(t *testing.T) {
	home := withHome(t)
	id, err := LoadOrCreate()
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if !ValidNodeID(id.NodeID) {
		t.Fatalf("node_id %q does not match fnode_<32hex>", id.NodeID)
	}
	if id.PublicKey == "" || id.PrivateKey == "" {
		t.Fatalf("keys must be populated: %+v", id)
	}
	if id.RotationChain == nil {
		t.Fatal("rotation_chain must serialize as [] not null (format reserved from day one)")
	}
	if len(id.RotationChain) != 0 {
		t.Fatalf("v1 rotation chain must be empty, got %d links", len(id.RotationChain))
	}
	// private key file perms 0600 — the private key never leaves this machine.
	fi, err := os.Stat(filepath.Join(home, "node.json"))
	if err != nil {
		t.Fatalf("stat node.json: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0600 {
		t.Fatalf("node.json perms = %o, want 0600", perm)
	}
}

func TestLoadOrCreate_PersistsAcrossCalls(t *testing.T) {
	withHome(t)
	a, err := LoadOrCreate()
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	b, err := LoadOrCreate()
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if a.NodeID != b.NodeID || a.PublicKey != b.PublicKey {
		t.Fatalf("identity changed across loads: %q/%q vs %q/%q", a.NodeID, a.PublicKey, b.NodeID, b.PublicKey)
	}
}

func TestLoad_MissingReturnsErrNotCreate(t *testing.T) {
	withHome(t)
	if _, err := Load(); err == nil {
		t.Fatal("Load without existing identity must error (only LoadOrCreate generates)")
	} else if !errors.Is(err, os.ErrNotExist) {
		// LoadOrCreate's create-branch keys on this — pin the wrap chain explicitly.
		t.Fatalf("Load error must wrap os.ErrNotExist, got %v", err)
	}
}

func TestNodeID_DerivesFromPublicKey(t *testing.T) {
	withHome(t)
	id, err := LoadOrCreate()
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	pub, err := id.PublicKeyBytes()
	if err != nil {
		t.Fatalf("PublicKeyBytes: %v", err)
	}
	if got := DeriveNodeID(pub); got != id.NodeID {
		t.Fatalf("DeriveNodeID(pub) = %q, want stored %q", got, id.NodeID)
	}
}

func TestSignVerify_Roundtrip(t *testing.T) {
	withHome(t)
	id, err := LoadOrCreate()
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	msg := []byte(`{"task_ref":"feat/x","gate":"task-verify"}`)
	sig, err := id.Sign(msg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if !Verify(id.PublicKey, msg, sig) {
		t.Fatal("Verify(roundtrip) = false")
	}
	if Verify(id.PublicKey, []byte("tampered"), sig) {
		t.Fatal("Verify(tampered msg) = true")
	}
	other, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if Verify(other.PublicKey, msg, sig) {
		t.Fatal("Verify(wrong key) = true")
	}
}

func TestLoad_RejectsTamperedNodeID(t *testing.T) {
	home := withHome(t)
	id, err := LoadOrCreate()
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	// flip the stored node_id without touching keys — Load must detect the inconsistency.
	raw, err := os.ReadFile(filepath.Join(home, "node.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	replacement := "fnode_" + strings.Repeat("0", 32)
	if id.NodeID == replacement {
		replacement = "fnode_" + strings.Repeat("1", 32)
	}
	raw = []byte(strings.Replace(string(raw), id.NodeID, replacement, 1))
	if err := os.WriteFile(filepath.Join(home, "node.json"), raw, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted node_id inconsistent with public key")
	}
}

func TestValidNodeID(t *testing.T) {
	good := "fnode_" + strings.Repeat("a1", 16)
	if !ValidNodeID(good) {
		t.Fatalf("ValidNodeID(%q) = false", good)
	}
	for _, bad := range []string{"", "fnode_", "fnode_" + strings.Repeat("a", 31), "fnode_" + strings.Repeat("A", 32), "fpid_" + strings.Repeat("a", 32), "x" + good} {
		if ValidNodeID(bad) {
			t.Fatalf("ValidNodeID(%q) = true", bad)
		}
	}
}

// rewriteIdentity tampers node.json via fn and returns the mutated raw bytes.
//
// rewriteIdentity 经 fn 篡改 node.json 并返回改后字节。
func rewriteIdentity(t *testing.T, home string, fn func(map[string]any)) {
	t.Helper()
	p := filepath.Join(home, "node.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	fn(m)
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(p, out, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestLoad_RejectsTamperedPublicKey(t *testing.T) {
	home := withHome(t)
	if _, err := LoadOrCreate(); err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	other, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	rewriteIdentity(t, home, func(m map[string]any) { m["public_key"] = other.PublicKey })
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted public key inconsistent with node_id")
	}
}

func TestLoad_RejectsMismatchedPrivateKey(t *testing.T) {
	home := withHome(t)
	if _, err := LoadOrCreate(); err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	other, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	rewriteIdentity(t, home, func(m map[string]any) { m["private_key"] = other.PrivateKey })
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted private key not matching public key")
	}
}

func TestLoad_RejectsCorruptJSON(t *testing.T) {
	home := withHome(t)
	if _, err := LoadOrCreate(); err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "node.json"), []byte(`{not json`), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted corrupt JSON")
	}
}

func TestLoad_RejectsNullRotationChain(t *testing.T) {
	home := withHome(t)
	if _, err := LoadOrCreate(); err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	// v1 contract: rotation_chain serializes as [], never null — a foreign/buggy writer
	// persisting null must fail loud at Load, not silently leak null into show output.
	rewriteIdentity(t, home, func(m map[string]any) { m["rotation_chain"] = nil })
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted null rotation_chain (v1 contract: always [])")
	}
}

func TestLoad_TightensLoosePerms(t *testing.T) {
	home := withHome(t)
	if _, err := LoadOrCreate(); err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	p := filepath.Join(home, "node.json")
	if err := os.Chmod(p, 0644); err != nil {
		t.Fatalf("chmod 0644: %v", err)
	}
	if _, err := Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0600 {
		t.Fatalf("Load left perms %o, want tightened 0600", perm)
	}
}
