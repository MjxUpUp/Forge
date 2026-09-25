package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/registry"
)

// TestRegistryRemoveCmd_RemovesThenReportsMissing 钉住 forge registry remove 命令契约：
// 命中条目移除并报告；再删同路径报错（调用方需要区分"删了"与"本来就没有"——
// 静默成功会让人误以为清理生效）。与 prune 的分工：prune 只能清死路径，
// 活着的伪根（如误注册的用户 home，2026-09 事故）只能具名移除。
func TestRegistryRemoveCmd_RemovesThenReportsMissing(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	live := mkLiveForgeDir(t)
	if err := registry.Add(live); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	registryRemoveCmd.SetOut(&buf)
	if err := registryRemoveCmd.RunE(registryRemoveCmd, []string{live}); err != nil {
		t.Fatalf(`RunE: %v`, err)
	}
	if !strings.Contains(buf.String(), `已移除`) {
		t.Errorf(`remove 应报告已移除, got: %s`, buf.String())
	}
	if got := registry.List(); len(got) != 0 {
		t.Errorf(`移除后 List = %v, want 空`, got)
	}

	// 再删同路径：无命中必须报错，不得静默成功。
	if err := registryRemoveCmd.RunE(registryRemoveCmd, []string{live}); err == nil {
		t.Fatal(`移除不存在的条目应报错（不得静默成功）`)
	}
}
