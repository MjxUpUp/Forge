package evalkit

// card_test.go — 披露卡校验与渲染（含仓内资产守卫）。

import (
	"path/filepath"
	"strings"
	"testing"
)

func validTestCard() GatesCard {
	return GatesCard{
		Version: 1,
		LayerClaim: []LayerClaim{
			{Layer: "Verification", Mechanisms: []string{"auto-compile hook"}},
			{Layer: "Governance", Mechanisms: []string{"checklog 审计"}},
		},
		Hooks:      []string{"auto-compile"},
		Gates:      []GateRow{{ID: "task-verify", Kind: "advisory", Where: "verify"}},
		Escapes:    []string{"FORGE_TEST_COVERAGE"},
		BlindSpots: []string{"Sig v1 恒空"},
		Attest: []CheckerAttestationRow{{
			Class: "GuardFall A 引号并词", Bypass: `r"m" -rf`,
			Detector: "语义分词层", Evidence: "hazardguard-blocks-guardfall-quote-merge",
		}},
	}
}

func TestGatesCardValidateAndRender(t *testing.T) {
	c := validTestCard()
	if err := c.Validate(); err != nil {
		t.Fatalf("合法卡不应报错: %v", err)
	}
	md, err := c.RenderMarkdown()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"占层声明", "Hook 清单", "门禁 roster", "逃生舱", "已知盲区", "检查器架构自证"} {
		if !strings.Contains(md, want) {
			t.Fatalf("渲染缺节 %q", want)
		}
	}
}

func TestGatesCardFailClosed(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*GatesCard)
	}{
		{"缺已知盲区", func(c *GatesCard) { c.BlindSpots = nil }},
		{"缺检查器架构自证", func(c *GatesCard) { c.Attest = nil }},
		{"自证行缺证据", func(c *GatesCard) { c.Attest[0].Evidence = "" }},
		{"非法层名", func(c *GatesCard) { c.LayerClaim[0].Layer = "Magic" }},
		{"层缺机制", func(c *GatesCard) { c.LayerClaim[0].Mechanisms = nil }},
		{"非法门禁 kind", func(c *GatesCard) { c.Gates[0].Kind = "maybe" }},
		{"version 非正", func(c *GatesCard) { c.Version = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validTestCard()
			tc.mutate(&c)
			if err := c.Validate(); err == nil {
				t.Fatal("期望 fail-closed 报错")
			}
		})
	}
}

func TestLoadCardRepoAsset(t *testing.T) {
	c, err := LoadCard(filepath.Join("..", "..", "evals", "forge", "gates-card.yaml"))
	if err != nil {
		t.Fatalf("仓内 gates-card.yaml 校验失败: %v", err)
	}
	if len(c.LayerClaim) < 4 {
		t.Fatalf("Forge 声明应占 ≥4 层（C/S/V/G），得到 %d", len(c.LayerClaim))
	}
	// GuardFall 五类（A-E）逐一自证——缺一类即盲区未披露（2026-09 W6）。
	if len(c.Attest) < 5 {
		t.Fatalf("检查器架构自证应覆盖 GuardFall 五类（≥5 行），得到 %d", len(c.Attest))
	}
	md, err := c.RenderMarkdown()
	if err != nil || !strings.Contains(md, "已知盲区") || !strings.Contains(md, "检查器架构自证") {
		t.Fatalf("渲染失败或缺节: %v", err)
	}
}
