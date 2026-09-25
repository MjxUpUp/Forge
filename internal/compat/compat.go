// Package compat implements forge's executable compatibility artifact
// (mechanism-hardening P1-1): a deterministic seven-surface snapshot of forge's
// externally visible contract, diffable across versions like API Extractor's
// *.api.md golden files.
//
// Package compat 实现 forge 的可执行兼容工件（mechanism-hardening P1-1）：七面
// 确定性快照 + 跨版本 diff——API Extractor 模型（golden 入库 + PR diff 呈现 +
// 破坏性变更显式评审）。
//
// 七面（每面附检测边界——工件 diff 的已知盲区按机制史调研写明，诚实呈现）：
//  1. commands   — cobra 树的命令路径 + flag 名（检测边界：flag 语义变化不可见）
//  2. checks     — checklog CheckName roster（边界：Detail 散文语义不可见）
//  3. escapes    — FORGE_* 逃生舱 env 清单（边界：默认值变化不可见）
//  4. payload    — skills 两棵树的 name→sha256（边界：references 内容不在面内）
//  5. schemas    — 序列化结构的键集合（边界：字段类型变化在键不变时不可见）
//  6. blockings  — internal/ 源码中 GateBlocked(/LevelBlocked 位点计数（边界：
//     hook 脚本内的阻断不可见——bash 字符串不在 Go 源扫描面）
//  7. bridges    — 外部桥契约（plugins/*/contract.json 的事件映射 + fail-open
//     承诺；边界：桥的 JS 行为实现与契约的符合性由插件侧 wiring 测试钉住，
//     本面只执法契约文件的自身变更——见 compat-bridge-face.md）
//
// 确定性契约：同一棵树两次实算字节一致（排序键、无时间戳）——下游可 diff 两份
// 快照定位增量（AAT 导出同款哲学）。
package compat

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Snapshot 是七面快照（确定性序列化：map 一律转排序 slice）。
type Snapshot struct {
	Commands  []CommandSurface    `json:"commands"`
	Checks    []string            `json:"checks"`
	Escapes   []string            `json:"escapes"`
	Payload   []PayloadItem       `json:"payload"`
	Schemas   map[string][]string `json:"schemas"`
	Blockings []BlockingSite      `json:"blockings"`
	Bridges   []BridgeContract    `json:"bridges"`
}

// CommandSurface 是一条命令的表面（路径 + 排序后的 flag 名）。
type CommandSurface struct {
	Path  string   `json:"path"`
	Flags []string `json:"flags,omitempty"`
}

// PayloadItem 是内嵌载荷的一项（树/skill 名/内容指纹）。
type PayloadItem struct {
	Tree string `json:"tree"`
	Name string `json:"name"`
	Hash string `json:"sha256"`
}

// BlockingSite 是源码中一个阻断位点（file + 计数）。
type BlockingSite struct {
	File  string `json:"file"`
	Count int    `json:"count"`
}

// BridgeContract 是一个外部桥（内核外进程内接线层，如 plugins/forge-dsh）的
// 行为契约快照：事件映射 + fail-open 承诺。契约文件是桥两端（宿主插件 ↔ forge
// 内核）的单一真相源，README 的人类可读表格镜像自它（compat-bridge-face.md）。
type BridgeContract struct {
	Bridge      string        `json:"bridge"`
	DshVerified string        `json:"dshVerified,omitempty"`
	Events      []BridgeEvent `json:"events"`
	FailOpen    []string      `json:"failOpen,omitempty"`
}

// BridgeEvent 是桥的一行事件映射（dsh 事件 → forge hook 事件 → 决策形状）。
// Note 是注释非契约——不参与 Diff 判级。
type BridgeEvent struct {
	Dsh      string `json:"dsh"`
	Forge    string `json:"forge"`
	Decision string `json:"decision"`
	Note     string `json:"note,omitempty"`
}

// EscapeEnvs 是逃生舱 env 清单（显式列表——单一真相源；新增逃生舱必须同步此处，
// 守卫见 compat 包测试的对照扫描）。
var EscapeEnvs = []string{
	"FORGE_WORK_ACTIVITY",
	"FORGE_TEST_COVERAGE",
	"FORGE_ACCEPTANCE_GATE",
	"FORGE_DOC_GATE",
	"FORGE_SKILL_DECISIONS",
	"FORGE_SELF_REPORT",
	"FORGE_HELDOUT",
	"FORGE_GATE_PUSH",
	"FORGE_ARTIFACT_CHAIN",
	"FORGE_REQ_HYGIENE",
	"FORGE_UNUSED_SCAN",
	"FORGE_TASK_DRIFT",
	"FORGE_TASK_VERIFY_STOP",
	"FORGE_MUTATION_TIMEOUT",
	"FORGE_GATE_CMD_FORM",
	"FORGE_HAZARD_PENDING",
	"FORGE_MUTATION_GATE",
}

// AllCheckNames 返回 checklog 常量 roster 的排序列表。
func AllCheckNames() []string {
	// 显式枚举（types.go 常量的消费侧单一真相源；types.go 侧新增常量时此处
	// 同步——guard 测试用反射对照防漏）。
	return checklog.AllCheckNames()
}

// BuildSnapshot 在 root（仓根）上实算七面。rootCmd 从 docsconsistency 的注册
// 回调拿（cli 包注入；compat 不 import cli——依赖方向）。
func BuildSnapshot(root string, rootCmd *cobra.Command) (*Snapshot, error) {
	snap := &Snapshot{Schemas: map[string][]string{}}

	// 面1：命令树。
	if rootCmd != nil {
		var walk func(cmd *cobra.Command, prefix string)
		walk = func(cmd *cobra.Command, prefix string) {
			// cobra 惰性注入的 completion/help 不属于 forge 的 API 面（且测试进程
			// 里 completion 未注册——纳入会破坏确定性的进程间一致性）。
			if cmd.Name() == "completion" || cmd.Name() == "help" {
				return
			}
			path := prefix + cmd.Name()
			var flags []string
			cmd.Flags().VisitAll(func(f *pflag.Flag) {
				if f.Name == "help" {
					return
				}
				flags = append(flags, f.Name)
			})
			sort.Strings(flags)
			snap.Commands = append(snap.Commands, CommandSurface{Path: path, Flags: flags})
			for _, sub := range cmd.Commands() {
				if sub.Name() == "help" || sub.Hidden {
					continue
				}
				walk(sub, path+" ")
			}
		}
		walk(rootCmd, "")
		sort.Slice(snap.Commands, func(i, j int) bool { return snap.Commands[i].Path < snap.Commands[j].Path })
	}

	// 面2：CheckName roster。
	snap.Checks = AllCheckNames()

	// 面3：逃生舱 env。
	snap.Escapes = append([]string(nil), EscapeEnvs...)
	sort.Strings(snap.Escapes)

	// 面4：内嵌载荷（两棵树）。
	for _, t := range []struct{ dir, label string }{{"skills", "canonical"}, {"skills-forge", "forge"}} {
		base := filepath.Join(root, t.dir)
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			data, err := os.ReadFile(filepath.Join(base, e.Name(), "SKILL.md"))
			if err != nil {
				continue
			}
			sum := sha256.Sum256(data)
			snap.Payload = append(snap.Payload, PayloadItem{
				Tree: t.label, Name: e.Name(), Hash: hex.EncodeToString(sum[:]),
			})
		}
	}
	sort.Slice(snap.Payload, func(i, j int) bool {
		if snap.Payload[i].Tree != snap.Payload[j].Tree {
			return snap.Payload[i].Tree < snap.Payload[j].Tree
		}
		return snap.Payload[i].Name < snap.Payload[j].Name
	})

	// 面5：序列化 schema 键集合（seeded 结构 marshal 后取键树的第一层+嵌套键路径）。
	snap.Schemas["TaskState"] = schemaKeys(SeedTaskStateForSchema())
	snap.Schemas["Entry"] = schemaKeys(checklog.SeedEntryForSchema())
	snap.Schemas["ToolCall"] = schemaKeys(SeedToolCallForSchema())

	// 面6：阻断位点（源扫描，确定性）。
	blockings, err := scanBlockings(root)
	if err != nil {
		return nil, err
	}
	snap.Blockings = blockings

	// 面7：外部桥契约（plugins/*/contract.json；缺失容错为空面——与 payload 面
	// 同款，但文件存在而损坏时 fail loud）。
	bridges, err := scanBridges(root)
	if err != nil {
		return nil, err
	}
	snap.Bridges = bridges
	return snap, nil
}

// Marshal 确定性序列化（json.Marshal 对 struct 字段序确定 + 上面全部 slice
// 已排序 + 无 map 直接出现在序列化面——Schemas 是 map[string][]string，键序
// 由 encoding/json 排序保证）。
func (s *Snapshot) Marshal() ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}

// Diff 对比两份快照，产出变更清单（surface / kind / item / 破坏性判级）。
type Change struct {
	Surface  string `json:"surface"` // commands|checks|escapes|payload|schemas|blockings
	Kind     string `json:"kind"`    // removed|added|changed
	Item     string `json:"item"`
	Breaking bool   `json:"breaking"`
}

// Diff computes changes between base (older) and cur (current).
//
// Diff 计算基线（旧）与当前（新）之间的变更。判级规则（承诺表 §一的对下面）：
// commands/checks/escapes/payload/schemas 的 removed|changed → Breaking；
// added → 非 Breaking（但输出提示同步 README/承诺表）；blockings 增加非 Breaking
// （新 BLOCKED 位点按文案契约需附预告版本——warn 级提示）。
func Diff(base, cur *Snapshot) []Change {
	var out []Change
	// commands
	baseCmd := map[string]string{} // path → flags 签名
	for _, c := range base.Commands {
		baseCmd[c.Path] = strings.Join(c.Flags, ",")
	}
	curCmd := map[string]string{}
	for _, c := range cur.Commands {
		curCmd[c.Path] = strings.Join(c.Flags, ",")
	}
	for p, sig := range baseCmd {
		if cs, ok := curCmd[p]; !ok {
			out = append(out, Change{Surface: "commands", Kind: "removed", Item: p, Breaking: true})
		} else if cs != sig {
			out = append(out, Change{Surface: "commands", Kind: "changed", Item: p + "（flags 变更）", Breaking: true})
		}
	}
	for p := range curCmd {
		if _, ok := baseCmd[p]; !ok {
			out = append(out, Change{Surface: "commands", Kind: "added", Item: p})
		}
	}
	// 字符串集合面：checks / escapes / payload（name 级）
	setDiff := func(surface string, baseItems, curItems []string, identity func(string) string) {
		bm, cm := map[string]string{}, map[string]string{}
		for _, it := range baseItems {
			bm[identity(it)] = it
		}
		for _, it := range curItems {
			cm[identity(it)] = it
		}
		for k := range bm {
			if _, ok := cm[k]; !ok {
				out = append(out, Change{Surface: surface, Kind: "removed", Item: k, Breaking: surface != "blockings"})
			}
		}
		for k := range cm {
			if _, ok := bm[k]; !ok {
				out = append(out, Change{Surface: surface, Kind: "added", Item: k})
			}
		}
	}
	setDiff("checks", base.Checks, cur.Checks, func(s string) string { return s })
	setDiff("escapes", base.Escapes, cur.Escapes, func(s string) string { return s })
	baseP, curP := []string{}, []string{}
	for _, p := range base.Payload {
		baseP = append(baseP, p.Tree+"/"+p.Name+"@"+p.Hash)
	}
	for _, p := range cur.Payload {
		curP = append(curP, p.Tree+"/"+p.Name+"@"+p.Hash)
	}
	setDiff("payload", baseP, curP, func(s string) string {
		// 内容 hash 变化是 changed 非 removed：按 name 身份比。
		if i := strings.IndexByte(s, '@'); i > 0 {
			return s[:i]
		}
		return s
	})
	// payload 的 hash 变化单列 changed：
	basePH := map[string]string{}
	for _, p := range base.Payload {
		basePH[p.Tree+"/"+p.Name] = p.Hash
	}
	for _, p := range cur.Payload {
		if bh, ok := basePH[p.Tree+"/"+p.Name]; ok && bh != p.Hash {
			out = append(out, Change{Surface: "payload", Kind: "changed", Item: p.Tree + "/" + p.Name + "（SKILL.md 内容变更）", Breaking: false})
		}
	}
	// schemas
	for key, baseKeys := range base.Schemas {
		curKeys, ok := cur.Schemas[key]
		if !ok {
			out = append(out, Change{Surface: "schemas", Kind: "removed", Item: key + "（结构整体移除）", Breaking: true})
			continue
		}
		bm := map[string]bool{}
		for _, k := range baseKeys {
			bm[k] = true
		}
		cm := map[string]bool{}
		for _, k := range curKeys {
			cm[k] = true
		}
		for k := range bm {
			if !cm[k] {
				out = append(out, Change{Surface: "schemas", Kind: "removed", Item: key + "." + k, Breaking: true})
			}
		}
		for k := range cm {
			if !bm[k] {
				out = append(out, Change{Surface: "schemas", Kind: "added", Item: key + "." + k})
			}
		}
	}
	for key := range cur.Schemas {
		if _, ok := base.Schemas[key]; !ok {
			out = append(out, Change{Surface: "schemas", Kind: "added", Item: key + "（新结构）"})
		}
	}
	// blockings：共有文件计数增长 + cur 新增文件（新增 BLOCKED 位点的最常见
	// 形态——对抗审查 should-fix：原实现漏掉新文件，恰是该面的存在目的）。
	baseB := map[string]int{}
	for _, b := range base.Blockings {
		baseB[b.File] = b.Count
	}
	for _, b := range cur.Blockings {
		if bc, ok := baseB[b.File]; ok {
			if b.Count > bc {
				out = append(out, Change{Surface: "blockings", Kind: "changed", Item: fmt.Sprintf("%s（阻断位点 %d→%d——按文案契约须附预告版本或首发声明）", b.File, bc, b.Count)})
			}
		} else {
			// 基线里没有该文件=新增阻断文件（对抗审查 should-fix：新 BLOCKED
			// 位点的最常见形态，原实现完全漏掉）。
			out = append(out, Change{Surface: "blockings", Kind: "changed", Item: fmt.Sprintf("%s（新文件含 %d 个阻断位点——按文案契约须附预告版本或首发声明）", b.File, b.Count)})
		}
	}
	// bridges：按桥名定位，行按 Dsh 事件定位。映射 removed / forge 事件或
	// decision 形状 changed → Breaking（桥两端任一端单独升级即断）；failOpen
	// 承诺 removed/changed → Breaking（执法承诺，收紧方向须走预告）；一切
	// added → 非 Breaking。Note 是注释非契约，不参与 diff。
	baseBr := map[string]BridgeContract{}
	for _, b := range base.Bridges {
		baseBr[b.Bridge] = b
	}
	for _, cb := range cur.Bridges {
		bb, ok := baseBr[cb.Bridge]
		if !ok {
			out = append(out, Change{Surface: "bridges", Kind: "added", Item: cb.Bridge})
			continue
		}
		baseEv := map[string]BridgeEvent{}
		for _, e := range bb.Events {
			baseEv[e.Dsh] = e
		}
		curEv := map[string]BridgeEvent{}
		for _, e := range cb.Events {
			curEv[e.Dsh] = e
		}
		for dsh, be := range baseEv {
			ce, ok := curEv[dsh]
			if !ok {
				out = append(out, Change{Surface: "bridges", Kind: "removed", Item: cb.Bridge + "/" + dsh, Breaking: true})
			} else if ce.Forge != be.Forge || ce.Decision != be.Decision {
				out = append(out, Change{Surface: "bridges", Kind: "changed", Item: cb.Bridge + "/" + dsh + "（forge 事件或 decision 形状变更）", Breaking: true})
			}
		}
		for dsh := range curEv {
			if _, ok := baseEv[dsh]; !ok {
				out = append(out, Change{Surface: "bridges", Kind: "added", Item: cb.Bridge + "/" + dsh})
			}
		}
		baseFo, curFo := map[string]bool{}, map[string]bool{}
		for _, s := range bb.FailOpen {
			baseFo[s] = true
		}
		for _, s := range cb.FailOpen {
			curFo[s] = true
		}
		for s := range baseFo {
			if !curFo[s] {
				out = append(out, Change{Surface: "bridges", Kind: "removed", Item: cb.Bridge + "/failOpen", Breaking: true})
			}
		}
		added := 0
		for s := range curFo {
			if !baseFo[s] {
				added++
			}
		}
		if added > 0 {
			out = append(out, Change{Surface: "bridges", Kind: "added", Item: cb.Bridge + "/failOpen"})
		}
	}
	curBr := map[string]bool{}
	for _, cb := range cur.Bridges {
		curBr[cb.Bridge] = true
	}
	for _, bb := range base.Bridges {
		if !curBr[bb.Bridge] {
			out = append(out, Change{Surface: "bridges", Kind: "removed", Item: bb.Bridge, Breaking: true})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Surface != out[j].Surface {
			return out[i].Surface < out[j].Surface
		}
		return out[i].Item < out[j].Item
	})
	return out
}

// BreakingCount 数破坏性变更数。
func BreakingCount(changes []Change) int {
	n := 0
	for _, c := range changes {
		if c.Breaking {
			n++
		}
	}
	return n
}

// schemaKeys 取 marshal 后 JSON 对象的递归键路径（数组元素展开一层）。
func schemaKeys(v any) []string {
	body, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m any
	if json.Unmarshal(body, &m) != nil {
		return nil
	}
	var keys []string
	var walk func(node any, prefix string)
	walk = func(node any, prefix string) {
		switch t := node.(type) {
		case map[string]any:
			mk := make([]string, 0, len(t))
			for k := range t {
				mk = append(mk, k)
			}
			sort.Strings(mk)
			for _, k := range mk {
				p := k
				if prefix != "" {
					p = prefix + "." + k
				}
				keys = append(keys, p)
				walk(t[k], p)
			}
		case []any:
			for _, el := range t {
				walk(el, prefix+"[]")
			}
		}
	}
	walk(m, "")
	sort.Strings(keys)
	return keys
}
