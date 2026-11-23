package agentproto_test

import (
	"os"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/agentproto"
	_ "github.com/MjxUpUp/Forge/internal/cli" // init 注册 rootCmd 回调（与 docsconsistency 同款破循环）
	"github.com/MjxUpUp/Forge/internal/docsconsistency"
)

// tree 从运行中的命令树取面（与 compat snapshot 面 1 同源）。
func tree(t *testing.T) []agentproto.Command {
	t.Helper()
	root := docsconsistency.CommandTree()
	if root == nil {
		t.Fatal("docsconsistency.CommandTree() 为 nil——cli 的 init 未注册命令树")
	}
	out := agentproto.FromCobra(root)
	if len(out) < 50 {
		t.Fatalf("命令树只有 %d 条命令，明显没走通", len(out))
	}
	return out
}

// 生成物必须确定性：同一命令树渲染两次逐字节相同，否则每次同步都制造 diff。
func TestRenderDeterministic(t *testing.T) {
	cmds := tree(t)
	a, err := agentproto.RenderAppendix(cmds)
	if err != nil {
		t.Fatalf("RenderAppendix: %v", err)
	}
	b, err := agentproto.RenderAppendix(cmds)
	if err != nil {
		t.Fatalf("RenderAppendix: %v", err)
	}
	if a != b {
		t.Fatal("两次渲染结果不同——附录不是确定性的")
	}
	if !strings.HasPrefix(a, agentproto.AppendixTitle) {
		t.Fatalf("附录标题不对：%q", a)
	}
	if n := strings.Count(a, "- `forge "); n < 10 {
		t.Fatalf("附录只列出 %d 条命令，注解表可能被清空了", n)
	}
	stamped := agentproto.Stamp(a, "9.9.9")
	if !strings.HasPrefix(stamped, agentproto.ProtoOpen+"9.9.9 ") {
		t.Fatalf("Stamp 没有把 proto 行放块首：%q", stamped)
	}
	if agentproto.Stamp(stamped, "9.9.9") != stamped {
		t.Fatal("Stamp 不幂等：同一正文重复 Stamp 结果变化")
	}
	v, sha, ok := agentproto.ParseProto(stamped)
	if !ok || v != "9.9.9" || len(sha) != 12 {
		t.Fatalf("ParseProto 读不回刚写的块头：v=%q sha=%q ok=%v", v, sha, ok)
	}
}

// 注解表与命令树必须双向对齐：注解指向的命令不存在时 agent 会被指向一条死路。
func TestAnnotationsMatchTree(t *testing.T) {
	cmds := tree(t)
	if _, err := agentproto.RenderAppendix(cmds); err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, c := range cmds {
		known[c.Path] = true
	}
	for p, note := range agentproto.Annotations {
		if !known[p] {
			t.Errorf("注解指向的命令不在树里：forge %s（%s）", p, note)
		}
		if strings.TrimSpace(note) == "" {
			t.Errorf("命令 %s 的注解为空", p)
		}
	}
}

// 协议散文里提到的每条命令都必须还在树里：这是 A 批真正要防的漂移。
func TestTemplateHasNoDeadCommand(t *testing.T) {
	raw, err := os.ReadFile("../../internal/change/templates/agents-protocol.md")
	if err != nil {
		t.Fatalf("读协议模板: %v", err)
	}
	issues := agentproto.Check(tree(t), string(raw))
	if len(issues) > 0 {
		t.Fatalf("协议散文引用了不存在的命令/flag（%d 处）：\n  %s", len(issues), strings.Join(issues, "\n  "))
	}
}

// 版本串来自 ldflags，真实形态带空格与括号；块头必须仍是可解析的单 token。
func TestStampNormalizesVersion(t *testing.T) {
	body := agentproto.Stamp("## 命令面（生成）", "v3.0.1-1-gabc123 (commit: deadbeef, built: 2026-11-23T10:39:17Z)")
	v, sha, ok := agentproto.ParseProto(body)
	if !ok {
		t.Fatalf("块头解析不回来：%q", body[:80])
	}
	if v != "v3.0.1-1-gabc123" {
		t.Fatalf("版本没有归一成单 token：%q", v)
	}
	if len(sha) != 12 {
		t.Fatalf("sha 长度不对：%q", sha)
	}
	if got := agentproto.Stamp(body, "v3.0.1-1-gabc123"); got != body {
		t.Fatal("归一后的版本与短版本不是同一个块头——幂等断裂")
	}
}
