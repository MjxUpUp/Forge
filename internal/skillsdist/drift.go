package skillsdist

import (
	"fmt"
	"os"
	"path/filepath"
)

// DriftReport 是 DriftCheck 的结果：所有 skill×目标的分发态 + target-only 孤儿。
type DriftReport struct {
	Canonical string      `json:"canonical"`
	Items     []DriftItem `json:"items"`
	Stats     DriftStats  `json:"stats"`
	Errors    []string    `json:"errors,omitempty"` // target 目录读取失败（权限等，非"不存在"）
}

// DriftItem 是单 skill 在单目标的分发态记录。
type DriftItem struct {
	Name   string `json:"name"`
	Target string `json:"target"`
	Dir    string `json:"dir"`
	State  string `json:"state"`
}

// DriftStats 是 drift-check 的态分布统计。
type DriftStats struct {
	Linked     int `json:"linked"`
	CopyInSync int `json:"copy_in_sync"`
	Drift      int `json:"drift"`
	Missing    int `json:"missing"`
	TargetOnly int `json:"target_only"`
}

// StateTargetOnly 是目标目录里有 skill 但 canonical 没有的孤儿态。
const StateTargetOnly = "target-only"

// DriftCheck 遍历 canonical skill × 目标目录，只检测分发态（不写任何东西），
// 并报告 target-only 孤儿。对应 `forge skills drift-check`（dry-run）。
func DriftCheck(canonical string, opts InstallOpts) (*DriftReport, error) {
	report := &DriftReport{Canonical: canonical}

	names, err := ListSkills(canonical)
	if err != nil {
		return nil, err
	}
	if len(opts.SkillFilter) > 0 {
		names = filterNames(names, opts.SkillFilter)
	}
	nameSet := map[string]bool{}
	for _, n := range names {
		nameSet[n] = true
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

	// target-only：目标目录里有 skill 但 canonical 没有（孤儿/外部管理）
	for _, tname := range targetOrder {
		tdir := targetDirs[tname]
		entries, err := os.ReadDir(tdir)
		if err != nil {
			// 目录不存在是正常的（该 target 未安装，无 target-only 可报）；
			// 其他错误（权限等）记录到 report，避免静默吞掉让 target-only 检测空跑。
			if !os.IsNotExist(err) {
				report.Errors = append(report.Errors, fmt.Sprintf("target %s: ReadDir %v", tname, err))
			}
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || nameSet[e.Name()] {
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
