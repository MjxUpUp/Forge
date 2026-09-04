// Package evalkit implements Forge's self-evaluation system (docs/design/
// forge-evaluation-system.md): a two-track measurement stack — Track A (agent-
// harness end-to-end: profile×model×benchmark runs) and Track B (governance-
// layer components: gate golden precision/recall, adversarial traps, judge
// audits, telemetry). It is observability and dev-time tooling only: it adds
// zero task gates and never changes the advisory/hard gate layering.
//
// Package evalkit 实现 Forge 自评测体系（docs/design/forge-evaluation-system.md）：
// 双轨测量栈——Track A（agent harness 端到端：profile×model×基准运行）与
// Track B（治理层组件：门禁 golden precision/recall、对抗陷阱、judge 审计、
// 遥测）。它只是观测与开发期工具：零新增任务门禁，绝不改变 advisory/hard
// 门禁分层。
package evalkit

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ClaimID enumerates the seven Forge claims the metrics dictionary hangs off
// (docs/design/forge-evaluation-system.md §三). The roster is the guard-test
// anchor: removing a claim from the dictionary fails the roster guard test.
//
// ClaimID 枚举指标字典所挂的七条 Forge 主张（docs/design/forge-evaluation-system.md
// §三）。该 roster 是 guard test 的锚点：从字典删掉一条主张，roster 守卫测试变红。
type ClaimID string

// The seven claims (single source of truth — mirrored only by the roster guard test).
//
// 七条主张（单一真相源——仅被 roster 守卫测试镜像）。
const (
	// ClaimGateCatches: gates catch real problems.
	ClaimGateCatches ClaimID = "C1" // 门禁拦真问题
	// ClaimGateStable: verdicts are stable and resist gaming.
	ClaimGateStable ClaimID = "C2" // 判定稳定、不被糊弄
	// ClaimContinuity: session handoff preserves context.
	ClaimContinuity ClaimID = "C3" // 接续不丢上下文
	// ClaimFriction: protocol-layer friction is tolerable.
	ClaimFriction ClaimID = "C4" // 协议层摩擦可承受
	// ClaimSkills: skills/conventions improve output.
	ClaimSkills ClaimID = "C5" // skills/约定提升产出
	// ClaimSafety: the policy surface blocks injections/abuse.
	ClaimSafety ClaimID = "C6" // 策略面安全
	// ClaimSelfDogfood: Forge eats its own dog food healthily.
	ClaimSelfDogfood ClaimID = "C7" // 自己吃自己狗粮
)

// AllClaims is the claim roster in canonical order.
//
// AllClaims 是规范顺序的主张 roster。
var AllClaims = []ClaimID{ClaimGateCatches, ClaimGateStable, ClaimContinuity, ClaimFriction, ClaimSkills, ClaimSafety, ClaimSelfDogfood}

// Track labels which measurement rail a metric belongs to.
//
// Track 标注指标属于哪条测量轨。
type Track string

const (
	// TrackA: end-to-end agent-harness measurement (profile×model×benchmark).
	TrackA Track = "track-a" // 端到端（profile×model×基准）
	// TrackB: governance-layer component measurement (gates/telemetry/judges).
	TrackB Track = "track-b" // 组件级（门禁/遥测/判分器）
)

// MetricDef is one metrics-dictionary entry. All seven fields are mandatory —
// LoadDictionary fails closed on any entry missing one (the misuse note and
// minimum-sample floor are what keep the numbers honest; a metric that cannot
// state how it would be misread does not get measured).
//
// MetricDef 是指标字典的一条条目。七个字段全部必填——LoadDictionary 对缺任一
// 字段的条目 fail-closed（误用注记与样本下限是数字诚实性的来源；说不出自己会
// 被怎么误读的指标不配被测量）。
type MetricDef struct {
	ID         string `yaml:"id"         json:"id"`
	Claim      string `yaml:"claim"      json:"claim"`
	Track      string `yaml:"track"      json:"track"`
	Definition string `yaml:"definition"  json:"definition"`
	Source     string `yaml:"source"      json:"source"`
	MisuseNote string `yaml:"misuse_note" json:"misuse_note"`
	MinSamples int    `yaml:"min_samples" json:"min_samples"`
}

// Dictionary is the parsed metrics.yaml (single source of truth for the whole
// evaluation system's metric surface).
//
// Dictionary 是解析后的 metrics.yaml（整个评测体系指标面的单一真相源）。
type Dictionary struct {
	Version int         `yaml:"version" json:"version"`
	Metrics []MetricDef `yaml:"metrics" json:"metrics"`
}

// Validate checks the dictionary's structural invariants: seven mandatory
// fields per entry, known claim IDs, known tracks, unique metric IDs, positive
// sample floors, and every claim C1-C7 covered by at least one metric.
//
// Validate 校验字典的结构不变量：每条七个必填字段、主张 ID 合法、track 合法、
// 指标 ID 唯一、样本下限为正、C1-C7 每条主张至少挂一个指标。
func (d *Dictionary) Validate() error {
	if d.Version <= 0 {
		return fmt.Errorf("evalkit: 字典 version 必须为正，得到 %d", d.Version)
	}
	if len(d.Metrics) == 0 {
		return fmt.Errorf("evalkit: 字典不含任何指标")
	}
	seen := make(map[string]bool, len(d.Metrics))
	covered := make(map[ClaimID]bool, len(AllClaims))
	for i := range d.Metrics {
		m := &d.Metrics[i]
		if m.ID == "" {
			return fmt.Errorf("evalkit: 第 %d 条指标缺 id", i+1)
		}
		if seen[m.ID] {
			return fmt.Errorf("evalkit: 指标 id 重复: %s", m.ID)
		}
		seen[m.ID] = true
		for field, val := range map[string]string{
			"claim":       m.Claim,
			"track":       m.Track,
			"definition":  m.Definition,
			"source":      m.Source,
			"misuse_note": m.MisuseNote,
		} {
			if val == "" {
				return fmt.Errorf("evalkit: 指标 %s 缺必填字段 %s", m.ID, field)
			}
		}
		claim := ClaimID(m.Claim)
		known := false
		for _, c := range AllClaims {
			if c == claim {
				known = true
				covered[c] = true
				break
			}
		}
		if !known {
			return fmt.Errorf("evalkit: 指标 %s 的 claim %q 不在 C1-C7 roster 内", m.ID, m.Claim)
		}
		if Track(m.Track) != TrackA && Track(m.Track) != TrackB {
			return fmt.Errorf("evalkit: 指标 %s 的 track %q 非法（仅 %s/%s）", m.ID, m.Track, TrackA, TrackB)
		}
		if m.MinSamples <= 0 {
			return fmt.Errorf("evalkit: 指标 %s 的 min_samples 必须为正（得到 %d）", m.ID, m.MinSamples)
		}
	}
	for _, c := range AllClaims {
		if !covered[c] {
			return fmt.Errorf("evalkit: 主张 %s 无任何指标挂靠（roster 完整性被破坏）", c)
		}
	}
	return nil
}

// LoadDictionary reads and validates metrics.yaml. Fail-closed: any structural
// defect (including an unreadable file) is an error — callers surface it as
// BLOCKED per the design's behavior contract (checklog eval-metrics-incomplete).
//
// LoadDictionary 读取并校验 metrics.yaml。Fail-closed：任何结构缺陷（含文件不可读）
// 都是错误——调用方按设计的行为契约上抛为 BLOCKED（checklog eval-metrics-incomplete）。
func LoadDictionary(path string) (*Dictionary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("evalkit: 读取指标字典失败: %w", err)
	}
	var d Dictionary
	if err := yaml.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("evalkit: 解析指标字典失败: %w", err)
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return &d, nil
}

// Find returns the metric definition by ID.
//
// Find 按 ID 返回指标定义。
func (d *Dictionary) Find(id string) (*MetricDef, bool) {
	for i := range d.Metrics {
		if d.Metrics[i].ID == id {
			return &d.Metrics[i], true
		}
	}
	return nil, false
}

// VCSAssetDir resolves the in-repo eval assets directory (evals/forge/) from
// the forge repo root. Callers pass the repo root they operate on; the assets
// live under <root>/evals/forge per the design's layout.
//
// VCSAssetDir 从 forge 仓库根解析仓内评测资产目录（evals/forge/）。调用方传入
// 所操作的仓库根；资产按设计的布局位于 <root>/evals/forge。
func VCSAssetDir(repoRoot string) string {
	return filepath.Join(repoRoot, "evals", "forge")
}
