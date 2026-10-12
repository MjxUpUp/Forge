package taskpipeline

// designtier.go —— 方案分档路由（design-stage-hardening W4 + W0.1，提案 D3/D4）。
//
// designtier.go — design-tier routing: deterministic hard triggers decide the
// minimum tier a change needs; the agent declares a tier; final = max of the two
// unless a reasoned downgrade. Pre-implement the rules run on PlanScope / design
// touch points, at complete on the actual diff (post-hoc escalation).
//
// 业界依据（调研 2026-10-12）：判断"该写多重的方案"靠规则清单 + agent 建议 + 人否决
// + 事后用 diff 兜底（OpenSpec 决策树、Rust RFC、ITIL 标准变更、Meta Diff Risk
// Score）。判的是风险（可回退性/影响面），不是代码行数。全程 advisory。

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/protocol"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// CheckNameDesignTier is the checklog name of the design-tier decision row.
//
// CheckNameDesignTier 是方案档位判定行的 checklog 名。
const CheckNameDesignTier checklog.CheckName = "design-tier"

// defaultTierRules is the built-in hard-trigger table (single source; protocol.yml
// design_tiers appends or overrides by Name).
//
// defaultTierRules 是内置硬触发器表（单一真相源；protocol.yml 的 design_tiers
// 按 Name 追加或覆盖）。
var defaultTierRules = []protocol.DesignTierRule{
	{Name: "schema-migration", Tier: tasktypes.DesignTierFull, Paths: []string{"seg:migrations", "seg:migration", "*.sql", "schema.prisma"}},
	{Name: "api-contract", Tier: tasktypes.DesignTierFull, Paths: []string{"*.proto", "openapi*.yaml", "openapi*.yml", "openapi*.json", "swagger*.json", "swagger*.yaml"}},
	{Name: "auth", Tier: tasktypes.DesignTierFull, Paths: []string{"seg:auth", "seg:authn", "seg:authz", "seg:oauth", "seg:rbac", "seg:permissions"}},
	// 只认仓库顶层的 api/ pkg/ sdk/：internal/api、web/src/api 不是对外公共接口（审查轮）。
	{Name: "public-api", Tier: tasktypes.DesignTierFull, Paths: []string{"api/", "pkg/", "sdk/"}},
	{Name: "dependency", Tier: tasktypes.DesignTierMini, Paths: []string{"go.mod", "package.json", "Cargo.toml", "pyproject.toml", "requirements.txt"}},
}

// defaultStandardChanges are pre-approved routine changes (ITIL "standard change"):
// files matching them never trigger a tier.
//
// defaultStandardChanges 是事先认可的例行改动（ITIL「标准变更」）：命中的文件永不
// 触发档位。
var defaultStandardChanges = []string{"*.md", "*.txt", "*.rst", "LICENSE*", "CHANGELOG*", ".gitignore", "go.sum", "package-lock.json", "*.lock"}

// crossModuleThreshold: a change spanning this many distinct directories is a
// multi-module change (mini) even without any hard trigger.
//
// crossModuleThreshold：改动跨越这么多个不同目录即视为跨模块改动（mini），即便
// 没命中任何硬触发器。
const crossModuleThreshold = 3

// tierPatternMatch applies one rule pattern to a repo-relative slash path.
//
// tierPatternMatch 对一条仓库相对斜杠路径应用一个规则模式。
func tierPatternMatch(pattern, file string) bool {
	file = filepath.ToSlash(file)
	if seg, ok := strings.CutPrefix(pattern, "seg:"); ok {
		parts := strings.Split(file, "/")
		for _, p := range parts[:len(parts)-1] {
			if p == seg {
				return true
			}
		}
		return false
	}
	if strings.Contains(pattern, "/") {
		return scopeMatchOne(pattern, file)
	}
	ok, err := path.Match(pattern, path.Base(file))
	return err == nil && ok
}

// tierConfig is the effective rule table.
type tierConfig struct {
	rules    []protocol.DesignTierRule
	standard []string
}

// loadTierConfig merges the built-in table with protocol.yml design_tiers (a
// missing/unreadable protocol means built-ins only — fail-open).
//
// loadTierConfig 合并内置表与 protocol.yml 的 design_tiers（protocol 缺失/不可读
// 即只用内置表——fail-open）。
func loadTierConfig(root string) tierConfig {
	cfg := tierConfig{
		rules:    append([]protocol.DesignTierRule(nil), defaultTierRules...),
		standard: append([]string(nil), defaultStandardChanges...),
	}
	proto, err := protocol.Load(root)
	if err != nil || proto == nil || proto.DesignTiers == nil {
		return cfg
	}
	o := proto.DesignTiers
	if o.ReplaceDefaults {
		cfg.rules, cfg.standard = nil, nil
	}
	for _, r := range o.Rules {
		if tasktypes.DesignTierRank(r.Tier) < 0 {
			fmt.Fprintf(os.Stderr, "warn: protocol.yml design_tiers 规则 %q 的 tier %q 非法（none/mini/full），已忽略\n", r.Name, r.Tier)
			continue
		}
		replaced := false
		for i := range cfg.rules {
			if cfg.rules[i].Name == r.Name {
				cfg.rules[i], replaced = r, true
				break
			}
		}
		if !replaced {
			cfg.rules = append(cfg.rules, r)
		}
	}
	cfg.standard = append(cfg.standard, o.StandardChanges...)
	return cfg
}

// deriveTier applies the rule table to files: the highest tier any non-standard
// file triggers, plus the cross-module signal. Triggers are "规则名: 文件".
//
// deriveTier 对文件集应用规则表：任一非标准变更文件触发的最高档位，外加跨模块信号。
// 触发记录形如「规则名: 文件」。
func deriveTier(files []string, cfg tierConfig) (string, []string) {
	tier := tasktypes.DesignTierNone
	var triggers []string
	dirs := map[string]bool{}
	for _, f := range files {
		f = filepath.ToSlash(f)
		standard := false
		for _, p := range cfg.standard {
			if tierPatternMatch(p, f) {
				standard = true
				break
			}
		}
		if standard {
			continue
		}
		dirs[path.Dir(f)] = true
		for _, r := range cfg.rules {
			for _, p := range r.Paths {
				if tierPatternMatch(p, f) {
					triggers = append(triggers, r.Name+": "+f)
					if tasktypes.DesignTierRank(r.Tier) > tasktypes.DesignTierRank(tier) {
						tier = r.Tier
					}
					break
				}
			}
		}
	}
	if len(dirs) >= crossModuleThreshold {
		triggers = append(triggers, fmt.Sprintf("cross-module: %d 个目录", len(dirs)))
		if tasktypes.DesignTierRank(tier) < tasktypes.DesignTierRank(tasktypes.DesignTierMini) {
			tier = tasktypes.DesignTierMini
		}
	}
	sort.Strings(triggers)
	return tier, triggers
}

// DeriveDesignTier is deriveTier with the project's effective rule table.
//
// DeriveDesignTier 用项目生效规则表推导档位。
func DeriveDesignTier(root string, files []string) (string, []string) {
	return deriveTier(files, loadTierConfig(root))
}

// DecideDesignTier combines the agent-declared tier with the derived one: final =
// max(declared, derived); a declared tier below the derived one stands only with
// a reason (recorded for audit).
//
// DecideDesignTier 合并 agent 声明档位与推导档位：最终 = 两者较高者；声明低于推导
// 时必须带理由才生效（理由落审计）。
func DecideDesignTier(declared, reason, derived string, triggers []string) tasktypes.DesignTierDecision {
	d := tasktypes.DesignTierDecision{Declared: declared, Derived: derived, Triggers: triggers, Final: derived}
	if declared == "" {
		return d
	}
	if tasktypes.DesignTierRank(declared) >= tasktypes.DesignTierRank(derived) {
		d.Final = declared
		return d
	}
	if strings.TrimSpace(reason) != "" {
		d.Final, d.Reason = declared, reason
	}
	return d
}

// ValidateDesignTier rejects anything outside none/mini/full ("" = undeclared is fine).
//
// ValidateDesignTier 拒绝 none/mini/full 以外的值（"" = 未声明，合法）。
func ValidateDesignTier(tier string) error {
	if tier != "" && tasktypes.DesignTierRank(tier) < 0 {
		return fmt.Errorf("--tier 只接受 none（直接改）/ mini（小方案）/ full（完整方案），got %q", tier)
	}
	return nil
}

// RecordDesignTier lands the design-tier decision row (phase = start | implement |
// complete; escalated marks a post-hoc escalation).
//
// RecordDesignTier 落方案档位判定行（phase = start | implement | complete；
// escalated 标记事后升档）。
func RecordDesignTier(root, taskRef, phase string, d tasktypes.DesignTierDecision, escalated bool) {
	meta := map[string]string{
		"phase": phase, "declared": d.Declared, "derived": d.Derived, "final": d.Final,
	}
	if d.Reason != "" {
		meta["override.reason"] = d.Reason
	}
	if len(d.Triggers) > 0 {
		meta["triggers"] = strings.Join(d.Triggers, "; ")
	}
	e := &checklog.Entry{
		Check: CheckNameDesignTier, Passed: !escalated, Checked: true, TaskRef: taskRef,
		Level:  checklog.LevelPass,
		Detail: fmt.Sprintf("design tier %s (declared=%q derived=%q)", d.Final, d.Declared, d.Derived),
		Meta:   meta,
	}
	if escalated {
		meta["escalated"] = "true"
		e.Level = checklog.LevelAdvisory
		e.Detail = fmt.Sprintf("post-hoc escalation: diff demands %s, declared %s, no design artifact", d.Derived, d.Final)
	}
	recordAudit(root, e)
}

// tierDeliverable names what a tier asks the author to write.
func tierDeliverable(tier string) string {
	switch tier {
	case tasktypes.DesignTierFull:
		return "完整方案（需求/设计/任务三文件 + 备选与取舍 + 人工批准）"
	case tasktypes.DesignTierMini:
		return "小方案（单文件，skills/doc-generator/references/template-tech-plan.md）"
	}
	return "直接改（不写方案）"
}

// FormatDesignTierAdvice renders a one-line tier hint for stderr.
//
// FormatDesignTierAdvice 渲染一行档位提示（stderr）。
func FormatDesignTierAdvice(d tasktypes.DesignTierDecision) string {
	msg := fmt.Sprintf("方案档位：%s → %s", d.Final, tierDeliverable(d.Final))
	if len(d.Triggers) > 0 {
		shown := d.Triggers
		if len(shown) > 3 {
			shown = append(append([]string(nil), shown[:3]...), fmt.Sprintf("…+%d", len(d.Triggers)-3))
		}
		msg += "；触发：" + strings.Join(shown, "，")
	}
	if d.Declared != "" && d.Declared != d.Final {
		msg += fmt.Sprintf("（声明 %s 低于硬触发器要求，已取较高档；确需下调用 --tier-reason 写明理由）", d.Declared)
	}
	if d.Reason != "" {
		msg += fmt.Sprintf("（已按理由下调：%s）", d.Reason)
	}
	return msg
}

// goModVersionLineRe parses a require entry "module vX.Y.Z".
// Pre-release and pseudo-versions (v0.0.0-2024…) are excluded by the anchored
// tail: they point at arbitrary commits, not patch releases.
var goModVersionLineRe = regexp.MustCompile(`^\s*(?:require\s+)?([\w.\-/]+)\s+v(\d+)\.(\d+)\.\d+(?:\+incompatible)?\s*(?://.*)?$`)

// goModPatchOnly reports whether the go.mod diff since base only bumps patch
// versions of already-required modules (a standard change, not a new dependency).
// Undeterminable diffs return false (fail toward the safer, higher tier).
//
// goModPatchOnly 报告自 base 起 go.mod 的 diff 是否只升级既有依赖的 patch 版本
// （标准变更，不是新依赖）。不可判定时返回 false（往更安全的高档位失败）。
func goModPatchOnly(root, base string) bool {
	if base == "" {
		return false
	}
	diff := gitOut(root, "diff", base, "--", "go.mod")
	if diff == "" {
		return false
	}
	removed := map[string][2]string{}
	added := map[string][2]string{}
	for _, l := range strings.Split(diff, "\n") {
		if strings.HasPrefix(l, "---") || strings.HasPrefix(l, "+++") {
			continue
		}
		var into map[string][2]string
		switch {
		case strings.HasPrefix(l, "-"):
			into = removed
		case strings.HasPrefix(l, "+"):
			into = added
		default:
			continue
		}
		body := strings.TrimSpace(l[1:])
		if body == "" {
			continue
		}
		m := goModVersionLineRe.FindStringSubmatch(body)
		if m == nil {
			return false // 非版本行改动（新增 replace/go 指令等）——不是纯 patch 升级
		}
		into[m[1]] = [2]string{m[2], m[3]}
	}
	if len(added) == 0 || len(added) != len(removed) {
		return false
	}
	for mod, nv := range added {
		ov, ok := removed[mod]
		if !ok || ov != nv {
			return false
		}
	}
	return true
}

// filesForTierAtComplete drops go.mod when its diff is a patch-only bump.
func filesForTierAtComplete(root string, state *TaskState, changed []string) []string {
	out := make([]string, 0, len(changed))
	for _, f := range changed {
		if filepath.ToSlash(f) == "go.mod" && goModPatchOnly(root, state.HeadCommit) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// CheckDesignTierEscalation is the complete-time post-hoc check: the actual diff
// demands a higher tier than the task's final tier and no design artifact was
// registered → advisory + escalated row. Advisory only in v1.
//
// CheckDesignTierEscalation 是 complete 时的事后复核：实际 diff 要求的档位高于
// 任务最终档位且未登记 design 产物 → advisory + 升档行。v1 仅 advisory。
func CheckDesignTierEscalation(root string, state *TaskState, changed []string) {
	derived, triggers := DeriveDesignTier(root, filesForTierAtComplete(root, state, changed))
	final := tasktypes.DesignTierNone
	declared := ""
	if state.DesignTier != nil {
		final, declared = state.DesignTier.Final, state.DesignTier.Declared
	}
	_, hasDesign := state.SpecArtifacts["design"]
	if tasktypes.DesignTierRank(derived) <= tasktypes.DesignTierRank(final) || derived == tasktypes.DesignTierNone || hasDesign {
		return
	}
	d := tasktypes.DesignTierDecision{Declared: declared, Derived: derived, Final: final, Triggers: triggers}
	RecordDesignTier(root, state.TaskRef, "complete", d, true)
	fmt.Fprintf(os.Stderr, "%s\n", GateAdvisory("[task-complete] 实际改动要求%s，但任务档位为 %s 且未登记 design 产物（触发：%s）——补方案：forge task artifact --set design --file <path>",
		tierDeliverable(derived), final, strings.Join(triggers, "，")))
}

// RecordImplementDesignTier decides (if not yet decided) and records the tier at
// the first implement round, using PlanScope plus files changed so far. Called
// from the once-per-task plan-first block.
//
// RecordImplementDesignTier 在首个 implement 轮判定（若尚未判定）并记录档位，用
// PlanScope 加上已改文件。由每任务一次的 plan-first 段调用。
func RecordImplementDesignTier(root string, state *TaskState, changed []string) {
	files := append(append([]string(nil), state.PlanScope...), changed...)
	derived, triggers := DeriveDesignTier(root, files)
	var d tasktypes.DesignTierDecision
	if state.DesignTier != nil {
		d = DecideDesignTier(state.DesignTier.Declared, state.DesignTier.Reason, derived, triggers)
	} else {
		d = DecideDesignTier("", "", derived, triggers)
	}
	state.DesignTier = &d
	RecordDesignTier(root, state.TaskRef, "implement", d, false)
	_, hasDesign := state.SpecArtifacts["design"]
	switch {
	case d.Final != tasktypes.DesignTierNone && !hasDesign && state.Plan == "":
		fmt.Fprintf(os.Stderr, "%s\n", GateAdvisory("[task-implement] %s——尚无方案产物（forge task artifact --set design --file <path>）", FormatDesignTierAdvice(d)))
	case d.Final == tasktypes.DesignTierFull && hasDesign && state.ArtifactApprovals["design"].Hash == "":
		// D4 目标映射 full → design 阶段 human；本批只给 advisory（提案原则 2）。
		fmt.Fprintf(os.Stderr, "%s\n", GateAdvisory("[task-implement] 完整方案档：design 产物尚未人工批准——批准走 forge task artifact --approve design --by <审批人>（需产物链 design 设为 human 档；本批不阻断）"))
	}
}
