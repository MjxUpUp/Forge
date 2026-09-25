package taskpipeline

import (
	"os"
	"path/filepath"
	"testing"
)

// TestParseAcceptanceFromArtifact 钉住三种行形态与围栏边界（§4 契约）。
func TestParseAcceptanceFromArtifact(t *testing.T) {
	t.Run("accept 列表行（含中文同义与列表前缀）", func(t *testing.T) {
		text := `## 验收

- accept: go test ./... :: ok
* 验收: go vet ./...
accept: echo done :: done
`
		got := ParseAcceptanceFromArtifact(text)
		if len(got) != 3 {
			t.Fatalf("应提取 3 条, got %d: %+v", len(got), got)
		}
		if got[0].Run != "go test ./..." || got[0].Expected != "ok" {
			t.Errorf("第 1 条形态不符: %+v", got[0])
		}
		if got[1].Run != "go vet ./..." || got[1].Expected != "" {
			t.Errorf("裸 accept 应只看退出码: %+v", got[1])
		}
	})
	t.Run("Run/Expected 行对与 plan 解析同语义", func(t *testing.T) {
		text := "Run: go build ./...\nExpected: (无输出即成功)\n"
		got := ParseAcceptanceFromArtifact(text)
		if len(got) != 1 || got[0].Run != "go build ./..." {
			t.Fatalf("Run/Expected 应配对提取: %+v", got)
		}
	})
	t.Run("accept 围栏块", func(t *testing.T) {
		text := "```accept\ngo test ./internal/... :: ok\ngo vet ./...\n```\n"
		got := ParseAcceptanceFromArtifact(text)
		if len(got) != 2 {
			t.Fatalf("围栏内 2 条应全提: %+v", got)
		}
	})
	t.Run("普通代码围栏内的 accept 字样不误提", func(t *testing.T) {
		text := "示例：\n```sh\naccept: fake-command :: nope\n```\n"
		got := ParseAcceptanceFromArtifact(text)
		if len(got) != 0 {
			t.Fatalf("代码围栏内不应提取: %+v", got)
		}
	})
	t.Run("无验收内容返回空", func(t *testing.T) {
		if got := ParseAcceptanceFromArtifact("# 纯叙事文档\n没有验收标准。\n"); len(got) != 0 {
			t.Fatalf("纯叙事应返回空: %+v", got)
		}
	})
}

// TestParseAcceptanceFromArtifactFile 走文件路径封装。
func TestParseAcceptanceFromArtifactFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(p, []byte("- accept: echo hi :: hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ParseAcceptanceFromArtifactFile(p)
	if err != nil || len(got) != 1 {
		t.Fatalf("文件提取失败: %+v err=%v", got, err)
	}
}
