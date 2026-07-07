package skillsdist

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/skillsfm"
	"github.com/MjxUpUp/Forge/internal/skillsqa"
	"github.com/MjxUpUp/Forge/internal/util"
)

// 分发模式。
type Mode string

const (
	ModeLink Mode = "link" // 目标 = 指向 canonical 的 junction/symlink（默认，单源）
	ModeCopy Mode = "copy" // 目标 = canonical 的独立副本
)

// DriftPolicy 对 drift（目标与 canonical 内容分叉）态的处理策略。
type DriftPolicy string

const (
	DriftAbort     DriftPolicy = "abort"     // 遇 drift 立即返回错误（默认，CI 友好）
	DriftSkip      DriftPolicy = "skip"      // 跳过 drift 的 skill
	DriftOverwrite DriftPolicy = "overwrite" // 强制以 canonical 覆盖
)

// 分发目标工具。
type Target string

const (
	TargetClaude  Target = "claude"
	TargetCursor  Target = "cursor"
	TargetCodex   Target = "codex"   // OpenAI Codex CLI（~/.codex/skills，2025-12 起 SKILL.md 原生支持）
	TargetCopilot Target = "copilot" // GitHub Copilot（~/.copilot/skills，跨项目个人 skill）
	TargetAll     Target = "all"
)

// 分发态（对齐 sync.py 四态）。
const (
	StateLinked     = "linked"       // 目标已是指向 canonical 的 link
	StateCopyInSync = "copy-in-sync" // 目标是副本且内容与 canonical 一致
	StateDrift      = "drift"        // 目标存在但内容与 canonical 分叉
	StateMissing    = "missing"      // 目标不存在
)

// reservedNames 是 forge 自身 skillgen 管理的 skill 名，install 必须跳过——
// 否则 autoSync 每次会用自生成版覆盖用户装的，造成分发抖动。
var reservedNames = map[string]bool{
	"forge-quality": true,
}

// InstallOpts 是 Install 的输入。
type InstallOpts struct {
	Mode             Mode
	DriftPolicy      DriftPolicy
	Targets          []Target
	SkillFilter      []string // 只装指定 skill（空=全部）
	SkipQuality      bool     // 跳过 install 前的 registry+audit 双门控
	SkipRequireCheck bool     // 跳过 frontmatter.requires 依赖同装检查（逃生舱）
	Global           bool     // true→~/.claude/skills 等；false→ProjectSkillsDir
	ProjectSkillsDir string   // Global=false 时用（项目 .claude/skills）
	BackupBase       string   // overwrite 备份根目录（测试注入；生产留空→自动 ~/.forge/skills-backup/<ts>）
}

// InstallReport 是一次 Install 的完整结果。
type InstallReport struct {
	Mode      Mode                 `json:"mode"`
	Canonical string               `json:"canonical"`
	Skills    []SkillInstallResult `json:"skills"`
	Stats     InstallStats         `json:"stats"`
	Aborted   string               `json:"aborted,omitempty"`
	Warnings  []string             `json:"warnings,omitempty"` // requires 依赖未满足等非阻断警告
}

// SkillInstallResult 是单个 skill 在所有目标的安装结果。
type SkillInstallResult struct {
	Name    string         `json:"name"`
	Pass    bool           `json:"pass"`
	Issues  []string       `json:"issues,omitempty"`
	Targets []TargetResult `json:"targets"`
}

// TargetResult 是单 skill 在单目标的安装结果。
type TargetResult struct {
	Target string `json:"target"`
	Dir    string `json:"dir"`
	State  string `json:"state"`
	Action string `json:"action"`
	Detail string `json:"detail"`
	Backup string `json:"backup,omitempty"` // overwrite 前的备份路径（空=未备份：link/断链/非 overwrite）
}

// InstallStats 是 install 统计摘要。
type InstallStats struct {
	Total     int `json:"total"`
	Installed int `json:"installed"`
	Skipped   int `json:"skipped"`
	Drifted   int `json:"drifted"`
	Failed    int `json:"failed"`
}

// skipDirs 与 skillsqa 同步：copyTree 跳过的目录段。
var distSkipDirs = map[string]bool{
	"node_modules": true, ".git": true, "__pycache__": true, ".venv": true,
}

// Install 把 canonical 下的 skill 同步到各目标目录。
func Install(canonical string, opts InstallOpts) (*InstallReport, error) {
	report := &InstallReport{Mode: opts.Mode, Canonical: canonical}

	// overwrite 备份根目录：opts.BackupBase（测试注入）或 ~/.forge/skills-backup/<ts>（生产）。
	// 一次 install 共享一个 ts 子目录，便于"这次覆盖了什么"回溯与恢复。
	backupBase := opts.BackupBase
	if backupBase == "" {
		home, herr := os.UserHomeDir()
		if herr != nil {
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
				res.Targets = []TargetResult{{Action: actBlocked, Detail: "registry 规范门控未通过（R1-R9）"}}
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

	// requires 依赖检查：对本次成功装的 skill 读 frontmatter.requires，检查声明的依赖
	// 是否在 canonical 全集（声明有效）且本次同装。不满足记入 Warnings（仅提示，不阻断）。
	// 解除 requires 字段无消费方的既有缺陷——单装依赖 skill 会断链，此处显式提示。
	if !opts.SkipRequireCheck {
		report.Warnings = checkRequires(canonical, report.Skills)
	}
	return report, nil
}

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

// handleTarget 按当前 state + mode + policy 决定对单个目标的动作。
// 返回 (action, detail, abortErr)；abortErr != nil 表示 drift-policy=abort 触发，调用方应中止整个 install。
func handleTarget(src, dst, state string, mode Mode, policy DriftPolicy) (string, string, error) {
	switch state {
	case StateLinked:
		return actSkipped, "已是 link（内容同步）", nil
	case StateCopyInSync:
		if mode == ModeLink {
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

// backupTarget 把即将被 overwrite 的 dst 备份到 backupBase/<target>/<skill>。
// 只备份真目录副本（用户的本地定制）；link/junction 无独立内容、断链/不存在/非目录无内容，
// 一律跳过返回空串。用 copyTree（与 install 一致，跳过 .git/node_modules）。
// 失败返回 error，调用方决定是否继续 overwrite。
func backupTarget(dst, backupBase, target, skill string) (string, error) {
	if backupBase == "" {
		return "", nil
	}
	// link/junction：reparse point 无独立用户内容（指向别处或断链），备份无意义。
	if isJunctionOrLink(dst) {
		return "", nil
	}
	info, err := os.Stat(dst)
	if err != nil || !info.IsDir() {
		return "", nil // 断链 os.Stat 失败；单文件（非目录）也不备份
	}
	bkDir := filepath.Join(backupBase, target, skill)
	// 路径注入防御：target/skill 须为规范 basename（非 ./..、无分隔符）。
	// canonical skill 名受文件系统约束天然安全，此处防御性兜底——防未来 canonical 来源
	// 含恶意名导致 backupBase 越界写。
	if !isSafeName(target) || !isSafeName(skill) {
		return "", fmt.Errorf("非法 target/skill 名（路径注入风险）: %q/%q", target, skill)
	}
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

// isSafeName 判断 name 是规范 basename（非空、非 . / ..、无路径分隔符），防路径注入越界。
func isSafeName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	return filepath.Base(name) == name
}

// removeTargetTree 删除目标：link/junction 只删 reparse point（不删源），真目录递归删。
func removeTargetTree(path string) {
	if isJunctionOrLink(path) {
		_ = os.Remove(path) // 对 junction/symlink 只删 reparse point 本身
		return
	}
	_ = os.RemoveAll(path)
}

// detectState 检测目标相对 canonical 的分发态（对齐 sync.py target_state）。
func detectState(canonicalSkillDir, targetSkillDir string) string {
	if _, err := os.Lstat(targetSkillDir); err != nil {
		if os.IsNotExist(err) {
			return StateMissing
		}
		return StateDrift // stat 错误保守按 drift
	}
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
	// copy-in-sync：SKILL.md md5 相同（独立副本但内容一致）
	ch, e1 := md5OfFile(filepath.Join(canonicalSkillDir, "SKILL.md"))
	th, e2 := md5OfFile(filepath.Join(targetSkillDir, "SKILL.md"))
	if e1 == nil && e2 == nil && ch == th {
		return StateCopyInSync
	}
	return StateDrift
}

// md5OfFile 返回文件 md5 的 hex（取前 10 字符，对齐 sync.py md5[:10]）。
func md5OfFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])[:10], nil
}

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
		// 不跟随 reparse point（避免把 link 展开成副本）
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		// 保留源文件权限位：硬编码 0644/0755 会丢失 0600（私有）/0444（只读）等位。
		// d.Info() 返回 WalkDir 入口的 FileInfo，Mode().Perm() 取 rwxrwxrwx 权限位。
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		return util.AtomicWrite(filepath.Join(dst, rel), data, info.Mode().Perm())
	})
}

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
	sort.Strings(names)
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
		// Codex CLI 2025-12 起原生读 ~/.codex/skills/<slug>/SKILL.md（对齐 Claude/Cursor 格式）。
		// 注意 openai/codex#17344：Codex 曾跳过"SKILL.md 文件本身是 symlink"的 user skill。
		// 本工具 makeDirLink 做的是目录级 junction（整个 <slug> 目录指向 canonical），
		// junction 内 SKILL.md 是真实文件非 symlink，理论上不受该 bug 影响——
		// 但 junction 是 Windows reparse point，Codex 实际跟随行为需在本机实测。
		// 若 Codex 未识别 link 分发的 skill，降级用 --mode copy --target codex。
		return filepath.Join(home, ".codex", "skills")
	case "copilot":
		// GitHub Copilot 个人 skill（跨项目）放 ~/.copilot/skills/<slug>/SKILL.md
		// （项目级放 .github/skills/，这里只管全局个人级）。格式与 Claude SKILL.md 兼容。
		return filepath.Join(home, ".copilot", "skills")
	}
	return ""
}

// orderedTargetNames 返回固定排序的目标名（字母序 claude<codex<copilot<cursor<pi），输出稳定。
func orderedTargetNames(m map[string]string) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
