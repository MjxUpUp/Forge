package checklog

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestMetaKeyLiterals pins the on-disk spelling of the exported Meta keys.
//
// These keys are persisted verbatim in checklog.jsonl: renaming one silently
// orphans every historical row that carries it (there is no migration), so the
// literals are part of the file contract, not an implementation detail.
func TestMetaKeyLiterals(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"MetaKeyResolvePath", MetaKeyResolvePath, "resolve_path"},
		{"MetaKeyPostSeal", MetaKeyPostSeal, "post_seal"},
		{"MetaKeySuggested", MetaKeySuggested, "suggested"},
		{"MetaKeyVerdict", MetaKeyVerdict, "verdict"},
		{"MetaKeySigner", MetaKeySigner, "signer"},
		{"MetaKeySyncOp", MetaKeySyncOp, "sync_op"},
		{"MetaKeyAttributionAttributed", MetaKeyAttributionAttributed, "attribution.attributed"},
		{"MetaKeyAttributionOrphans", MetaKeyAttributionOrphans, "attribution.orphans"},
		{"MetaKeyAttributionRate", MetaKeyAttributionRate, "attribution.rate"},
	}

	seen := make(map[string]string, len(cases))
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q (on-disk key is a contract; changing it orphans historical rows)", c.name, c.got, c.want)
		}
		if prev, dup := seen[c.got]; dup {
			t.Errorf("%s and %s share the key %q", prev, c.name, c.got)
		}
		seen[c.got] = c.name
	}
}

// TestEntryMetaKeysSerializeFlat locks how the keys reach the JSON line: Meta
// is a flat object on Entry, so readers can index a row by key without
// knowing which writer produced it.
func TestEntryMetaKeysSerializeFlat(t *testing.T) {
	entry := Entry{
		Check:     CheckNextHint,
		Passed:    true,
		Checked:   true,
		Detail:    "next-hint: forge task start",
		SessionID: "sess-1",
		Meta: map[string]string{
			MetaKeySuggested:   "forge task start --ref feat/x",
			MetaKeyResolvePath: "branch",
			MetaKeyPostSeal:    "true",
		},
	}

	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal entry: %v", err)
	}
	line := string(raw)
	for _, key := range []string{`"suggested"`, `"resolve_path"`, `"post_seal"`} {
		if !strings.Contains(line, key) {
			t.Errorf("serialized entry missing key %s: %s", key, line)
		}
	}

	var back Entry
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal entry: %v", err)
	}
	if back.Meta[MetaKeySuggested] != entry.Meta[MetaKeySuggested] {
		t.Errorf("suggested round-trip = %q, want %q", back.Meta[MetaKeySuggested], entry.Meta[MetaKeySuggested])
	}
	if back.Meta[MetaKeyPostSeal] != "true" {
		t.Errorf("post_seal round-trip = %q, want true", back.Meta[MetaKeyPostSeal])
	}
}
