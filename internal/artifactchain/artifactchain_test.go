package artifactchain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultChainShape(t *testing.T) {
	c := DefaultChain()
	if err := c.Validate(); err != nil {
		t.Fatalf("默认链必须自洽: %v", err)
	}
	if len(c.Stages) != 4 {
		t.Fatalf("默认链 4 节点，got %d", len(c.Stages))
	}
	wantOrder := []string{"proposal", "spec", "design", "plan"}
	for i, w := range wantOrder {
		if c.Stages[i].Name != w {
			t.Fatalf("默认链顺序：[%d]=%q，want %q", i, c.Stages[i].Name, w)
		}
		if c.Stages[i].Mode != ModeAdvisory {
			t.Fatalf("默认链全 advisory，%s=%s", w, c.Stages[i].Mode)
		}
	}
	if c.Enforcement() {
		t.Fatal("默认链全 advisory 不得执法（零配置零行为变化承诺）")
	}
}

func TestParseTwoPhaseValidation(t *testing.T) {
	t.Run("合法 schema 归一 mode 与默认 produces", func(t *testing.T) {
		c, err := Parse([]byte(`
version: 1
stages:
  - name: proposal
    requires: []
  - name: spec
    mode: hard
    requires: [proposal]
    instruction: 规格先行
`))
		if err != nil {
			t.Fatalf("合法 schema 不应报错: %v", err)
		}
		if c.Stages[0].Mode != ModeAdvisory {
			t.Fatalf("省略 mode 应归一 advisory，got %s", c.Stages[0].Mode)
		}
		if c.Stages[0].Produces != "proposal.md" {
			t.Fatalf("省略 produces 应默认 <name>.md，got %q", c.Stages[0].Produces)
		}
		if !c.Enforcement() {
			t.Fatal("含 hard 节点的链必须执法")
		}
	})
	t.Run("结构：非法 mode 与重复 stage", func(t *testing.T) {
		if _, err := Parse([]byte("version: 1\nstages:\n  - name: a\n    mode: ultra\n")); err == nil || !strings.Contains(err.Error(), "mode") {
			t.Fatalf("非法 mode 应报错，got %v", err)
		}
		if _, err := Parse([]byte("version: 1\nstages:\n  - name: a\n  - name: a\n")); err == nil || !strings.Contains(err.Error(), "重复") {
			t.Fatalf("重复 stage 应报错，got %v", err)
		}
	})
	t.Run("结构：version 与保留名", func(t *testing.T) {
		if _, err := Parse([]byte("version: 2\nstages:\n  - name: a\n")); err == nil {
			t.Fatal("version 2 应报错")
		}
		if _, err := Parse([]byte("version: 1\nstages:\n  - name: attempts\n")); err == nil || !strings.Contains(err.Error(), "保留") {
			t.Fatalf("保留名 attempts 应报错，got %v", err)
		}
	})
	t.Run("结构：produces 路径安全", func(t *testing.T) {
		for _, bad := range []string{"../evil.md", "/abs.md", "sub/dir.md"} {
			schema := "version: 1\nstages:\n  - name: a\n    produces: '" + bad + "'\n"
			if _, err := Parse([]byte(schema)); err == nil {
				t.Fatalf("produces %q 应拒绝", bad)
			}
		}
	})
	t.Run("语义：requires 未声明引用与环", func(t *testing.T) {
		if _, err := Parse([]byte("version: 1\nstages:\n  - name: a\n    requires: [ghost]\n")); err == nil || !strings.Contains(err.Error(), "未声明") {
			t.Fatalf("幽灵引用应报错，got %v", err)
		}
		cyc := "version: 1\nstages:\n  - name: a\n    requires: [b]\n  - name: b\n    requires: [a]\n"
		if _, err := Parse([]byte(cyc)); err == nil || !strings.Contains(err.Error(), "成环") {
			t.Fatalf("成环应报错，got %v", err)
		}
		if _, err := Parse([]byte("version: 1\nstages:\n  - name: a\n    requires: [a]\n")); err == nil || !strings.Contains(err.Error(), "成环") {
			t.Fatalf("自环应报错，got %v", err)
		}
	})
	t.Run("语义：合法 DAG 通过", func(t *testing.T) {
		dag := "version: 1\nstages:\n  - name: b\n    requires: [a]\n  - name: a\n  - name: c\n    requires: [a, b]\n"
		if _, err := Parse([]byte(dag)); err != nil {
			t.Fatalf("合法 DAG 不应报错: %v", err)
		}
	})
}

func TestLoadFailsOpen(t *testing.T) {
	t.Run("无 schema 静默默认", func(t *testing.T) {
		root := t.TempDir()
		c, warns := Load(root)
		if len(warns) != 0 {
			t.Fatalf("零配置应零警告，got %v", warns)
		}
		if len(c.Stages) != 4 {
			t.Fatalf("无 schema 回落默认链，got %d 节点", len(c.Stages))
		}
	})
	t.Run("坏 YAML 回落默认链 + 警告", func(t *testing.T) {
		root := t.TempDir()
		p := SchemaPath(root)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("version: [unclosed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		c, warns := Load(root)
		if len(warns) == 0 {
			t.Fatal("坏 schema 必须给 stderr 警告（可观测的 fail-open）")
		}
		if !strings.Contains(warns[0], "回落默认链") {
			t.Fatalf("警告应说明回落: %q", warns[0])
		}
		if len(c.Stages) != 4 {
			t.Fatalf("fail-open 必须回落默认链，got %d 节点", len(c.Stages))
		}
	})
	t.Run("语义非法回落默认链 + 警告", func(t *testing.T) {
		root := t.TempDir()
		p := SchemaPath(root)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		cyc := "version: 1\nstages:\n  - name: a\n    mode: hard\n    requires: [b]\n  - name: b\n    requires: [a]\n"
		if err := os.WriteFile(p, []byte(cyc), 0o644); err != nil {
			t.Fatal(err)
		}
		c, warns := Load(root)
		if len(warns) == 0 || len(c.Stages) != 4 {
			t.Fatalf("语义非法须回落+警告，got warns=%v stages=%d", warns, len(c.Stages))
		}
	})
}

func TestStageByName(t *testing.T) {
	c := DefaultChain()
	if s, ok := c.StageByName("spec"); !ok || s.Name != "spec" {
		t.Fatalf("StageByName(spec) 失败: %v %v", s, ok)
	}
	if _, ok := c.StageByName("ghost"); ok {
		t.Fatal("ghost 不应命中")
	}
}

// TestEdgeDeclarationAndValidation 钉住回边声明语义（「回边语义」节）：缺省值
// 归一（max_rounds=3 / exhaustion=escalate / progress=fingerprint）、重复回边
// 拒绝、非法耗尽语义拒绝、EdgeByFrom 查找。
func TestEdgeDeclarationAndValidation(t *testing.T) {
	t.Run("缺省归一与查找", func(t *testing.T) {
		c, err := Parse([]byte("version: 1\nstages:\n  - name: a\nedges:\n  - from: review\n    to: implement\n"))
		if err != nil {
			t.Fatalf("合法回边不应报错: %v", err)
		}
		e, ok := c.EdgeByFrom("review")
		if !ok {
			t.Fatal("review 回边应可查")
		}
		if e.MaxRounds != 3 || e.Exhaustion != ExhaustionEscalate || e.Progress != "fingerprint" {
			t.Fatalf("缺省归一不符: %+v", e)
		}
	})
	t.Run("非法耗尽语义与重复回边拒绝", func(t *testing.T) {
		if _, err := Parse([]byte("version: 1\nstages:\n  - name: a\nedges:\n  - from: review\n    to: implement\n    exhaustion: forever\n")); err == nil || !strings.Contains(err.Error(), "escalate") {
			t.Fatalf("非法 exhaustion 应报错: %v", err)
		}
		dup := "version: 1\nstages:\n  - name: a\nedges:\n  - from: review\n    to: implement\n  - from: review\n    to: implement\n"
		if _, err := Parse([]byte(dup)); err == nil || !strings.Contains(err.Error(), "重复") {
			t.Fatalf("重复回边应报错: %v", err)
		}
	})
	t.Run("负预算拒绝", func(t *testing.T) {
		if _, err := Parse([]byte("version: 1\nstages:\n  - name: a\nedges:\n  - from: review\n    to: implement\n    max_rounds: -1\n")); err == nil {
			t.Fatal("负 max_rounds 应报错")
		}
	})
}
