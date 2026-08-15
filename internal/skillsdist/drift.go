package skillsdist

import (
	"fmt"
	"os"
	"path/filepath"
)

// DriftReport is the result of DriftCheck: distribution state of every skill x target plus target-only orphans.
//
// DriftReport 是 DriftCheck 的结果：所有 skill×目标的分发态 + target-only 孤儿。
type DriftReport struct {
	Canonical string      `json:"canonical"`
	Items     []DriftItem `json:"items"`
	Stats     DriftStats  `json:"stats"`
	Errors    []string    `json:"errors,omitempty"` // target 目录读取失败（权限等，非"不存在"）
}

// DriftItem is the distribution-state record of one skill in one target.
//
// DriftItem 是单 skill 在单目标的分发态记录。
type DriftItem struct {
	Name   string `json:"name"`
	Target string `json:"target"`
	Dir    string `json:"dir"`
	State  string `json:"state"`
}

// DriftStats is the state-distribution tally for drift-check.
//
// DriftStats 是 drift-check 的态分布统计。
type DriftStats struct {
	Linked     int `json:"linked"`
	CopyInSync int `json:"copy_in_sync"`
	Drift      int `json:"drift"`
	Missing    int `json:"missing"`
	TargetOnly int `json:"target_only"`
}

// StateTargetOnly is the orphan state for a skill present in a target directory but missing from canonical.
//
// StateTargetOnly 是目标目录里有 skill 但 canonical 没有的孤儿态。
const StateTargetOnly = "target-only"

// DriftCheck walks every canonical skill x target directory, detecting only the
// distribution state (writes nothing), and reports target-only orphans. Backs
// the forge skills drift-check command (dry-run).
//
// DriftCheck 遍历 canonical skill × 目标目录，只检测分发态（不写任何东西），
// 并报告 target-only 孤儿。对应 `forge skills drift-check`（dry-run）。
func DriftCheck(canonical string, opts InstallOpts) (*DriftReport, error) {
	report := &DriftReport{Canonical: canonical}

	names, err := ListSkills(canonical)
	if err != nil {
		return nil, err
	}
	// nameSet for target-only orphan detection must use the FULL canonical list (before
	// SkillFilter). With a filtered set, `--skill foo` would misreport every other legit
	// skill present in the target as an orphan.
	//
	// target-only 孤儿检测的 nameSet 必须用过滤前的完整 canonical 名单。用过滤后的集合，
	// `--skill foo` 会把 target 里其他正常 skill 全误报成孤儿。
	nameSet := map[string]bool{}
	for _, n := range names {
		nameSet[n] = true
	}
	if len(opts.SkillFilter) > 0 {
		names, err = filterNames(names, opts.SkillFilter)
		if err != nil {
			return nil, err
		}
	}
	// Project profile scopes the drift walk the same way install does: excluded skills are
	// not managed by this project's distribution, so reporting them would be noise. Unknown
	// profile entries are silently dropped here (read-only view; install is the surface that
	// warns about them).
	//
	// 项目画像以与 install 相同的方式界定 drift 遍历范围：被排除的 skill 不归本项目
	// 分发管，报告它们是噪声。画像里的未知条目在此静默丢弃（只读视图；告警它们是
	// install 的职责）。
	if len(opts.Profile) > 0 {
		names, _ = filterByProfile(names, opts.Profile)
	}

	targetDirs, err := TargetDirs(opts.Targets, opts.Global, opts.ProjectSkillsDir)
	if err != nil {
		return nil, err
	}
	targetOrder := orderedTargetNames(targetDirs)

	for _, name := range names {
		for _, tname := range targetOrder {
			dst := filepath.Join(targetDirs[tname], name)
			state := detectState(filepath.Join(canonical, name), dst)
			report.Items = append(report.Items, DriftItem{Name: name, Target: tname, Dir: dst, State: state})
			switch state {
			case StateLinked:
				report.Stats.Linked++
			case StateCopyInSync:
				report.Stats.CopyInSync++
			case StateDrift:
				report.Stats.Drift++
			case StateMissing:
				report.Stats.Missing++
			}
		}
	}

	// target-only: a skill exists in a target directory but not in canonical (orphan or externally managed).
	//
	// target-only：目标目录里有 skill 但 canonical 没有（孤儿/外部管理）
	for _, tname := range targetOrder {
		tdir := targetDirs[tname]
		entries, err := os.ReadDir(tdir)
		if err != nil {
			// A missing target directory is normal (the target was never installed; nothing
			// to report as target-only); other errors (permissions, etc.) are recorded in
			// the report to avoid silently swallowing them and making target-only detection a no-op.
			//
			// 目录不存在是正常的（该 target 未安装，无 target-only 可报）；
			// 其他错误（权限等）记录到 report，避免静默吞掉让 target-only 检测空跑。
			if !os.IsNotExist(err) {
				report.Errors = append(report.Errors, fmt.Sprintf("target %s: ReadDir %v", tname, err))
			}
			continue
		}
		for _, e := range entries {
			// DirEntryIsDir follows junction/symlink (os.Stat semantics); e.IsDir() is
			// Lstat-based and would silently skip link-form skills in orphan detection.
			//
			// DirEntryIsDir 跟随 junction/symlink（os.Stat 语义）；e.IsDir() 基于
			// Lstat，会在孤儿检测里静默漏掉 link 形态的 skill。
			if !DirEntryIsDir(tdir, e) || nameSet[e.Name()] {
				continue
			}
			report.Stats.TargetOnly++
			report.Items = append(report.Items, DriftItem{
				Name: e.Name(), Target: tname, Dir: filepath.Join(tdir, e.Name()), State: StateTargetOnly,
			})
		}
	}
	return report, nil
}
