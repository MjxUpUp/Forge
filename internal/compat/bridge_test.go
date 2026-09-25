package compat

// bridge_test.go — 第七面（外部桥契约）的 Diff 判级与扫描测试。
// 判级规则（compat-bridge-face.md §二.4）：事件映射 removed / forge 事件或
// decision 形状 changed → Breaking；failOpen 承诺 removed/changed → Breaking；
// 一切 added → 非 Breaking；note 变更不入 diff（注释非契约）。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func bridgeFixture() *Snapshot {
	return &Snapshot{Bridges: []BridgeContract{{
		Bridge:      "forge-dsh",
		DshVerified: "0.1.0-rc.7",
		Events: []BridgeEvent{
			{Dsh: "tools/pre-execute", Forge: "PreToolUse", Decision: "deny"},
			{Dsh: "tools/post-execute", Forge: "PostToolUse", Decision: "block"},
		},
		FailOpen: []string{"block 只读 stdout JSON 的 decision 字段"},
	}}}
}

func changesByItem(changes []Change) map[string]Change {
	m := map[string]Change{}
	for _, c := range changes {
		m[c.Surface+"/"+c.Item] = c
	}
	return m
}

// findChangeByItemPrefix 按 Item 前缀找 change——changed 行的 Item 带中文判语
// 后缀（对齐 blockings 面的既有格式），精确键匹配不到。
func findChangeByItemPrefix(t *testing.T, changes []Change, surface, itemPrefix string) Change {
	t.Helper()
	for _, c := range changes {
		if c.Surface == surface && strings.HasPrefix(c.Item, itemPrefix) {
			return c
		}
	}
	t.Fatalf("%s/%s 前缀的 change 未出现在 diff: %+v", surface, itemPrefix, changes)
	return Change{}
}

func TestBridgeDiff_EventRemovedBreaking(t *testing.T) {
	cur := bridgeFixture()
	cur.Bridges[0].Events = cur.Bridges[0].Events[:1] // post-execute 映射消失
	changes := Diff(bridgeFixture(), cur)
	c, ok := changesByItem(changes)["bridges/forge-dsh/tools/post-execute"]
	if !ok {
		t.Fatalf("映射 removed 未出现在 diff: %+v", changes)
	}
	if !c.Breaking {
		t.Errorf("映射 removed 应判 Breaking: %+v", c)
	}
}

func TestBridgeDiff_DecisionChangedBreaking(t *testing.T) {
	cur := bridgeFixture()
	cur.Bridges[0].Events[1].Decision = "reject" // decision 形状改变
	changes := Diff(bridgeFixture(), cur)
	c := findChangeByItemPrefix(t, changes, "bridges", "forge-dsh/tools/post-execute")
	if !c.Breaking {
		t.Errorf("decision 形状 changed 应判 Breaking: %+v", c)
	}
}

func TestBridgeDiff_ForgeEventChangedBreaking(t *testing.T) {
	cur := bridgeFixture()
	cur.Bridges[0].Events[1].Forge = "PostToolUseFailure" // forge 事件改挂
	changes := Diff(bridgeFixture(), cur)
	c := findChangeByItemPrefix(t, changes, "bridges", "forge-dsh/tools/post-execute")
	if !c.Breaking {
		t.Errorf("forge 事件 changed 应判 Breaking: %+v", c)
	}
}

func TestBridgeDiff_EventAddedNonBreaking(t *testing.T) {
	cur := bridgeFixture()
	cur.Bridges[0].Events = append(cur.Bridges[0].Events, BridgeEvent{Dsh: "agent/pre-step", Forge: "UserPromptSubmit", Decision: "reject"})
	changes := Diff(bridgeFixture(), cur)
	c, ok := changesByItem(changes)["bridges/forge-dsh/agent/pre-step"]
	if !ok {
		t.Fatalf("映射 added 未出现在 diff: %+v", changes)
	}
	if c.Breaking {
		t.Errorf("映射 added 应判非 Breaking: %+v", c)
	}
}

func TestBridgeDiff_NoteOnlyChangeInvisible(t *testing.T) {
	cur := bridgeFixture()
	cur.Bridges[0].Events[0].Note = "注释措辞调整"
	changes := Diff(bridgeFixture(), cur)
	if len(changes) != 0 {
		t.Errorf("note 变更是注释非契约，不应入 diff: %+v", changes)
	}
}

func TestBridgeDiff_FailOpenRemovedBreaking(t *testing.T) {
	cur := bridgeFixture()
	cur.Bridges[0].FailOpen = nil
	changes := Diff(bridgeFixture(), cur)
	c, ok := changesByItem(changes)["bridges/forge-dsh/failOpen"]
	if !ok {
		t.Fatalf("failOpen removed 未出现在 diff: %+v", changes)
	}
	if !c.Breaking {
		t.Errorf("failOpen 承诺 removed 应判 Breaking: %+v", c)
	}
}

func TestBridgeDiff_FailOpenAddedNonBreaking(t *testing.T) {
	cur := bridgeFixture()
	cur.Bridges[0].FailOpen = append(cur.Bridges[0].FailOpen, "可见面仅 /forge-status")
	changes := Diff(bridgeFixture(), cur)
	c, ok := changesByItem(changes)["bridges/forge-dsh/failOpen"]
	if !ok {
		t.Fatalf("failOpen added 未出现在 diff: %+v", changes)
	}
	if c.Breaking {
		t.Errorf("failOpen added 应判非 Breaking: %+v", c)
	}
}

func TestBridgeDiff_BridgeRemovedBreaking(t *testing.T) {
	cur := bridgeFixture()
	cur.Bridges = nil
	changes := Diff(bridgeFixture(), cur)
	c, ok := changesByItem(changes)["bridges/forge-dsh"]
	if !ok {
		t.Fatalf("整桥 removed 未出现在 diff: %+v", changes)
	}
	if !c.Breaking {
		t.Errorf("整桥 removed 应判 Breaking: %+v", c)
	}
}

func TestBridgeDiff_NewBridgeNonBreaking(t *testing.T) {
	cur := bridgeFixture()
	cur.Bridges = append(cur.Bridges, BridgeContract{Bridge: "forge-future", Events: []BridgeEvent{{Dsh: "x", Forge: "PreToolUse", Decision: "deny"}}})
	changes := Diff(bridgeFixture(), cur)
	c, ok := changesByItem(changes)["bridges/forge-future"]
	if !ok {
		t.Fatalf("新桥 added 未出现在 diff: %+v", changes)
	}
	if c.Breaking {
		t.Errorf("新桥 added 应判非 Breaking: %+v", c)
	}
}

func TestScanBridges_ReadsContract(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "plugins", "forge-dsh")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	contract := map[string]any{
		"bridge":      "forge-dsh",
		"dshVerified": "0.1.0-rc.7",
		"events": []map[string]string{
			{"dsh": "tools/pre-execute", "forge": "PreToolUse", "decision": "deny"},
		},
		"failOpen": []string{"基础设施故障一律 fail-open"},
	}
	body, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "contract.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := scanBridges(root)
	if err != nil {
		t.Fatalf("scanBridges: %v", err)
	}
	if len(got) != 1 || got[0].Bridge != "forge-dsh" {
		t.Fatalf("扫描结果不符: %+v", got)
	}
	if len(got[0].Events) != 1 || got[0].Events[0].Dsh != "tools/pre-execute" {
		t.Fatalf("事件映射解析不符: %+v", got[0].Events)
	}
	if got[0].FailOpen[0] != "基础设施故障一律 fail-open" {
		t.Fatalf("failOpen 解析不符: %+v", got[0].FailOpen)
	}
}

func TestScanBridges_MissingFileEmpty(t *testing.T) {
	got, err := scanBridges(t.TempDir())
	if err != nil {
		t.Fatalf("契约文件缺失应容错为空面（与 payload 面同款）: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("契约文件缺失应为空面: %+v", got)
	}
}

func TestScanBridges_MalformedContractFailsLoud(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "plugins", "forge-dsh")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "contract.json"), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := scanBridges(root); err == nil {
		t.Fatal("契约文件损坏必须 fail loud（fail-visible），不应静默为空面")
	}
}
