package agentbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// loadJSON reads path into v, failing the test if the file is missing or not
// valid JSON.
func loadJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
}

// TestStripForgeMCPServer_NoFile：无 .mcp.json 时 no-op。
func TestStripForgeMCPServer_NoFile(t *testing.T) {
	dir := t.TempDir()
	changed, err := StripForgeMCPServer(dir)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if changed {
		t.Error(`无 .mcp.json 应 changed=false`)
	}
}

// TestStripForgeMCPServer_ForgeOnly_DeletesFile：.mcp.json 仅含 forge server，
// strip 后空 mcpServers → 删除整个文件。
func TestStripForgeMCPServer_ForgeOnly_DeletesFile(t *testing.T) {
	dir := t.TempDir()
	pre := `{"mcpServers":{"forge":{"command":"forge","args":["mcp","serve"]}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(pre), 0644); err != nil {
		t.Fatal(err)
	}
	changed, err := StripForgeMCPServer(dir)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !changed {
		t.Error(`forge only 应 changed=true`)
	}
	if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); !os.IsNotExist(err) {
		t.Error(`纯 forge 移除后 .mcp.json 应删除`)
	}
}

// TestStripForgeMCPServer_PreservesOtherServers：forge + 其他 server（github），
// 删 forge 保留 github。
func TestStripForgeMCPServer_PreservesOtherServers(t *testing.T) {
	dir := t.TempDir()
	pre := `{"mcpServers":{"forge":{"command":"forge","args":["mcp","serve"]},"github":{"command":"gh","args":[]}}}`
	os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(pre), 0644)
	changed, err := StripForgeMCPServer(dir)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !changed {
		t.Error(`应 changed=true`)
	}
	var cfg map[string]any
	loadJSON(t, filepath.Join(dir, ".mcp.json"), &cfg)
	servers := cfg["mcpServers"].(map[string]any)
	if _, ok := servers["forge"]; ok {
		t.Error(`forge server 未被移除`)
	}
	if _, ok := servers["github"]; !ok {
		t.Error(`其他 server（github）被误删`)
	}
}

// TestStripForgeMCPServer_NoForge_NoOp：无 forge server（纯 github）时 no-op。
func TestStripForgeMCPServer_NoForge_NoOp(t *testing.T) {
	dir := t.TempDir()
	pre := `{"mcpServers":{"github":{"command":"gh","args":[]}}}`
	os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(pre), 0644)
	changed, err := StripForgeMCPServer(dir)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if changed {
		t.Error(`无 forge server 应 changed=false（no-op）`)
	}
}

// TestStripForgeMCPServer_PreservesTopLevelFields：forge server + 其他顶层字段（version）
// —— 删 forge 后保留 version，空 mcpServers 键被删除（不残留空对象）。
func TestStripForgeMCPServer_PreservesTopLevelFields(t *testing.T) {
	dir := t.TempDir()
	pre := `{"mcpServers":{"forge":{"command":"forge","args":["mcp","serve"]}},"version":1}`
	os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(pre), 0644)
	changed, err := StripForgeMCPServer(dir)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !changed {
		t.Error(`应 changed=true`)
	}
	var cfg map[string]any
	loadJSON(t, filepath.Join(dir, ".mcp.json"), &cfg)
	if cfg["version"] != float64(1) {
		t.Errorf(`顶层 version 字段丢失: %v`, cfg["version"])
	}
	if _, ok := cfg["mcpServers"]; ok {
		t.Error(`空 mcpServers 应被删除（非保留空对象）`)
	}
}
