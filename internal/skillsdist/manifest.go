// Package skillsdist 实现 skill 库的分发：把 canonical 源同步到各工具目标目录
// （pi/claude/cursor 的全局位置或项目 .claude/skills），支持 link 与 copy 两种模式，
// 检测分发分叉（drift），部署 skill-routing adapter 单文件，维护 manifest 状态。
// 1:1 对齐 SkillsHub admin/scripts/sync.py 的语义。
package skillsdist

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/MjxUpUp/Forge/internal/skillsqa"
	"github.com/MjxUpUp/Forge/internal/util"
)

// Manifest 是分发状态清单（对齐 registry.py manifest schema）。
// 落点改为 ~/.forge/skills-manifest.json（统一 forge 全局根，不放 ~/.agents/）。
type Manifest struct {
	Version         string              `json:"version"`
	GeneratedAt     string              `json:"generated_at"`
	CanonicalRoot   string              `json:"canonical_root"`
	Targets         map[string]string   `json:"targets"`
	Stats           ManifestStats       `json:"stats"`
	TargetOnly      map[string][]string `json:"target_only"`
	ExternalManaged map[string][]string `json:"external_managed"`
	Skills          []ManifestSkill     `json:"skills"`
}

// ManifestStats 是 manifest 的统计摘要。
type ManifestStats struct {
	Total  int `json:"total"`
	Pass   int `json:"pass"`
	Issues int `json:"issues"`
}

// ManifestSkill 是 manifest 里每个 skill 的条目：skillsqa 报告 + 分发目标状态。
type ManifestSkill struct {
	skillsqa.SkillReport
	Targets map[string]string `json:"targets"`
}

// ManifestPath 返回 ~/.forge/skills-manifest.json。
func ManifestPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".forge", "skills-manifest.json"), nil
}

// SaveManifest 把 manifest 写到 ~/.forge/skills-manifest.json。
func SaveManifest(m *Manifest) error {
	path, err := ManifestPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return util.AtomicWrite(path, data, 0644)
}

// LoadManifest 从 ~/.forge/skills-manifest.json 读取。
func LoadManifest() (*Manifest, error) {
	path, err := ManifestPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// BuildManifest 扫描 canonical 全量 skill，组装分发状态清单（registry 报告 + 各目标态）。
// install 成功后调用并 SaveManifest，留作"上次安装快照"供 system 健康检查与查询。
// 与 install 的质量门控独立重新 audit：manifest 反映 canonical 全量，而非本次 install 子集。
func BuildManifest(canonical string, opts InstallOpts) (*Manifest, error) {
	names, err := ListSkills(canonical)
	if err != nil {
		return nil, err
	}
	targetDirs, err := TargetDirs(opts.Targets, opts.Global, opts.ProjectSkillsDir)
	if err != nil {
		return nil, err
	}
	targetOrder := orderedTargetNames(targetDirs)

	skills := make([]ManifestSkill, 0, len(names))
	passCount := 0
	for _, name := range names {
		skillDir := filepath.Join(canonical, name)
		targets := map[string]string{}
		for _, t := range targetOrder {
			targets[t] = detectState(skillDir, filepath.Join(targetDirs[t], name))
		}
		ms := ManifestSkill{Targets: targets}
		if rep, aerr := skillsqa.AuditSkill(skillDir); aerr == nil {
			ms.SkillReport = *rep
			if rep.Pass {
				passCount++
			}
		} else {
			// SKILL.md 不可读：仅记名，不阻塞 manifest 生成
			ms.Name = name
		}
		skills = append(skills, ms)
	}

	targetsSummary := map[string]string{}
	for _, t := range targetOrder {
		targetsSummary[t] = targetDirs[t]
	}

	return &Manifest{
		Version:       "1",
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		CanonicalRoot: canonical,
		Targets:       targetsSummary,
		Stats: ManifestStats{
			Total:  len(skills),
			Pass:   passCount,
			Issues: len(skills) - passCount,
		},
		Skills: skills,
	}, nil
}
