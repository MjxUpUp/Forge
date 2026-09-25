package cliskills

// skills_inventory_test.go — AST10 pinning 三态验证：一致/漂移/未知新增/缺项，
// 用临时树夹具走真命令面（buildInventory + verifyInventory）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seedSkillTree(t *testing.T, root, treeDir, name, body string) {
	t.Helper()
	dir := filepath.Join(root, treeDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInventoryVerifyStates(t *testing.T) {
	root := t.TempDir()
	seedSkillTree(t, root, "skills", "alpha", "body-a")
	seedSkillTree(t, root, "skills", "beta", "body-b")
	seedSkillTree(t, root, "plugins/forge-design/skills", "gamma", "body-g")

	items, err := buildInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("应枚举 3 个（canonical+pack）: %+v", items)
	}
	var gamma *inventoryItem
	for i := range items {
		if items[i].Name == "gamma" {
			gamma = &items[i]
		}
	}
	if gamma == nil || gamma.Tree != "pack:forge-design" {
		t.Fatalf("pack 树识别失败: %+v", items)
	}

	// 钉基线（借 renderInventory 写锁）。
	body, err := renderInventory(items)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(root, inventoryLockFile)
	if err := os.WriteFile(lockPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// 一致。
	if err := verifyInventory(root, items); err != nil {
		t.Fatalf("一致应通过: %v", err)
	}
	// 漂移：改 alpha 内容。
	seedSkillTree(t, root, "skills", "alpha", "body-a-tampered")
	items2, _ := buildInventory(root)
	err = verifyInventory(root, items2)
	if err == nil || !strings.Contains(err.Error(), "漂移 1") {
		t.Fatalf("漂移应拦截: %v", err)
	}
	// 未知新增 + 缺项：删 beta、加 delta。
	if err := os.RemoveAll(filepath.Join(root, "skills", "beta")); err != nil {
		t.Fatal(err)
	}
	seedSkillTree(t, root, "skills", "delta", "body-d")
	items3, _ := buildInventory(root)
	err = verifyInventory(root, items3)
	if err == nil || !strings.Contains(err.Error(), "未知 1") || !strings.Contains(err.Error(), "缺项 1") {
		t.Fatalf("未知/缺项应拦截: %v", err)
	}
	// 无锁文件 → BLOCKED 提示钉基线。
	if err := verifyInventory(t.TempDir(), nil); err == nil || !strings.Contains(err.Error(), "钉基线") {
		t.Fatalf("无锁应提示: %v", err)
	}
}
