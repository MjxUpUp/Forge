package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/nodeid"
	"github.com/spf13/cobra"
)

// trust_cli_test.go — the `forge trust` command surface: add/list/remove/
// require-signed via RunE, incl. the node_id↔pubkey consistency rejection.
//
// trust_cli_test.go —— `forge trust` 命令面：add/list/remove/require-signed 经
// RunE，含 node_id↔公钥一致性拒绝。

func runTrustCmd(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	t.Cleanup(func() { cmd.SetOut(nil) })
	err := cmd.RunE(cmd, args)
	return buf.String(), err
}

func TestTrustCmd_SurfaceFlow(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	id, err := nodeid.Generate()
	if err != nil {
		t.Fatal(err)
	}

	// add with mismatched pubkey → rejected (consistency check).
	other, _ := nodeid.Generate()
	if _, err := runTrustCmd(t, trustAddCmd, id.NodeID, other.PublicKey); err == nil {
		t.Fatal("mismatched node_id/pubkey must be rejected at the CLI surface")
	}

	// add → list shows it → require-signed on → remove.
	if err := trustAddCmd.Flags().Set(`label`, `工作机`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = trustAddCmd.Flags().Set(`label`, ``) })
	if _, err := runTrustCmd(t, trustAddCmd, id.NodeID, id.PublicKey); err != nil {
		t.Fatalf("trust add: %v", err)
	}
	out, err := runTrustCmd(t, trustListCmd)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, id.NodeID) || !strings.Contains(out, `工作机`) {
		t.Fatalf("list missing peer: %s", out)
	}
	if _, err := runTrustCmd(t, trustRequireSignedCmd, `on`); err != nil {
		t.Fatal(err)
	}
	ts, err := nodeid.LoadTrustStore()
	if err != nil || !ts.RequireSigned {
		t.Fatalf("require-signed not persisted: %+v %v", ts, err)
	}
	if _, err := runTrustCmd(t, trustRequireSignedCmd, `maybe`); err == nil {
		t.Fatal("require-signed must reject non-on/off")
	}
	if _, err := runTrustCmd(t, trustRemoveCmd, id.NodeID); err != nil {
		t.Fatal(err)
	}
	if out, _ := runTrustCmd(t, trustListCmd); strings.Contains(out, id.NodeID) {
		t.Fatal("peer still listed after remove")
	}
}
