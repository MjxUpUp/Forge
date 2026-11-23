package change

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/agentproto"
)

func TestVerifySummaryCarriesCapability(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	writeFile(t, filepath.Join(dir, FileAcceptance), "# 验收\n\n\x60\x60\x60bash\necho ok\n\x60\x60\x60\n")
	writeFile(t, filepath.Join(root, "README.md"), "forge change verify demo\nforge hook install\n")

	surface := []agentproto.Command{{Path: "change"}, {Path: "change verify"}}
	res, err := Verify(context.Background(), root, "demo", VerifyOptions{Surface: surface})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.Summary.Capability == nil {
		t.Fatalf("摘要应带能力面闭合表，got %+v", res.Summary)
	}
	c := res.Summary.Capability
	if c.Commands != 2 {
		t.Errorf("命令数不对：%+v", c)
	}
	// fixture 里 Init 会写 AGENTS.md（真实协议块），所以漂移不止 README 一处；
	// 这里只钉「README 里那条假命令被抓到」，不把夹具的其余引用算进来。
	found := false
	for _, d := range c.Drifts {
		if strings.HasPrefix(d, "README.md:") && strings.HasSuffix(d, " hook") {
			found = true
		}
	}
	if !found {
		t.Errorf("应报 README 的 hook 漂移，got %+v", c.Drifts)
	}
	if c.DriftCount < 1 {
		t.Errorf("漂移计数不对：%+v", c)
	}
}
