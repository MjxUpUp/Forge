package skillsdist

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/skillsfm"
	"github.com/MjxUpUp/Forge/internal/skillsqa"
	"github.com/MjxUpUp/Forge/internal/util"
)

// Distribution mode.
//
// 分发模式。
type Mode string

const (
	// Target = junction/symlink pointing to canonical (default, single source).
	ModeLink Mode = "link" // 目标 = 指向 canonical 的 junction/symlink（默认，单源）
	// Target = standalone copy of canonical.
	ModeCopy Mode = "copy" // 目标 = canonical 的独立副本
)

// DriftPolicy is the handling strategy for drift (target diverging from canonical content).
//
// DriftPolicy 对 drift（目标与 canonical 内容分叉）态的处理策略。
type DriftPolicy string

const (
	// Return an error immediately on drift (default, CI-friendly).
	DriftAbort     DriftPolicy = "abort"     // 遇 drift 立即返回错误（默认，CI 友好）
	// Skip the drifting skill.
	DriftSkip      DriftPolicy = "skip"      // 跳过 drift 的 skill
	// Force-overwrite with canonical.
	DriftOverwrite DriftPolicy = "overwrite" // 强制以 canonical 覆盖
)

// Distribution target tool.
//
// 分发目标工具。
type Target string

const (
	TargetClaude  Target = "claude"
	TargetCursor  Target = "cursor"
	// OpenAI Codex CLI (~/.codex/skills, native SKILL.md support since 2025-12).
	TargetCodex   Target = "codex"   // OpenAI Codex CLI（~/.codex/skills，2025-12 起 SKILL.md 原生支持）
	// GitHub Copilot (~/.copilot/skills, cross-project personal skill).
	TargetCopilot Target = "copilot" // GitHub Copilot（~/.copilot/skills，跨项目个人 skill）
	TargetAll     Target = "all"
)

// Distribution states (aligned with sync.py's four states).
//
// 分发态（对齐 sync.py 四态）。
const (
	// Target is already a link pointing to canonical.
	StateLinked     = "linked"       // 目标已是指向 canonical 的 link
	// Target is a copy whose content matches canonical.
	StateCopyInSync = "copy-in-sync" // 目标是副本且内容与 canonical 一致
	// Target exists but content diverges from canonical.
	StateDrift      = "drift"        // 目标存在但内容与 canonical 分叉
	// Target does not exist.
	StateMissing    = "missing"      // 目标不存在
)

// reservedNames are skill names managed by forge's own skillgen; install must skip them —
// otherwise autoSync would overwrite user-installed versions with the self-generated one on every
// run, causing distribution churn.
//
// reservedNames 是 forge 自身 skillgen 管理的 skill 名，install 必须跳过——
// 否则 autoSync 每次会用自生成版覆盖用户装的，造成分发抖动。
var reservedNames = map[string]bool{
	"forge-quality": true,
}

// InstallOpts is the input to Install.
//
// InstallOpts 是 Install 的输入。
type InstallOpts struct {
	Mode             Mode
	DriftPolicy      DriftPolicy
	Targets          []Target
	// Install only the named skills (empty = all).
	SkillFilter      []string // 只装指定 skill（空=全部）
	// Skip the registry+audit dual gate before install.
	SkipQuality      bool     // 跳过 install 前的 registry+audit 双门控
	// Skip the frontmatter.requires dependency co-install check (escape hatch).
	SkipRequireCheck bool     // 跳过 frontmatter.requires 依赖同装检查（逃生舱）
	// true → ~/.claude/skills etc.; false → ProjectSkillsDir.
	Global           bool     // true→~/.claude/skills 等；false→ProjectSkillsDir
	// Used when Global=false (project .claude/skills).
	ProjectSkillsDir string   // Global=false 时用（项目 .claude/skills）
	// Backup root for overwrite (test injection; empty in production → auto ~/.forge/skills-backup/<ts>).
	BackupBase       string   // overwrite 备份根目录（测试注入；生产留空→自动 ~/.forge/skills-backup/<ts>）
}

// InstallReport is the full result of one Install.
//
// InstallReport 是一次 Install 的完整结果。
type InstallReport struct {
	Mode      Mode                 `json:"mode"`
	Canonical string               `json:"canonical"`
	Skills    []SkillInstallResult `json:"skills"`
	Stats     InstallStats         `json:"stats"`
	Aborted   string               `json:"aborted,omitempty"`
	// Non-blocking warnings such as unmet requires dependencies.
	Warnings  []string             `json:"warnings,omitempty"` // requires 依赖未满足等非阻断警告
}

// SkillInstallResult is the install result of a single skill across all targets.
//
// SkillInstallResult 是单个 skill 在所有目标的安装结果。
type SkillInstallResult struct {
	Name    string         `json:"name"`
	Pass    bool           `json:"pass"`
	Issues  []string       `json:"issues,omitempty"`
	Targets []TargetResult `json:"targets"`
}

// TargetResult is the install result of a single skill at a single target.
//
// TargetResult 是单 skill 在单目标的安装结果。
type TargetResult struct {
	Target string `json:"target"`
	Dir    string `json:"dir"`
	State  string `json:"state"`
	Action string `json:"action"`
	Detail string `json:"detail"`
	// Backup path before overwrite (empty = not backed up: link / broken link / non-overwrite).
	Backup string `json:"backup,omitempty"` // overwrite 前的备份路径（空=未备份：link/断链/非 overwrite）
}

// InstallStats is the statistical summary of an install.
//
// InstallStats 是 install 统计摘要。
type InstallStats struct {
	Total     int `json:"total"`
	Installed int `json:"installed"`
	Skipped   int `json:"skipped"`
	Drifted   int `json:"drifted"`
	Failed    int `json:"failed"`
}

// distSkipDirs is synced with skillsqa: directory segments skipped by copyTree.
//
// skipDirs 与 skillsqa 同步：copyTree 跳过的目录段。
var distSkipDirs = map[string]bool{
	"node_modules": true, ".git": true, "__pycache__": true, ".venv": true,
}

// Install syncs skills under canonical to each target directory.
//
// Install 把 canonical 下的 skill 同步到各目标目录。
func Install(canonical string, opts InstallOpts) (*InstallReport, error) {
	report := &InstallReport{Mode: opts.Mode, Canonical: canonical}

	// overwrite backup root: opts.BackupBase (test injection) or ~/.forge/skills-backup/<ts> (production).
	// One install shares one ts subdirectory for "what was overwritten this time" traceability and recovery.
	//
	// overwrite 备份根目录：opts.BackupBase（测试注入）或 ~/.forge/skills-backup/<ts>（生产）。
	// 一次 install 共享一个 ts 子目录，便于"这次覆盖了什么"回溯与恢复。
	backupBase := opts.BackupBase
	if backupBase == "" {
		home, herr := os.UserHomeDir()
		if herr != nil {
			// home unavailable (no USERPROFILE/HOME, container/CI scenarios) → backup cannot land on disk.
			// Warn explicitly rather than silently disabling: users choosing overwrite expect a rollback path;
			// silently giving up violates the design intent (anti error-swallow).
			//
			// home 拿不到（无 USERPROFILE/HOME，容器/CI 场景）→ 备份无法落盘。
			// 显式告警而非静默禁用：用户选 overwrite 期待有后悔药，静默放弃违背设计意图（防 error-swallow）。
			fmt.Fprintf(os.Stderr, "warn: 无法定位家目录，overwrite 备份已禁用: %v\n", herr)
		} else {
			backupBase = filepath.Join(home, ".forge", "skills-backup", time.Now().Format("20060102-150405"))
		}
	}

	names, err := ListSkills(canonical)
	if err != nil {
		return nil, err
	}
	if len(opts.SkillFilter) > 0 {
		names = filterNames(names, opts.SkillFilter)
	}

	targetDirs, err := TargetDirs(opts.Targets, opts.Global, opts.ProjectSkillsDir)
	if err != nil {
		return nil, err
	}
	// Output target names in a fixed order (alphabetic claude<codex<copilot<cursor<pi>) for stable rendering.
	//
	// 目标名按固定顺序输出（字母序 claude<codex<copilot<cursor<pi），便于稳定渲染
	targetOrder := orderedTargetNames(targetDirs)

	for _, name := range names {
		if reservedNames[name] {
			report.Skills = append(report.Skills, SkillInstallResult{
				Name: name,
				Targets: []TargetResult{{
					Action: actReserved, Detail: "保留名（forge 自身管理），跳过",
				}},
			})
			report.Stats.Skipped++
			continue
		}

		skillDir := filepath.Join(canonical, name)
		res := SkillInstallResult{Name: name}

		// Quality gate: registry spec + audit security (aligned with sync.py apply-time dual gate)
		//
		// 质量门控：registry 规范 + audit 安全（对齐 sync.py apply 时双门控）
		if !opts.SkipQuality {
			rep, qerr := skillsqa.AuditSkill(skillDir)
			if qerr != nil {
				res.Issues = []string{"审查失败: " + qerr.Error()}
				res.Targets = []TargetResult{{Action: actBlocked, Detail: "SKILL.md 不可读或无 frontmatter"}}
				report.Stats.Failed++
				report.Skills = append(report.Skills, res)
				continue
			}
			res.Pass = rep.Pass
			res.Issues = rep.Issues
			if !rep.Pass {
				res.Targets = []TargetResult{{Action: actBlocked, Detail: "registry 规范门控未通过（R1-R11）"}}
				report.Stats.Failed++
				report.Skills = append(report.Skills, res)
				continue
			}
			findings, _ := skillsqa.ScanSkill(skillDir)
			_, _, rec := skillsqa.ScoreFindings(findings)
			if rec == "DO_NOT_INSTALL" {
				res.Issues = append(res.Issues, "安全门控: DO_NOT_INSTALL（score≥50，CRITICAL）")
				res.Targets = []TargetResult{{Action: actBlocked, Detail: "audit 安全门控 DO_NOT_INSTALL"}}
				report.Stats.Failed++
				report.Skills = append(report.Skills, res)
				continue
			}
		}

		report.Stats.Total++

		for _, tname := range targetOrder {
			tdir := targetDirs[tname]
			dst := filepath.Join(tdir, name)
			state := detectState(skillDir, dst)
			tr := TargetResult{Target: tname, Dir: dst, State: state}

			// Pre-overwrite backup: a drift real-directory copy is the user's local customization,
			// back it up before overwriting (rollback path).
			// link/junction/broken-link has no independent user content; backupTarget auto-skips it
			// and returns an empty string.
			// Backup failure does not block overwrite (user explicitly chose overwrite); it only
			// leaves a trail on stderr.
			//
			// overwrite 前备份：drift 的真目录副本是用户本地定制，覆盖前留底（后悔药）。
			// link/junction/断链无独立用户内容，backupTarget 自动跳过返回空串。
			// 备份失败不阻断 overwrite（用户已明确选 overwrite），仅 stderr 留痕。
			if state == StateDrift && opts.DriftPolicy == DriftOverwrite && backupBase != "" {
				if bk, berr := backupTarget(dst, backupBase, tname, name); berr != nil {
					fmt.Fprintf(os.Stderr, "warn: 备份失败 %s（继续 overwrite）: %v\n", dst, berr)
				} else {
					tr.Backup = bk
				}
			}

			action, detail, abortErr := handleTarget(skillDir, dst, state, opts.Mode, opts.DriftPolicy)
			tr.Action = action
			tr.Detail = detail
			if abortErr != nil {
				report.Aborted = fmt.Sprintf("skill %q target %q: %v", name, tname, abortErr)
				tr.Action = actAborted
				res.Targets = append(res.Targets, tr)
				report.Stats.Drifted++
				break
			}
			switch action {
			case actLinked, actCopied:
				report.Stats.Installed++
			case actSkipped:
				report.Stats.Skipped++
			}
			res.Targets = append(res.Targets, tr)
		}
		report.Skills = append(report.Skills, res)

		if report.Aborted != "" {
			return report, fmt.Errorf("%s", report.Aborted)
		}
	}

	// requires dependency check: for skills successfully installed this run, read frontmatter.requires
	// and check whether declared dependencies are in the canonical superset (declared valid) and
	// co-installed this run. Failures are recorded in Warnings (advisory, non-blocking).
	// Resolves the existing defect that the requires field has no consumer — installing a single
	// dependency skill breaks the chain; this surfaces it explicitly.
	//
	// requires 依赖检查：对本次成功装的 skill 读 frontmatter.requires，检查声明的依赖
	// 是否在 canonical 全集（声明有效）且本次同装。不满足记入 Warnings（仅提示，不阻断）。
	// 解除 requires 字段无消费方的既有缺陷——单装依赖 skill 会断链，此处显式提示。
	if !opts.SkipRequireCheck {
		report.Warnings = checkRequires(canonical, report.Skills)
	}
	return report, nil
}

// parseRequires splits the frontmatter.requires field. requires is a single string, conventionally
// comma-separated for multiple dependencies (e.g. code-review-gate, doc-generator). Whitespace is
// auto-trimmed and empty segments filtered out.
//
// ⚠ Comma separation is fragile: skill names must not contain commas (otherwise ambiguous with the
// separator). Medium-term, skillsfm.Parse could also recognize the YAML list form requires: [a, b],
// and parseRequires would unify internally.
//
// parseRequires 拆解 frontmatter.requires 字段。requires 是单 string，约定逗号分隔多个依赖
// （如 code-review-gate, doc-generator）。空白自动 trim，空段过滤。
//
// ⚠ 逗号分隔脆弱：skill 名禁用逗号（否则与分隔符歧义）。中长期可让 skillsfm.Parse 同时
// 识别 YAML 列表形式 requires: [a, b]，parseRequires 内部统一展开。
func parseRequires(s string) []string {
	parts := strings.Split(s, string(','))
	var out []string
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// install action constants (consistent with the action literals returned/set by handleTarget and
// the Install main loop; backtick raw string avoids Windows input quote corrosion).
//
// The closed success-action set (counts as installed to target) = {linked, copied, skipped} —
// the okActions whitelist in checkRequires. **Any new success action must be synced**: (1) add an
// actXxx constant (2) add it to okActions (3) add a TestCheckRequires case to guard it, to avoid
// silently extending the set and breaking checkRequires semantics.
//
// Non-success actions: blocked (quality gate failed) / aborted (drift-policy=abort triggered) /
// reserved (forge-quality reserved name) — not in okActions, naturally skipped by checkRequires.
//
// install action 常量（与 handleTarget 及 Install 主循环返回/设置的 action 字面量一致；
// 反引号 raw string 规避 Windows 输入引号腐蚀）。
//
// 闭合的成功 action 集（"算装到目标"）= {linked, copied, skipped}——checkRequires 的
// okActions 白名单。**任何新增成功 action 必须同步**：(1) 加 actXxx 常量 (2) 加进
// okActions (3) TestCheckRequires 加用例守护，避免静默扩展破坏 checkRequires 语义。
//
// 非成功 action：blocked（质量门控未过）/ aborted（drift-policy=abort 触发）/ reserved
// （forge-quality 保留名）——不在 okActions，checkRequires 自然跳过。
const (
	actLinked   = `linked`
	actCopied   = `copied`
	actSkipped  = `skipped`
	actBlocked  = `blocked`
	actAborted  = `aborted`
	actDrifted  = `drifted`
	actReserved = `reserved`
)

// checkRequires verifies that the frontmatter.requires dependencies of each successfully installed
// skill in this run are satisfied. A dependency is satisfied when (1) it is in the canonical
// superset (declared valid, not a typo) and (2) it is co-installed this run.
// Two kinds of failure each raise a warning:
//   - Dependency not in canonical → the requires declaration is invalid (typo or target skill removed).
//   - Dependency in canonical but not co-installed this run → single-install break risk
//     (--skill filtered out the dependency); co-install recommended.
//
// installedSet is based on the action that successfully landed on a target (linked/copied/skipped),
// not on SkillInstallResult.Pass — the latter is not set to true on the --skip-quality path, so
// using Pass would miss successful installs that skipped the quality gate. blocked/aborted/reserved
// actions are not in the whitelist and are naturally excluded.
//
// Error handling: canonical unreadable (ListSkills error) → return a system-level warning rather
// than silently dropping; a single SKILL.md unreadable → that skill is skipped but a per-skill
// warning is added (avoiding silent corruption that is hard to diagnose).
//
// Only returns warnings, never blocks install (requires is a declarative field; a missing
// dependency should not override the user's explicit single-install intent).
//
// checkRequires 检查本次安装集里每个成功装的 skill 的 frontmatter.requires 依赖是否满足。
// 依赖满足 = (1) 在 canonical 全集（声明有效，非笔误）且 (2) 本次同装。
// 两类不满足分别告警：
//   - 依赖不在 canonical → requires 声明无效（笔误或目标 skill 已移除）
//   - 依赖在 canonical 但本次未同装 → 单装断链风险（--skill 过滤掉依赖），建议同装
//
// installedSet 基于"成功落到目标"的 action（linked/copied/skipped）而非
// SkillInstallResult.Pass——后者在 --skip-quality 路径下不设 true，用 Pass 会漏掉
// 跳过质量门控的成功安装。blocked/aborted/reserved 的 action 不在白名单，自然排除。
//
// 错误处理：canonical 不可读（ListSkills 错）→ 返回系统级警告而非静默丢；单个 SKILL.md
// 不可读 → 该 skill 跳过但加 per-skill 警告（避免静默损坏排查困难）。
//
// 仅返回警告，不阻断 install（requires 是声明性字段，缺失依赖不应违背用户显式单装的意图）。
func checkRequires(canonical string, results []SkillInstallResult) []string {
	var warns []string
	allNames, err := ListSkills(canonical)
	if err != nil {
		warns = append(warns, fmt.Sprintf(`%s: requires 检查跳过（canonical 不可读: %v）`, canonical, err))
		return warns
	}
	allSet := make(map[string]bool, len(allNames))
	for _, n := range allNames {
		allSet[n] = true
	}
	// okActions whitelist = closed set of success actions (see the constant-block comment); new
	// actions must extend it in sync.
	//
	// okActions 白名单 = 成功 action 闭合集（见常量块注释）；新增 action 必须同步扩展。
	okActions := map[string]bool{actLinked: true, actCopied: true, actSkipped: true}
	installedSet := make(map[string]bool, len(results))
	for _, r := range results {
		for _, t := range r.Targets {
			if okActions[t.Action] {
				installedSet[r.Name] = true
				break
			}
		}
	}
	for _, r := range results {
		if !installedSet[r.Name] {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(canonical, r.Name, "SKILL.md"))
		if rerr != nil {
			warns = append(warns, fmt.Sprintf(`%s: requires 检查跳过（SKILL.md 不可读: %v）`, r.Name, rerr))
			continue
		}
		fm := skillsfm.Parse(data)
		for _, dep := range parseRequires(fm.Requires) {
			if !allSet[dep] {
				warns = append(warns, fmt.Sprintf(`%s: requires %s 不在 canonical（requires 声明无效，可能笔误或目标 skill 已移除）`, r.Name, dep))
			} else if !installedSet[dep] {
				warns = append(warns, fmt.Sprintf(`%s: requires %s 但本次未同装（跨 skill 引用可能断链；用 --skill 含两者或省略 --skill 全装）`, r.Name, dep))
			}
		}
	}
	return warns
}

// handleTarget decides the action for a single target based on the current state + mode + policy.
// Returns (action, detail, abortErr); abortErr != nil means drift-policy=abort triggered and the
// caller should abort the entire install.
//
// handleTarget 按当前 state + mode + policy 决定对单个目标的动作。
// 返回 (action, detail, abortErr)；abortErr != nil 表示 drift-policy=abort 触发，调用方应中止整个 install。
func handleTarget(src, dst, state string, mode Mode, policy DriftPolicy) (string, string, error) {
	switch state {
	case StateLinked:
		return actSkipped, "已是 link（内容同步）", nil
	case StateCopyInSync:
		if mode == ModeLink {
			// copy-in-sync → safely replace with link (delete copy, create link)
			//
			// copy-in-sync → 安全替换为 link（删副本建 link）
			if err := os.RemoveAll(dst); err != nil {
				return "", "", fmt.Errorf("remove copy %s: %w", dst, err)
			}
			if err := makeDirLink(dst, src); err != nil {
				return "", "", fmt.Errorf("link %s: %w", dst, err)
			}
			return actLinked, "copy 安全替换为 link", nil
		}
		return actSkipped, "已是 copy（内容同步）", nil
	case StateMissing:
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return "", "", err
		}
		if mode == ModeLink {
			if err := makeDirLink(dst, src); err != nil {
				return "", "", fmt.Errorf("link %s: %w", dst, err)
			}
			return actLinked, "新建 link", nil
		}
		if err := copyTree(src, dst); err != nil {
			return "", "", fmt.Errorf("copy %s: %w", dst, err)
		}
		return actCopied, "新建 copy", nil
	case StateDrift:
		switch policy {
		case DriftSkip:
			return actSkipped, "drift（策略 skip，保留现状）", nil
		case DriftOverwrite:
			removeTargetTree(dst)
			if mode == ModeLink {
				if err := makeDirLink(dst, src); err != nil {
					return "", "", fmt.Errorf("link %s: %w", dst, err)
				}
				return actLinked, "drift 强制以 canonical 建 link", nil
			}
			if err := copyTree(src, dst); err != nil {
				return "", "", fmt.Errorf("copy %s: %w", dst, err)
			}
			return actCopied, "drift 强制以 canonical 覆盖 copy", nil
		default: // abort
			return actDrifted, "drift（策略 abort）", fmt.Errorf("drift detected at %s，用 --drift-policy skip|overwrite 处理", dst)
		}
	}
	return "", "", fmt.Errorf("未知 state: %s", state)
}

// backupTarget backs up the dst about to be overwritten to backupBase/<target>/<skill>.
// Only real-directory copies are backed up (the user's local customizations); link/junction has no
// independent content, broken-link/nonexistent/non-directory has no content — all are skipped and
// return an empty string. Uses copyTree (consistent with install, skips .git/node_modules).
// Returns an error on failure; the caller decides whether to continue overwriting.
//
// backupTarget 把即将被 overwrite 的 dst 备份到 backupBase/<target>/<skill>。
// 只备份真目录副本（用户的本地定制）；link/junction 无独立内容、断链/不存在/非目录无内容，
// 一律跳过返回空串。用 copyTree（与 install 一致，跳过 .git/node_modules）。
// 失败返回 error，调用方决定是否继续 overwrite。
func backupTarget(dst, backupBase, target, skill string) (string, error) {
	if backupBase == "" {
		return "", nil
	}
	// link/junction: reparse point has no independent user content (points elsewhere or is broken);
	// backup is meaningless.
	//
	// link/junction：reparse point 无独立用户内容（指向别处或断链），备份无意义。
	if isJunctionOrLink(dst) {
		return "", nil
	}
	info, err := os.Stat(dst)
	if err != nil || !info.IsDir() {
		return "", nil // 断链 os.Stat 失败；单文件（非目录）也不备份
	}
	bkDir := filepath.Join(backupBase, target, skill)
	// Path-injection defense: target/skill must be a canonical basename (not ./.., no separators).
	// Canonical skill names are inherently safe under filesystem constraints; this is a defensive
	// fallback — to guard against future canonical sources containing malicious names that write
	// beyond backupBase.
	//
	// 路径注入防御：target/skill 须为规范 basename（非 ./..、无分隔符）。
	// canonical skill 名受文件系统约束天然安全，此处防御性兜底——防未来 canonical 来源
	// 含恶意名导致 backupBase 越界写。
	if !isSafeName(target) || !isSafeName(skill) {
		return "", fmt.Errorf("非法 target/skill 名（路径注入风险）: %q/%q", target, skill)
	}
	// Clear any prior residue before copying, so the backup is a clean point-in-time snapshot —
	// otherwise on directory reuse, files that existed last time but were deleted this time would
	// remain and pollute the rollback result (aligned with handleTarget overwrite's remove-then-copy).
	//
	// 先清空可能的上次残留再 copy，保证备份是"那一刻的纯净快照"——否则同目录复用时
	// 上次有、这次删的文件会残留，污染回滚结果（对齐 handleTarget overwrite 先 remove 再 copy 的模式）。
	_ = os.RemoveAll(bkDir)
	if err := os.MkdirAll(filepath.Dir(bkDir), 0755); err != nil {
		return "", err
	}
	if err := copyTree(dst, bkDir); err != nil {
		return "", err
	}
	return bkDir, nil
}

// isSafeName reports whether name is a canonical basename (non-empty, not . / .., no path
// separators), preventing path-injection escape.
//
// isSafeName 判断 name 是规范 basename（非空、非 . / ..、无路径分隔符），防路径注入越界。
func isSafeName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	return filepath.Base(name) == name
}

// removeTargetTree deletes the target: for link/junction only the reparse point is removed
// (source untouched); real directories are removed recursively.
//
// removeTargetTree 删除目标：link/junction 只删 reparse point（不删源），真目录递归删。
func removeTargetTree(path string) {
	if isJunctionOrLink(path) {
		// For junction/symlink, only remove the reparse point itself.
		_ = os.Remove(path) // 对 junction/symlink 只删 reparse point 本身
		return
	}
	_ = os.RemoveAll(path)
}

// detectState detects the target's distribution state relative to canonical (aligned with
// sync.py target_state).
//
// detectState 检测目标相对 canonical 的分发态（对齐 sync.py target_state）。
func detectState(canonicalSkillDir, targetSkillDir string) string {
	if _, err := os.Lstat(targetSkillDir); err != nil {
		if os.IsNotExist(err) {
			return StateMissing
		}
		// stat error conservatively treated as drift.
		return StateDrift // stat 错误保守按 drift
	}
	// linked: target, after following, is the same physical directory as canonical (junction/
	// symlink points to canonical). Use os.SameFile (compares volume serial + file index) instead
	// of EvalSymlinks—the latter doesn't resolve junctions on Windows (only true symlinks), mis-
	// judging junctions as copy-in-sync. os.Stat follows junction/symlink to the target physical
	// directory; SameFile precisely distinguishes "same physical directory" (linked) from
	// "independent copy" (copy-in-sync/drift).
	//
	// linked：target 跟随后与 canonical 是同一物理目录（junction/symlink 指向 canonical）。
	// 用 os.SameFile（比较 volume serial + file index）而非 EvalSymlinks——后者在 Windows
	// 不解析 junction（只解析真 symlink），会把 junction 误判为 copy-in-sync。
	// os.Stat 对 junction/symlink 跟随到目标物理目录，SameFile 精确区分"同一物理目录"(linked)
	// 与"独立副本"(copy-in-sync/drift)。
	ci, errC := os.Stat(canonicalSkillDir)
	ti, errT := os.Stat(targetSkillDir)
	if errC == nil && errT == nil && os.SameFile(ci, ti) {
		return StateLinked
	}
	// copy-in-sync: SKILL.md md5 identical (independent copy but same content).
	//
	// copy-in-sync：SKILL.md md5 相同（独立副本但内容一致）
	ch, e1 := md5OfFile(filepath.Join(canonicalSkillDir, "SKILL.md"))
	th, e2 := md5OfFile(filepath.Join(targetSkillDir, "SKILL.md"))
	if e1 == nil && e2 == nil && ch == th {
		return StateCopyInSync
	}
	return StateDrift
}

// md5OfFile returns the file md5 hex (first 10 chars, aligned with sync.py md5[:10]).
//
// md5OfFile 返回文件 md5 的 hex（取前 10 字符，对齐 sync.py md5[:10]）。
func md5OfFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])[:10], nil
}

// copyTree copies the whole src tree to dst (atomic writes, skips .git/node_modules etc., does
// not follow links).
//
// copyTree 把 src 整树复制到 dst（原子写，跳过 .git/node_modules 等，不跟随 link）。
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if d.IsDir() {
			if distSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0755)
		}
		// Do not follow reparse points (avoids expanding links into copies).
		//
		// 不跟随 reparse point（避免把 link 展开成副本）
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		// Preserve source permission bits: hardcoding 0644/0755 would drop 0600 (private)/0444
		// (read-only) etc. d.Info() returns the FileInfo of the WalkDir entry; Mode().Perm() takes
		// the rwxrwxrwx permission bits.
		//
		// 保留源文件权限位：硬编码 0644/0755 会丢失 0600（私有）/0444（只读）等位。
		// d.Info() 返回 WalkDir 入口的 FileInfo，Mode().Perm() 取 rwxrwxrwx 权限位。
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		return util.AtomicWrite(filepath.Join(dst, rel), data, info.Mode().Perm())
	})
}

// ListSkills returns the names of all direct subdirectories under canonical that contain a
// SKILL.md (excludes skill-routing/, CONVENTIONS.md, etc.).
//
// ListSkills 返回 canonical 下所有含 SKILL.md 的直接子目录名（排除 skill-routing/、CONVENTIONS.md 等）。
func ListSkills(canonical string) ([]string, error) {
	entries, err := os.ReadDir(canonical)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		dir := filepath.Join(canonical, name)
		// e.IsDir() is based on Lstat and returns false for symlink/junction. forge install's
		// default link mode and external-managed (lark-* junction) skills are links pointing to
		// real directories—must use os.Stat (follows symlinks) to judge, otherwise many link
		// skills under ~/.claude/skills are missed (in practice only real dirs like alipay-* are
		// detected). Symlink loops make os.Stat error → skip, which is safe.
		//
		// e.IsDir() 基于 Lstat，对 symlink/junction 返回 false。forge install 默认
		// link 模式、external managed（lark-* junction）的 skill 都是 link 指向真实
		// 目录——必须用 os.Stat（跟随 symlink）判断，否则 ~/.claude/skills 下大量
		// link skill 被漏掉（实测只识别真实目录如 alipay-*）。symlink 循环 os.Stat
		// 报错 → 跳过，安全。
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err == nil {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names, nil
}

func filterNames(all, want []string) []string {
	set := map[string]bool{}
	for _, w := range want {
		set[w] = true
	}
	var out []string
	for _, a := range all {
		if set[a] {
			out = append(out, a)
		}
	}
	return out
}

// TargetDirs resolves the target-tool → target-skills-directory map. target=all expands to
// claude/cursor/pi/codex/copilot.
//
// TargetDirs 解析目标工具→目标 skills 目录的映射。target=all 展开 claude/cursor/pi/codex/copilot。
func TargetDirs(targets []Target, global bool, projectSkillsDir string) (map[string]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	seen := map[string]bool{}
	expand := func(t Target) {
		if t == TargetAll {
			for _, sub := range []Target{TargetClaude, TargetCursor, TargetCodex, TargetCopilot} {
				if !seen[string(sub)] {
					seen[string(sub)] = true
					out[string(sub)] = targetDir(string(sub), global, home, projectSkillsDir)
				}
			}
			return
		}
		if !seen[string(t)] {
			seen[string(t)] = true
			out[string(t)] = targetDir(string(t), global, home, projectSkillsDir)
		}
	}
	for _, t := range targets {
		expand(t)
	}
	return out, nil
}

func targetDir(name string, global bool, home, projectSkillsDir string) string {
	if !global {
		return projectSkillsDir // 项目级统一 .claude/skills
	}
	switch name {
	case "claude":
		return filepath.Join(home, ".claude", "skills")
	case "cursor":
		return filepath.Join(home, ".cursor", "skills")
	case "codex":
		// Since 2025-12 Codex CLI natively reads ~/.codex/skills/<slug>/SKILL.md (aligned with the
		// Claude/Cursor format). Note openai/codex#17344: Codex once skipped user skills whose
		// SKILL.md file itself is a symlink. This tool's makeDirLink creates a directory-level
		// junction (the whole <slug> directory points to canonical); the SKILL.md inside the
		// junction is a real file, not a symlink—theoretically unaffected by that bug. But a
		// junction is a Windows reparse point; Codex's actual follow behavior needs to be tested
		// on the host. If Codex doesn't recognize a link-distributed skill, fall back to
		// --mode copy --target codex.
		//
		// Codex CLI 2025-12 起原生读 ~/.codex/skills/<slug>/SKILL.md（对齐 Claude/Cursor 格式）。
		// 注意 openai/codex#17344：Codex 曾跳过"SKILL.md 文件本身是 symlink"的 user skill。
		// 本工具 makeDirLink 做的是目录级 junction（整个 <slug> 目录指向 canonical），
		// junction 内 SKILL.md 是真实文件非 symlink，理论上不受该 bug 影响——
		// 但 junction 是 Windows reparse point，Codex 实际跟随行为需在本机实测。
		// 若 Codex 未识别 link 分发的 skill，降级用 --mode copy --target codex。
		return filepath.Join(home, ".codex", "skills")
	case "copilot":
		// GitHub Copilot personal skills (cross-project) live at
		// ~/.copilot/skills/<slug>/SKILL.md (project-level ones go to .github/skills/, out of
		// scope here—global personal only). Format is compatible with Claude SKILL.md.
		//
		// GitHub Copilot 个人 skill（跨项目）放 ~/.copilot/skills/<slug>/SKILL.md
		// （项目级放 .github/skills/，这里只管全局个人级）。格式与 Claude SKILL.md 兼容。
		return filepath.Join(home, ".copilot", "skills")
	}
	return ""
}

// orderedTargetNames returns target names in a fixed order (alphabetic
// claude<codex<copilot<cursor<pi) for stable output.
//
// orderedTargetNames 返回固定排序的目标名（字母序 claude<codex<copilot<cursor<pi），输出稳定。
func orderedTargetNames(m map[string]string) []string {
	return slices.Sorted(maps.Keys(m))
}
