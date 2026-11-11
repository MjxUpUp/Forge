// Package change 实现仓库内的 change 生命周期（spec-in-repo 第二块）。
//
// 第一块（internal/specroot）把成员资格的锚点从安装态移到仓库态：仓库里
// 有 spec/forge.yaml 即被认定为 forge 项目。本包负责这份契约里最实在的
// 那一半——change 的产物与状态：
//
//	spec/changes/<id>/proposal.md    为什么做、备选方案与取舍
//	spec/changes/<id>/design.md      怎么做
//	spec/changes/<id>/tasks.md       拆成哪些可执行步骤
//	spec/changes/<id>/acceptance.md  判据：可执行的 bash 命令 + 人工检查项
//	spec/changes/<id>/evidence/      实跑证据（stdout/stderr/exit + 绑定 diff 的封印）
//
// 设计约束（来自 E:\Forge\docs\design\spec-in-repo-activation-2026-11-proposal.md）：
//   - 状态从文件系统推导，不新增进度库：删掉证据就回到未验证，改判据就让旧证据
//     失效（Stale），不需要谁去「更新任务状态」。
//   - acceptance.md 必须可执行。写不成命令的验收标准不是判据，只是愿望。
//   - evidence 与 diff 绑定并封印：验证的是"这个工作区的这份改动"，
//     换了代码或换了判据，旧证据自动作废。
//
// 这里刻意不依赖 forge 的项目数据目录：所有产物都躺在仓库里随 git 走，
// clone 到别的机器、换个 agent 也能续上——这正是它「不装插件也能用」的部分。
package change

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/MjxUpUp/Forge/internal/specroot"
)

const (
	// DirName 是 change 目录名（位于 spec/ 之下）。
	DirName = "changes"
	// ArchiveDirName 是归档子目录：已交付的 change 挪进来，保留可追溯性。
	ArchiveDirName = "archive"

	// FileProposal 等是 change 目录内的标准产物名。
	FileProposal   = "proposal.md"
	FileDesign     = "design.md"
	FileTasks      = "tasks.md"
	FileAcceptance = "acceptance.md"

	// EvidenceDirName 是实跑证据目录；SummaryFile 是每次验证的封印文件。
	EvidenceDirName = "evidence"
	SummaryFile     = "summary.json"
)

// Stage 是 change 的阶段，完全由目录里的文件推导。
type Stage string

const (
	// StageEmpty：目录在，但还没有任何产物。
	StageEmpty Stage = "empty"
	// StageProposal：只有提案。
	StageProposal Stage = "proposal"
	// StageDesign：提案 + 设计。
	StageDesign Stage = "design"
	// StageTasks：提案 + 设计 + 任务拆分，可以开工。
	StageTasks Stage = "tasks"
	// StageVerified：有与当前判据、当前改动一致（未 stale）的通过证据。
	StageVerified Stage = "verified"
	// StageArchived：已归档到 spec/changes/archive/。
	StageArchived Stage = "archived"
)

// ErrNoSpec 表示仓库缺少 spec/ 目录（还没声明契约）。
var ErrNoSpec = errors.New("change: 仓库没有 spec/（先运行 forge change init 声明契约）")

var idPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

// ValidID 校验 change id：小写字母数字与连字符，首尾必须是字母数字。
// 收紧到 kebab-case 是为了让它同时能当目录名、分支名与 evidence 目录名。
func ValidID(id string) error {
	if id == "" {
		return errors.New("change: id 不能为空")
	}
	if !idPattern.MatchString(id) {
		return fmt.Errorf("change: id %q 不合法（小写字母/数字/连字符，首尾非连字符，≤64 字符）", id)
	}
	// 保留名：这两个会与 spec/changes/archive/、<change>/evidence/ 撞目录，
	// 允许它们会让 change 建好即从 list 里消失。
	if id == ArchiveDirName || id == EvidenceDirName {
		return fmt.Errorf("change: id %q 是保留名（归档/证据目录占用）", id)
	}
	return nil
}

// ChangesDir 返回 root 下的 change 根目录（spec/changes）。
func ChangesDir(root string) string {
	return filepath.Join(root, specroot.DirName, DirName)
}

// ChangeDir 返回某个 change 的目录（spec/changes/<id>）。
func ChangeDir(root, id string) string {
	return filepath.Join(ChangesDir(root), id)
}

// Change 是一个 change 的磁盘视图（不缓存，每次从文件推导）。
type Change struct {
	ID      string `json:"id"`
	Dir     string `json:"dir"`
	Archive bool   `json:"archived,omitempty"`

	HasProposal   bool `json:"has_proposal"`
	HasDesign     bool `json:"has_design"`
	HasTasks      bool `json:"has_tasks"`
	HasAcceptance bool `json:"has_acceptance"`

	// EvidenceCount 是已落盘的证据目录数；Latest 是最新一次的摘要（可能为空）。
	EvidenceCount int    `json:"evidence_count"`
	Latest        string `json:"latest_evidence,omitempty"`
	LatestDir     string `json:"latest_evidence_dir,omitempty"`

	// Sealed 表示最新证据通过且与当前判据一致；Stale 表示曾经通过但判据/改动变了。
	Sealed bool `json:"sealed"`
	Stale  bool `json:"stale"`
	// DiffBound 表示证据记录了工作区指纹（非 git 仓库里为 false，封印强度更弱）。
	DiffBound bool `json:"diff_bound"`

	Stage Stage `json:"stage"`
}

// Init 在 root 下声明契约：写 spec/forge.yaml（缺省 profile=mini）、
// 建 spec/changes/，并在没有 AGENTS.md 时放一份 20 行协议。
//
// 已存在的东西一律不覆盖——init 是幂等的引导，不是模板重刷。
func Init(root, profile string) (created []string, err error) {
	if profile == "" {
		profile = specroot.ProfileMini
	}
	switch profile {
	case specroot.ProfileMini, specroot.ProfileStandard, specroot.ProfileFull:
	default:
		return nil, fmt.Errorf("change: 未知 profile %q（可选 %s/%s/%s）",
			profile, specroot.ProfileMini, specroot.ProfileStandard, specroot.ProfileFull)
	}

	marker := specroot.MarkerPath(root)
	if _, statErr := os.Stat(marker); statErr == nil {
		return nil, fmt.Errorf("change: %s 已存在（不覆盖；要改档位请直接编辑该文件）", marker)
	}
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		return nil, fmt.Errorf("change: 创建 spec/: %w", err)
	}
	body := fmt.Sprintf("# forge 契约（由 forge change init 生成）\n# 成员资格锚点：仓库里有这个文件即被认定为 forge 项目，clone 即用。\nversion: %q\nprofile: %q\n",
		specroot.CurrentVersion, profile)
	if err := os.WriteFile(marker, []byte(body), 0o644); err != nil {
		return nil, fmt.Errorf("change: 写 %s: %w", marker, err)
	}
	created = append(created, relTo(root, marker))

	if err := os.MkdirAll(ChangesDir(root), 0o755); err != nil {
		return nil, fmt.Errorf("change: 创建 spec/changes/: %w", err)
	}
	created = append(created, relTo(root, ChangesDir(root))+"/")

	agents := filepath.Join(root, "AGENTS.md")
	if _, statErr := os.Stat(agents); errors.Is(statErr, os.ErrNotExist) {
		if err := os.WriteFile(agents, []byte(renderTemplate(tmplAgents, "", "")), 0o644); err != nil {
			return nil, fmt.Errorf("change: 写 AGENTS.md: %w", err)
		}
		created = append(created, "AGENTS.md")
	}
	return created, nil
}

// New 创建一个 change 骨架（四份产物模板），返回目录。
// 已存在则报错——change 是人的决策落点，不能被静默覆盖。
func New(root, id, title string) (string, error) {
	if err := ValidID(id); err != nil {
		return "", err
	}
	dir := ChangeDir(root, id)
	if _, err := os.Stat(dir); err == nil {
		return "", fmt.Errorf("change: %s 已存在", relTo(root, dir))
	}
	if _, err := os.Stat(specroot.MarkerPath(root)); err != nil {
		return "", fmt.Errorf("%w（%s 不存在）", ErrNoSpec, relTo(root, specroot.MarkerPath(root)))
	}
	if title == "" {
		title = id
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("change: 创建 %s: %w", relTo(root, dir), err)
	}
	files := map[string]string{
		FileProposal:   tmplProposal,
		FileDesign:     tmplDesign,
		FileTasks:      tmplTasks,
		FileAcceptance: tmplAcceptance,
	}
	for name, tmpl := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(renderTemplate(tmpl, id, title)), 0o644); err != nil {
			return "", fmt.Errorf("change: 写 %s: %w", relTo(root, path), err)
		}
	}
	return dir, nil
}

// List 枚举 spec/changes 下的全部 change（含 archive/ 下的归档项），按 id 排序。
func List(root string) ([]Change, error) {
	base := ChangesDir(root)
	if _, err := os.Stat(base); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoSpec
		}
		return nil, fmt.Errorf("change: 读取 %s: %w", relTo(root, base), err)
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, fmt.Errorf("change: 读取 %s: %w", relTo(root, base), err)
	}
	var out []Change
	// 工作区指纹对整仓只需算一次（List 里所有 change 共享同一份工作区）。
	fp := WorktreeFingerprint(root)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// 先认 archive 目录：它是保留名，ValidID 会拒掉它，
		// 顺序反了会把整棵归档树当成"非法 id"静默跳过。
		if e.Name() == ArchiveDirName {
			archived, aerr := listArchived(root)
			if aerr != nil {
				return nil, aerr
			}
			out = append(out, archived...)
			continue
		}
		if ValidID(e.Name()) != nil {
			// 不是合法 id 的目录（编辑器临时物、.DS_Store 之外的杂项）不是 change。
			continue
		}
		c, cerr := load(root, e.Name(), &fp)
		if cerr != nil {
			return nil, cerr
		}
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Archive != out[j].Archive {
			return !out[i].Archive
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func listArchived(root string) ([]Change, error) {
	base := filepath.Join(ChangesDir(root), ArchiveDirName)
	entries, err := os.ReadDir(base)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("change: 读取 %s: %w", relTo(root, base), err)
	}
	var out []Change
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := archiveID(e.Name())
		if ValidID(id) != nil {
			continue
		}
		dir := filepath.Join(base, e.Name())
		c, cerr := loadFrom(root, id, dir, nil)
		if cerr != nil {
			return nil, cerr
		}
		c.Archive = true
		c.Stage = StageArchived
		// 归档记录的是"当时验过什么"，之后仓库怎么变都不改变这条历史；
		// 把 archived 条目按当前工作区判 stale 会让归档列表越用越吓人。
		c.Stale = false
		out = append(out, *c)
	}
	return out, nil
}

// InvalidArchiveNames 返回 spec/changes/archive/ 下不符合 <YYYYMMDD>-<id>
// 约定的目录名。List 会跳过这些目录；调用方应当把它说出来，而不是让人以为
// 归档记录凭空消失了。
func InvalidArchiveNames(root string) ([]string, error) {
	base := filepath.Join(ChangesDir(root), ArchiveDirName)
	entries, err := os.ReadDir(base)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("change: 读取 %s: %w", relTo(root, base), err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if ValidID(archiveID(e.Name())) != nil {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// archiveID 去掉归档目录名里的日期前缀（<YYYYMMDD>-<id> → <id>）。
// 前缀必须真的是 8 位数字，否则 `demo-x` 这类目录会被当成 id `x`。
func archiveID(name string) string {
	if len(name) > 9 && name[8] == '-' && allASCIIDigits(name[:8]) {
		return name[9:]
	}
	return name
}

func allASCIIDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

// Load 读取单个 change 的磁盘状态。
//
// 与 List 用同一套判据：各自算一次工作区指纹，避免"list 说失效、verify 显示
// 已封印"这种两套结论。
// Load 读取单个 change，并返回这次判定所用的工作区指纹。
//
// Verify 要同时知道"旧证据是否失效"和"当前工作区指纹"，分别算一遍会把每个
// 改动文件读两次（未跟踪文件只能逐个读盘哈希，代价随改动量线性上升）。
func Load(root, id string) (*Change, *Fingerprint, error) {
	if err := ValidID(id); err != nil {
		return nil, nil, err
	}
	fp := WorktreeFingerprint(root)
	c, err := loadFrom(root, id, ChangeDir(root, id), &fp)
	if err != nil {
		return nil, nil, err
	}
	return c, &fp, nil
}

func load(root, id string, fp *Fingerprint) (*Change, error) {
	return loadFrom(root, id, ChangeDir(root, id), fp)
}

func loadFrom(root, id, dir string, fp *Fingerprint) (*Change, error) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("change: 不存在 %s", relTo(root, dir))
	}
	c := &Change{ID: id, Dir: dir}
	c.HasProposal = exists(filepath.Join(dir, FileProposal))
	c.HasDesign = exists(filepath.Join(dir, FileDesign))
	c.HasTasks = exists(filepath.Join(dir, FileTasks))
	c.HasAcceptance = exists(filepath.Join(dir, FileAcceptance))

	evDir := filepath.Join(dir, EvidenceDirName)
	if evEntries, eerr := os.ReadDir(evDir); eerr == nil {
		var names []string
		for _, e := range evEntries {
			// 只认 verify 自己产出的时间戳目录；evidence/foo 之类的杂物不能顶替真证据。
			if e.IsDir() && evidenceNameRE.MatchString(e.Name()) {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		c.EvidenceCount = len(names)
		if len(names) > 0 {
			c.Latest = names[len(names)-1]
			c.LatestDir = filepath.Join(evDir, c.Latest)
			if sum, serr := loadSummary(c.LatestDir); serr == nil {
				c.Sealed = sum.Sealed
				c.DiffBound = sum.DiffSHA256 != ""
				stale := false
				// 判据漂移：acceptance.md 改过或者被删掉，旧证据都不再代表当前验收标准。
				if !c.HasAcceptance {
					stale = c.Sealed
				} else if sum.AcceptanceSHA256 != "" {
					// 读不出来（权限、IO 错误）就无法证明判据没漂移。这种情况按
					// 保守处理判失效：宁可让人重跑一次 verify，也不能让一份来路
					// 不明的旧证据继续对外宣称"已封印"。
					raw, rerr := os.ReadFile(filepath.Join(dir, FileAcceptance))
					if rerr != nil || hashBytes(raw) != sum.AcceptanceSHA256 {
						stale = true
					}
				}
				// 内容漂移：封印时记录的每个改动文件，现在内容必须还对得上。
				// 只比内容、不比 HEAD——提交把改动落进 commit 并不改变文件内容，
				// 不该因此判失效；但提交后又改了这些文件，内容变了就必须重跑。
				if fp != nil && fp.OK && sum.DiffSHA256 != "" {
					if filesDrifted(root, sum.Files) {
						stale = true
					}
					// HEAD 没动时还要比"改动集合"：verify 之后新写一批文件
					// 却不提交，等于在被验证的代码之外继续加料——这份证据
					// 已经代表不了当前工作区。换过 HEAD 则只按内容比：
					// 提交之后出现的新改动属于下一件事，不该把已落地的证据判死。
					if sum.Head != "" && sum.Head == fp.Head && !sameDigests(sum.Files, fp.Files) {
						stale = true
					}
					// 干净工作区 verify 没有文件级指纹，证据只代表 verify 时那个
					// commit。HEAD 一动，被验证的代码就不是当初那份了——不能因为
					// "没记录改动文件"就继续对外宣称已验证。
					if len(sum.Files) == 0 && sum.Head != "" && sum.Head != fp.Head {
						stale = true
					}
				}
				if stale {
					c.Stale = c.Sealed
					c.Sealed = false
				}
			}
		}
	}

	switch {
	case c.Sealed:
		c.Stage = StageVerified
	case c.HasTasks:
		c.Stage = StageTasks
	case c.HasDesign:
		c.Stage = StageDesign
	case c.HasProposal:
		c.Stage = StageProposal
	default:
		c.Stage = StageEmpty
	}
	return c, nil
}

// Summary 是一次验证的封印摘要（evidence/<ts>/summary.json）。
type Summary struct {
	Change     string `json:"change"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
	Passed     bool   `json:"passed"`
	Sealed     bool   `json:"sealed"`
	Head       string `json:"head,omitempty"`
	DiffSHA256 string `json:"diff_sha256,omitempty"`
	// Files 是封印时每个改动文件的内容摘要。判失效按"文件内容是否还对得上"
	// 而不是按 HEAD：先 verify 后 commit 与先 commit 后 verify 是同一份代码。
	Files            []FileDigest `json:"files,omitempty"`
	AcceptanceSHA256 string       `json:"acceptance_sha256"`
	// DiffBound 表示证据绑定了 git 工作区指纹；false 时只能证明命令跑过，
	// 证明不了它对应哪份代码（非 git 仓库）。
	DiffBound bool    `json:"diff_bound,omitempty"`
	Checks    []Check `json:"checks"`
}

// evidenceNameRE 匹配 verify 产出的证据目录名（UTC 时间戳，同秒重跑带 -N 后缀）。
var evidenceNameRE = regexp.MustCompile(`^\d{8}T\d{6}Z(-\d+)?$`)

func loadSummary(dir string) (*Summary, error) {
	raw, err := os.ReadFile(filepath.Join(dir, SummaryFile))
	if err != nil {
		return nil, err
	}
	var s Summary
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func relTo(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}

// renderTemplate 把模板里的占位符换成具体值（模板只认这两个占位符，
// 用最小替换而不是 text/template——产物是给人读的 markdown，不需要控制流）。
func renderTemplate(tmpl, id, title string) string {
	out := strings.ReplaceAll(tmpl, "{{id}}", id)
	return strings.ReplaceAll(out, "{{title}}", title)
}
