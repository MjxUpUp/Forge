// Package artifactchain implements the declarative artifact chain (multi-task-
// concurrency §9's schema.yaml DAG): stage nodes with gate modes that the
// task-implement gate enforces.
//
// Package artifactchain 实现声明式产物链（multi-task-concurrency §9 的 schema.yaml
// DAG）：阶段节点 + 门禁分档，由 task-implement gate 执法（docs/design/
// artifact-chain-workflow.md）。本包保持纯解析/校验——不 import taskpipeline
// （后者 import 本包，依赖方向单一）；产物引用与哈希校验归 taskpipeline（specs.go）。
//
// fail-open 宪法（leverage-points-landing.md）：schema 缺失/解析失败/校验失败一律
// 回落默认链（全 advisory）+ stderr 警告——配置错误绝不阻断任务。只有「显式
// rubric/human/hard 配置 + 事实缺失」才在 gate 处阻断（HARD 只守事实）。
package artifactchain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"gopkg.in/yaml.v3"
)

// Mode is a stage's gate enforcement tier at task-implement.
//
// Mode 是 stage 在 task-implement gate 的执法档位。advisory 只提醒（叙事阶段靠
// 纪律的现状）；rubric/human/hard 升档为事实门禁——hard 只守事实（产物存在 +
// 引用哈希一致），rubric 只加机械 L1 lint，human 加「哈希匹配的人工审批」；
// 意见类判断（L2 rubric）永不进 gate。
type Mode string

const (
	ModeAdvisory Mode = "advisory"
	ModeRubric   Mode = "rubric"
	ModeHuman    Mode = "human"
	ModeHard     Mode = "hard"
)

// ValidMode reports whether m is one of the four known tiers.
func ValidMode(m Mode) bool {
	switch m {
	case ModeAdvisory, ModeRubric, ModeHuman, ModeHard:
		return true
	}
	return false
}

// Blocking reports whether the mode enforces (blocks) rather than advises.
func (m Mode) Blocking() bool { return m == ModeRubric || m == ModeHuman || m == ModeHard }

// Stage is one node of the artifact chain.
//
// Stage 是产物链的一个节点。Produces 是 specs/<ref>/ 内的文件名（默认 <name>.md）；
// Requires 声明前置 stage（语义校验保证 DAG；执法阶段对 blocking 档检查前置存在
// ——「进入下一节点先查上一节点产物」）。
type Stage struct {
	Name        string   `yaml:"name" json:"name"`
	Mode        Mode     `yaml:"mode,omitempty" json:"mode"`
	Produces    string   `yaml:"produces,omitempty" json:"produces,omitempty"`
	Requires    []string `yaml:"requires,omitempty" json:"requires,omitempty"`
	Instruction string   `yaml:"instruction,omitempty" json:"instruction,omitempty"`
}

// Chain is the declared artifact chain: ordered stages forming a DAG.
type Chain struct {
	Version int     `yaml:"version" json:"version"`
	Stages  []Stage `yaml:"stages" json:"stages"`
}

// SchemaPath returns the project-level chain schema location:
// <DataDir>/schemas/schema.yaml (same side as specs/, versioned with the
// harness repo).
//
// SchemaPath 返回项目级链 schema 位置：<DataDir>/schemas/schema.yaml（与 specs/
// 同侧，随 harness repo 版本化；multi-task-concurrency §9 原案落位）。
func SchemaPath(root string) string {
	return filepath.Join(forgedata.DataDirFor(root), "schemas", "schema.yaml")
}

// reservedStageName 排除 specs/<ref>/ 目录里的非产物条目（attempts/ 是审查失败
// 轮次归档目录，specs.go ArchiveAttempt 一次写入语义，不可被 stage 名侵占）。
const reservedStageName = "attempts"

// DefaultChain returns the built-in proposal→spec→design→plan chain, all
// advisory. Zero-config behavior change vs the pre-chain world is exactly one
// one-shot implement-gate advisory — 叙事阶段靠纪律的现状不被突袭。
func DefaultChain() *Chain {
	return &Chain{
		Version: 1,
		Stages: []Stage{
			{Name: "proposal", Mode: ModeAdvisory, Produces: "proposal.md",
				Instruction: "提案：为什么做、解决谁的什么问题、备选方案与取舍"},
			{Name: "spec", Mode: ModeAdvisory, Produces: "spec.md",
				Instruction: "规格：验收标准（accept: <cmd> :: <expected> 行可被 forge task artifact --extract 编译成验收门禁）"},
			{Name: "design", Mode: ModeAdvisory, Produces: "design.md",
				Instruction: "技术方案：结构/接口/数据流与既有代码的关系"},
			{Name: "plan", Mode: ModeAdvisory, Produces: "plan.md",
				Instruction: "执行计划：任务拆解与 Run/Expected 验收命令"},
		},
	}
}

// Load reads and validates the project chain schema, failing open to
// DefaultChain with warnings on any problem (missing file is silent default —
// zero-config projects get zero noise).
//
// Load 读取并校验项目链 schema，任何问题都 fail-open 回落默认链并给警告（文件
// 缺失静默用默认——零配置项目零噪音）。绝不因配置错误阻断任务。
func Load(root string) (chain *Chain, warnings []string) {
	data, err := os.ReadFile(SchemaPath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultChain(), nil
		}
		return DefaultChain(), []string{fmt.Sprintf("[artifact-chain] schema 读取失败，回落默认链: %v", err)}
	}
	c, err := Parse(data)
	if err != nil {
		return DefaultChain(), []string{fmt.Sprintf("[artifact-chain] schema 非法（%v），回落默认链（全 advisory）", err)}
	}
	return c, nil
}

// Parse unmarshals and validates schema bytes (two-phase: structural then
// semantic, per multi-task-concurrency §9).
func Parse(data []byte) (*Chain, error) {
	var c Chain
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("yaml 解析失败: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate runs the two-phase validation. Phase 1 structural: version/stage
// names/modes/produces path safety. Phase 2 semantic: requires reference
// existing stages and the graph is acyclic.
//
// Validate 跑两阶段校验。阶段 1 结构：version/stage 名/mode/produces 路径安全。
// 阶段 2 语义：requires 引用存在的 stage 且图无环。
func (c *Chain) Validate() error {
	// —— 阶段 1：结构 ——
	if c.Version != 1 {
		return fmt.Errorf("chain: version 仅支持 1，got %d", c.Version)
	}
	if len(c.Stages) == 0 {
		return fmt.Errorf("chain: stages 不能为空")
	}
	seen := make(map[string]struct{}, len(c.Stages))
	for i := range c.Stages {
		s := &c.Stages[i]
		if strings.TrimSpace(s.Name) == "" {
			return fmt.Errorf("chain: stages[%d] name 不能为空", i)
		}
		if s.Name == reservedStageName {
			return fmt.Errorf("chain: stage 名 %q 保留（attempts 是审查归档目录）", s.Name)
		}
		if strings.ContainsAny(s.Name, "/\\ \t") {
			return fmt.Errorf("chain: stage 名 %q 含非法字符（禁路径分隔符与空白）", s.Name)
		}
		if _, dup := seen[s.Name]; dup {
			return fmt.Errorf("chain: stage %q 重复声明", s.Name)
		}
		seen[s.Name] = struct{}{}
		if s.Mode == "" {
			s.Mode = ModeAdvisory // 归一：省略 mode = advisory（声明面最小化）
		}
		if !ValidMode(s.Mode) {
			return fmt.Errorf("chain: stage %q mode %q 非法（advisory|rubric|human|hard）", s.Name, s.Mode)
		}
		if s.Produces == "" {
			s.Produces = s.Name + ".md"
		}
		if err := validateProduces(s.Produces); err != nil {
			return fmt.Errorf("chain: stage %q %w", s.Name, err)
		}
	}
	// —— 阶段 2：语义 ——
	for i := range c.Stages {
		for _, r := range c.Stages[i].Requires {
			if _, ok := seen[r]; !ok {
				return fmt.Errorf("chain: stage %q requires 未声明的 stage %q", c.Stages[i].Name, r)
			}
		}
	}
	if cyc := findCycle(c.Stages); cyc != "" {
		return fmt.Errorf("chain: requires 成环（经 %q）——产物链必须是 DAG", cyc)
	}
	return nil
}

// validateProduces enforces specs-dir-relative path safety: the produced file
// lands under SpecsDir via WriteArtifact, so "..", absolute paths and path
// separators that escape sideways are rejected.
func validateProduces(p string) error {
	if p == "" {
		return fmt.Errorf("produces 不能为空")
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "~/") {
		return fmt.Errorf("produces %q 必须是 specs 目录内相对路径", p)
	}
	clean := filepath.ToSlash(filepath.Clean(p))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("produces %q 越出 specs 目录（禁 ..）", p)
	}
	if strings.ContainsAny(clean, `\/`) {
		return fmt.Errorf("produces %q 须是 specs 目录内单级文件名（禁子目录）", p)
	}
	return nil
}

// findCycle returns a node on a requires-cycle ("" if acyclic). DFS with
// three-color marking; chain sizes are tiny (handful of stages) so recursion
// is fine.
func findCycle(stages []Stage) string {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(stages))
	var visit func(name string) string
	visit = func(name string) string {
		color[name] = gray
		for _, s := range stages {
			if s.Name != name {
				continue
			}
			for _, r := range s.Requires {
				switch color[r] {
				case gray:
					return r
				case white:
					if hit := visit(r); hit != "" {
						return hit
					}
				}
			}
		}
		color[name] = black
		return ""
	}
	for _, s := range stages {
		if color[s.Name] == white {
			if hit := visit(s.Name); hit != "" {
				return hit
			}
		}
	}
	return ""
}

// StageByName returns the stage with the given name.
func (c *Chain) StageByName(name string) (Stage, bool) {
	for _, s := range c.Stages {
		if s.Name == name {
			return s, true
		}
	}
	return Stage{}, false
}

// Enforcement reports whether any stage blocks (mode != advisory). A purely
// advisory chain never changes gate outcomes — the aggregate one-shot
// advisory is the only signal.
func (c *Chain) Enforcement() bool {
	for _, s := range c.Stages {
		if s.Mode.Blocking() {
			return true
		}
	}
	return false
}
