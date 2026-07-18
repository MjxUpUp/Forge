package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/registry"
)

// mkLiveForgeDir 建一个含 .forge/ 的临时目录——registry 仅登记含 .forge 的为活跃项目
// （List/Prune 过滤 .forge/ 不存在的死路径）。cli 包内 helper，与 registry 包的
// mkForgeProject 同义（跨包不能共享）。
func mkLiveForgeDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if err := os.MkdirAll(filepath.Join(d, `.forge`), 0755); err != nil {
		t.Fatal(err)
	}
	return d
}

// TestRegistryPruneCmd_PrunesDead runRegistryPrune 跑真实精简，输出报告移除条数 + 保留数。
// 隔离 FORGE_DATA_HOME 到 temp，注册 1 活跃 + 1 死路径，命令应移除 1 保留 1。
func TestRegistryPruneCmd_PrunesDead(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	live := mkLiveForgeDir(t)
	dead := t.TempDir() // 无 .forge，死路径
	if err := registry.Add(live); err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(dead); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	registryPruneCmd.SetOut(&buf)
	if err := registryPruneCmd.RunE(registryPruneCmd, nil); err != nil {
		t.Fatalf(`RunE: %v`, err)
	}
	got := buf.String()
	if !strings.Contains(got, `移除 1`) {
		t.Errorf(`prune 应报告移除 1 条死路径, got: %s`, got)
	}
	if !strings.Contains(got, `保留 1`) {
		t.Errorf(`prune 应报告保留 1 个活跃项目, got: %s`, got)
	}
}

// TestRegistryPruneCmd_AlreadyClean 无死路径时输出"已是最精简"，pruned=0。
func TestRegistryPruneCmd_AlreadyClean(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	live := mkLiveForgeDir(t)
	if err := registry.Add(live); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	registryPruneCmd.SetOut(&buf)
	if err := registryPruneCmd.RunE(registryPruneCmd, nil); err != nil {
		t.Fatalf(`RunE: %v`, err)
	}
	got := buf.String()
	if !strings.Contains(got, `最精简`) {
		t.Errorf(`已精简时输出应含"最精简", got: %s`, got)
	}
}
