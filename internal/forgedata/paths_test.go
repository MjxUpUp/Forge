package forgedata

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestSurvivingAccessors pins the path accessors that survived the zombie cleanup: they must join under DataDir (never escape it) and stay stable.
//
// TestSurvivingAccessors 钉住僵尸清剿后存活的 path accessor：必须拼接在
// DataDir 之下（永不逃逸）且保持稳定。
func TestSurvivingAccessors(t *testing.T) {
	p := &Project{Key: "abc123", GitRoot: "/repo", DataDir: filepath.Join("/home", ".forge", "projects", "abc123")}

	for name, got := range map[string]string{
		"MetaPath":           p.MetaPath(),
		"HazardsEventsPath":  p.HazardsEventsPath(),
		"HazardsConfirmPath": p.HazardsConfirmPath("deadbeef"),
		"ActConclusionsPath": p.ActConclusionsPath(),
		"FreezeDir":          p.FreezeDir(),
		"FreezeStatePath":    p.FreezeStatePath(),
	} {
		if !strings.HasPrefix(got, p.DataDir+string(filepath.Separator)) {
			t.Errorf("%s escapes DataDir: %q", name, got)
		}
	}
	if got := p.HazardsConfirmPath("deadbeef"); filepath.Base(got) != "deadbeef.json" {
		t.Errorf("HazardsConfirmPath filename = %q, want deadbeef.json", filepath.Base(got))
	}
	if got := p.FreezeStatePath(); got != filepath.Join(p.FreezeDir(), "state.json") {
		t.Errorf("FreezeStatePath = %q, want <FreezeDir>/state.json", got)
	}
}
