package agentbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// TestStripForgeMCPServer drives the strip contract over one table: no-op without .mcp.json, whole-file deletion when the file is forge-only, preservation of other servers and top-level fields (with the emptied mcpServers key dropped — no empty object left behind), and no-op when no forge server is present.
//
// TestStripForgeMCPServer 用一张表驱动 strip 契约：无 .mcp.json 时 no-op；纯
// forge 文件删除整个文件；其他 server 与顶层字段保留（清空的 mcpServers 键被
// 删除——不残留空对象）；无 forge server 时 no-op。
func TestStripForgeMCPServer(t *testing.T) {
	cases := []struct {
		name        string
		seed        string // empty → no .mcp.json at all
		wantChanged bool
		wantExists  bool     // file must exist after strip
		wantBody    []string // substrings the remaining body must contain
		wantGone    []string // substrings the remaining body must NOT contain
		wantTopGone []string // top-level JSON keys that must be absent after strip
	}{
		{
			name:        "NoFile",
			seed:        ``,
			wantChanged: false,
			wantExists:  false,
		},
		{
			name:        "ForgeOnly_DeletesFile",
			seed:        `{"mcpServers":{"forge":{"command":"forge","args":["mcp","serve"]}}}`,
			wantChanged: true,
			wantExists:  false,
		},
		{
			name:        "PreservesOtherServers",
			seed:        `{"mcpServers":{"forge":{"command":"forge","args":["mcp","serve"]},"github":{"command":"gh","args":[]}}}`,
			wantChanged: true,
			wantExists:  true,
			wantBody:    []string{`"github"`},
			wantGone:    []string{`"forge"`},
		},
		{
			name:        "NoForge_NoOp",
			seed:        `{"mcpServers":{"github":{"command":"gh","args":[]}}}`,
			wantChanged: false,
			wantExists:  true,
			wantBody:    []string{`"github"`},
		},
		{
			name:        "PreservesTopLevelFields",
			seed:        `{"mcpServers":{"forge":{"command":"forge","args":["mcp","serve"]}},"version":1}`,
			wantChanged: true,
			wantExists:  true,
			wantBody:    []string{`"version"`},
			wantTopGone: []string{"mcpServers"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.seed != "" {
				if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(tc.seed), 0644); err != nil {
					t.Fatal(err)
				}
			}
			changed, err := StripForgeMCPServer(dir)
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if changed != tc.wantChanged {
				t.Errorf("changed = %v, want %v", changed, tc.wantChanged)
			}
			_, statErr := os.Stat(filepath.Join(dir, ".mcp.json"))
			if exists := statErr == nil; exists != tc.wantExists {
				t.Fatalf("file exists = %v, want %v (stat err = %v)", exists, tc.wantExists, statErr)
			}
			if !tc.wantExists {
				return
			}
			data, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
			if err != nil {
				t.Fatal(err)
			}
			body := string(data)
			for _, want := range tc.wantBody {
				if !strings.Contains(body, want) {
					t.Errorf(`body 缺 %q: %s`, want, body)
				}
			}
			for _, gone := range tc.wantGone {
				if strings.Contains(body, gone) {
					t.Errorf(`body 不应再含 %q: %s`, gone, body)
				}
			}
			if len(tc.wantTopGone) > 0 {
				var cfg map[string]json.RawMessage
				loadJSON(t, filepath.Join(dir, ".mcp.json"), &cfg)
				for _, key := range tc.wantTopGone {
					if _, ok := cfg[key]; ok {
						t.Errorf(`空 %s 应被删除（非保留空对象）`, key)
					}
				}
			}
		})
	}
}
