package evalkit

// card.go — 治理披露卡（gates-card.yaml）：对外声明 Forge 作为广义 harness 占据
// ETCSOVG 哪些层、以什么机制、改了宿主行为的哪里，以及已知盲区。校验 fail-closed：
// 缺任一节（尤其"已知盲区"——诚实呈现的最低要求）即渲染拒绝。
//
// card.go — governance disclosure card: declares which ETCSOVG layers Forge
// occupies, via which mechanisms, what host behavior it alters, and the known
// blind spots. Fail-closed validation: any missing section (notably "known blind
// spots" — the minimum bar of honest presentation) rejects rendering.

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ETCSOVLayers lists the seven layers of the ETCSOVG taxonomy (arXiv
// 2605.23950). Forge's card claims a subset per layer — never all seven.
//
// ETCSOVLayers 列出 ETCSOVG 七层分类法（arXiv 2605.23950）。卡按层声明子集——
// 绝不声称全占七层。
var ETCSOVLayers = []string{"Execution", "Tool", "Context", "Scheduling", "Observability", "Verification", "Governance"}

// LayerClaim declares one ETCSOVG layer Forge occupies and the concrete
// mechanisms by which it alters host behavior there.
//
// LayerClaim 声明 Forge 占据的一个 ETCSOVG 层及其改变宿主行为的具体机制。
type LayerClaim struct {
	Layer      string   `yaml:"layer"       json:"layer"`
	Mechanisms []string `yaml:"mechanisms"  json:"mechanisms"`
}

// GateRow is one entry of the disclosure card's gate roster.
//
// GateRow 是披露卡门禁 roster 的一行。
type GateRow struct {
	ID    string `yaml:"id"    json:"id"`
	Kind  string `yaml:"kind"  json:"kind"`  // advisory | hard
	Where string `yaml:"where" json:"where"` // 触发面（如 task-verify）
}

// GatesCard is the parsed gates-card.yaml: what Forge changes about the host,
// stated so a user can audit it and turn it off.
//
// GatesCard 是解析后的 gates-card.yaml：如实声明 Forge 改了宿主的什么，让用户
// 可审计、可关闭。
type GatesCard struct {
	Version    int          `yaml:"version"     json:"version"`
	LayerClaim []LayerClaim `yaml:"layers"      json:"layers"`
	Hooks      []string     `yaml:"hooks"       json:"hooks"`
	Gates      []GateRow    `yaml:"gates"       json:"gates"`
	Escapes    []string     `yaml:"escapes"     json:"escapes"`
	BlindSpots []string     `yaml:"blind_spots" json:"blind_spots"`
}

// Validate enforces the card's invariants: version positive, every claimed
// layer is a real ETCSOVG layer with ≥1 mechanism, gates carry known kinds,
// and at least one known blind spot is disclosed (a card claiming no blind
// spots fails validation — that claim is never true).
//
// Validate 强制卡的不变量：version 为正、每个声明层是真实 ETCSOVG 层且 ≥1 条
// 机制、门禁 kind 合法、至少披露一条已知盲区（声称无盲区的卡校验失败——该声称
// 永远不为真）。
func (c *GatesCard) Validate() error {
	if c.Version <= 0 {
		return fmt.Errorf("evalkit: 披露卡 version 必须为正，得到 %d", c.Version)
	}
	if len(c.LayerClaim) == 0 {
		return fmt.Errorf("evalkit: 披露卡未声明任何占层")
	}
	seenLayer := map[string]bool{}
	for _, lc := range c.LayerClaim {
		known := false
		for _, l := range ETCSOVLayers {
			if l == lc.Layer {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("evalkit: 披露卡的层 %q 不在 ETCSOVG 七层内", lc.Layer)
		}
		if seenLayer[lc.Layer] {
			return fmt.Errorf("evalkit: 披露卡重复声明层 %s", lc.Layer)
		}
		seenLayer[lc.Layer] = true
		if len(lc.Mechanisms) == 0 {
			return fmt.Errorf("evalkit: 层 %s 缺机制声明", lc.Layer)
		}
	}
	for _, g := range c.Gates {
		if g.ID == "" {
			return fmt.Errorf("evalkit: 门禁 roster 条目缺 id")
		}
		if g.Kind != "advisory" && g.Kind != "hard" {
			return fmt.Errorf("evalkit: 门禁 %s 的 kind %q 非法（advisory|hard）", g.ID, g.Kind)
		}
	}
	if len(c.BlindSpots) == 0 {
		return fmt.Errorf("evalkit: 披露卡缺已知盲区节（诚实呈现的最低要求）")
	}
	return nil
}

// LoadCard reads and validates gates-card.yaml (fail-closed).
//
// LoadCard 读取并校验 gates-card.yaml（fail-closed）。
func LoadCard(path string) (*GatesCard, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("evalkit: 读取披露卡失败: %w", err)
	}
	var c GatesCard
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("evalkit: 解析披露卡失败: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// RenderMarkdown renders the disclosure card as user-facing Markdown. It fails
// closed on an invalid card — a card that cannot state what it changes does
// not get to render a clean sheet.
//
// RenderMarkdown 把披露卡渲染成面向用户的 Markdown。无效的卡渲染即失败——说
// 不清自己改了什么的卡，不配渲染出一张干净清单。
func (c *GatesCard) RenderMarkdown() (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("# Forge 治理披露卡\n\n")
	b.WriteString(fmt.Sprintf("> 本分数/本卡为 forge×model 组合声明：以下列出的机制改变你的宿主行为；全部可通过清单中的逃生舱或 `forge off` 关闭。\n\n"))
	b.WriteString("## 占层声明（ETCSOVG）\n\n")
	b.WriteString("| 层 | 机制 |\n|---|---|\n")
	for _, lc := range c.LayerClaim {
		b.WriteString(fmt.Sprintf("| %s | %s |\n", lc.Layer, strings.Join(lc.Mechanisms, "；")))
	}
	b.WriteString("\n## Hook 清单\n\n")
	for _, h := range c.Hooks {
		b.WriteString(fmt.Sprintf("- %s\n", h))
	}
	b.WriteString("\n## 门禁 roster\n\n")
	b.WriteString("| 门禁 | 级别 | 触发面 |\n|---|---|---|\n")
	for _, g := range c.Gates {
		b.WriteString(fmt.Sprintf("| %s | %s | %s |\n", g.ID, g.Kind, g.Where))
	}
	b.WriteString("\n## 逃生舱（全部留痕：checklog escape-hatch 行 + 证据封顶 Weak）\n\n")
	for _, e := range c.Escapes {
		b.WriteString(fmt.Sprintf("- %s\n", e))
	}
	b.WriteString("\n## 已知盲区\n\n")
	for _, bs := range c.BlindSpots {
		b.WriteString(fmt.Sprintf("- %s\n", bs))
	}
	return b.String(), nil
}
