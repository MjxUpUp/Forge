// Package skillseval provides the skill eval-case lifecycle: case mining from
// real runs, golden baselines, SKIP-conflict mutex pairs, blind preamble, run
// records and regression judging.
//
// Package skillseval 提供 skill eval 案例生命周期：从实跑挖案例、黄金基线、
// SKIP 边冲突对照例、盲测 preamble、run 记录与回归判定（2026-09 普查 A4：
// 使用度量已拆至 internal/skillmetrics，本包只剩案例机器；mine 经
// skillmetrics.EngagedAfter 判定 engaged）。
package skillseval

// cases.go 是 skill eval 闭环的数据层之一：结构化 case 集的派生与存读。
//
// eval-gen 原本只产出 markdown 清单（EvalSkill，人工跑、不落盘）。闭环需要
// 可机读、可比对的 case 集作为回归基准：EvalCases 从 SKILL.md description
// 派生 []EvalCase，落盘到 <evals-root>/cases/<skill>.json（原子写）。根目录由
// ResolveDir（dir.go）解析：--dir > FORGE_EVAL_DIR > ~/.forge/evals，并从历史
// ~/.pi/research/skill-eval 一次性迁移。
//
// case ID 锚定在「未替换的原始 trigger/skip 片段」上，而非渲染后的 prompt——
// 这样 GenerateEvalPrompts 的渲染规则演进（用户说→空 等替换）不会让 case ID
// 集体漂移，否则会被误报成全量 case 换血、毁掉回归信号。
//
// 加载时两个来源合并成一个视图：人工策展黄金 case（<dir>/golden/<skill>/cases.json，
// 进 VCS，真实话语改写而非 description 自我派生，ID 带 "g-" 前缀，Origin=curated）
// 优先，派生 case（cases/<skill>.json）补充 golden 未覆盖的 ID。

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/MjxUpUp/Forge/internal/skillsfm"
	"github.com/MjxUpUp/Forge/internal/util"
)

// case 的两类语义。
const (
	KindTrigger    = "trigger"     // 该 prompt 应触发本 skill
	KindNotTrigger = "not-trigger" // 该 prompt 不应触发本 skill（误触发检测）
)

// OriginCurated 标记人工策展的黄金 case（evals/golden/<skill>/cases.json，进 VCS）。
// 派生 case（EvalCases 从 SKILL.md description 生成）Origin 留空。策展 ID 统一
// "g-" 前缀，与派生的 sha1[:12] ID 域不冲突。
const OriginCurated = "curated"

// EvalCase is a single eval test case.
//
// EvalCase 是单个 eval 测试用例。
type EvalCase struct {
	ID             string    `json:"id"`                        // sha1(skill+":"+rawFragment)[:12]，锚定原始片段
	Skill          string    `json:"skill"`                     // 本 skill 名
	Kind           string    `json:"kind"`                      // trigger | not-trigger
	Prompt         string    `json:"prompt"`                    // 渲染后的测试 prompt
	SourceFragment string    `json:"source_fragment,omitempty"` // 生成此 case 的原始 trigger/skip 片段
	Target         string    `json:"target,omitempty"`          // trigger 类 = Skill；not-trigger 类 = ""（MVP）
	DescHash       string    `json:"desc_hash,omitempty"`       // 生成时 description 的 sha1[:12]
	Origin         string    `json:"origin,omitempty"`          // "curated" = 人工策展黄金 case；空 = 派生
	CreatedAt      time.Time `json:"created_at"`
}

// CaseSet is the on-disk format of the case store.
//
// CaseSet 是 case 存储的磁盘格式。带 DescHash 便于 submit 时一致性校验。
type CaseSet struct {
	Skill    string     `json:"skill"`
	DescHash string     `json:"desc_hash"`
	Cases    []EvalCase `json:"cases"`
}

// EvalDir / ResolveDir have moved to dir.go (namespace governance + legacy migration;
// see that file for the resolution chain and the ~/.pi/research/skill-eval history).

func casesFile(dir, skill string) string { return filepath.Join(dir, "cases", skill+".json") }

// goldenFile 定位人工策展黄金集：<dir>/golden/<skill>/cases.json。黄金集在仓库里
// （evals/golden/）进 VCS；派生集在 cases/ 下、机器生成。
func goldenFile(dir, skill string) string { return filepath.Join(dir, "golden", skill, "cases.json") }
func runsFile(dir, skill string) string   { return filepath.Join(dir, "runs", skill+".jsonl") }
func baselinesFile(dir string) string     { return filepath.Join(dir, "baselines.json") }

// RunsFile / BaselinesFile export the storage paths so read-side consumers (dashboard cache) can fingerprint the files for mtime-based invalidation without duplicating the path layout — single truth stays here.
//
// RunsFile / BaselinesFile 导出存储路径，让读侧消费者（dashboard 缓存）能对文件做
// mtime 指纹失效判断，而不必复制路径布局——单一真相仍在本包。
func RunsFile(dir, skill string) string { return runsFile(dir, skill) }
func BaselinesFile(dir string) string   { return baselinesFile(dir) }

// DescHash returns the sha1[:12] of the description.
//
// DescHash 返回 description 的 sha1[:12]。用作 case 集与 run 的一致性指纹——
// description 变更后旧 case 集必须重新生成，否则 submit 会拒绝。
func DescHash(description string) string {
	h := sha1.Sum([]byte(description))
	return hex.EncodeToString(h[:])[:12]
}

// caseID 用未替换的原始片段生成稳定 ID。锚定 rawFragment 而非渲染后 prompt，
// 避免渲染规则演进导致 ID 漂移（见文件头注释）。
func caseID(skill, rawFragment string) string {
	h := sha1.Sum([]byte(skill + ":" + rawFragment))
	return hex.EncodeToString(h[:])[:12]
}

// currentDescHash 读 canonical/<skill>/SKILL.md 的当前 description 指纹。
func currentDescHash(canonical, skill string) (string, error) {
	data, err := os.ReadFile(filepath.Join(canonical, skill, "SKILL.md"))
	if err != nil {
		return "", err
	}
	return DescHash(skillsfm.Parse(data).Description), nil
}

// EvalCases derives a structured case set from canonical/<name>/SKILL.md.
//
// EvalCases 从 canonical/<name>/SKILL.md 派生结构化 case 集。
// 复用 ExtractTriggers 拿原始 trigger/skip 片段（不重写提取逻辑），对每个片段
// 渲染出 prompt、用原始片段算稳定 ID。triggers≤5、skips≤3 由 ExtractTriggers 保证。
func EvalCases(canonical, name string) ([]EvalCase, error) {
	data, err := os.ReadFile(filepath.Join(canonical, name, "SKILL.md"))
	if err != nil {
		return nil, err
	}
	desc := skillsfm.Parse(data).Description
	dh := DescHash(desc)
	triggers, skips := ExtractTriggers(desc)
	now := time.Now()

	cases := make([]EvalCase, 0, len(triggers)+len(skips))
	for _, raw := range triggers {
		cases = append(cases, EvalCase{
			ID:             caseID(name, raw),
			Skill:          name,
			Kind:           KindTrigger,
			Prompt:         renderTriggerPrompt(raw),
			SourceFragment: raw,
			Target:         name,
			DescHash:       dh,
			CreatedAt:      now,
		})
	}
	for _, raw := range skips {
		cases = append(cases, EvalCase{
			ID:             caseID(name, raw),
			Skill:          name,
			Kind:           KindNotTrigger,
			Prompt:         renderSkipPrompt(raw),
			SourceFragment: raw,
			Target:         "",
			DescHash:       dh,
			CreatedAt:      now,
		})
	}
	return cases, nil
}

// SaveCases atomically writes the case set to dir/cases/<skill>.json. An empty set is a no-op (no empty file is written).
//
// SaveCases 原子写 case 集到 dir/cases/<skill>.json。空集视为无操作（不写空文件）。
//
// 不变量：所有 case 同 DescHash（由 EvalCases 保证——一次派生用同一 description 的
// 指纹）。CaseSet.DescHash 取首个非空（firstDescHash）。调用方不应手工拼接异指纹的 case。
func SaveCases(dir, skill string, cases []EvalCase) error {
	if len(cases) == 0 {
		return nil
	}
	set := CaseSet{
		Skill:    skill,
		DescHash: firstDescHash(cases),
		Cases:    cases,
	}
	data, err := json.MarshalIndent(set, "", "  ")
	if err != nil {
		return err
	}
	return util.AtomicWrite(casesFile(dir, skill), data, 0644)
}

// firstDescHash 返回首个非空 DescHash。EvalCases 派生的 case 都带同一 DescHash，
// 此即 case 集的 description 指纹。
func firstDescHash(cases []EvalCase) string {
	for _, c := range cases {
		if c.DescHash != "" {
			return c.DescHash
		}
	}
	return ""
}

// LoadCases reads the merged case set: golden (curated) first, derived cases supplementing any IDs the golden set does not cover (golden wins on ID conflict).
//
// LoadCases 读合并后的 case 集：golden（策展）优先，派生 case 补充 golden 未覆盖的 ID
// （同 ID golden 胜出）。两个文件都不存在返回 nil,nil（skill 未生成过 case）。
func LoadCases(dir, skill string) ([]EvalCase, error) {
	set, err := LoadCaseSet(dir, skill)
	if err != nil || set == nil {
		return nil, err
	}
	return set.Cases, nil
}

// LoadCaseSet reads the full merged CaseSet (including DescHash), for use by submit-time validation.
//
// LoadCaseSet 读完整合并 CaseSet（含 DescHash），供 submit 校验用。合并后的 DescHash
// 来自派生 case（firstDescHash）：策展 case 锚定真实话语而非 description，不带
// DescHash，纯 golden 集不会因 description 变更而过期。
func LoadCaseSet(dir, skill string) (*CaseSet, error) {
	golden, err := readCaseSet(goldenFile(dir, skill))
	if err != nil {
		return nil, err
	}
	derived, err := readCaseSet(casesFile(dir, skill))
	if err != nil {
		return nil, err
	}
	if golden == nil && derived == nil {
		return nil, nil
	}
	merged := &CaseSet{Skill: skill}
	seen := make(map[string]bool)
	for _, c := range goldenCases(golden) {
		merged.Cases = append(merged.Cases, c)
		seen[c.ID] = true
	}
	if derived != nil {
		for _, c := range derived.Cases {
			if seen[c.ID] {
				continue
			}
			merged.Cases = append(merged.Cases, c)
		}
	}
	merged.DescHash = firstDescHash(merged.Cases)
	return merged, nil
}

// goldenCases 把可能为 nil 的 golden 集压平，供合并循环用。
func goldenCases(set *CaseSet) []EvalCase {
	if set == nil {
		return nil
	}
	return set.Cases
}

// readCaseSet 读单个 CaseSet 文件。文件不存在返回 nil,nil。
func readCaseSet(path string) (*CaseSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var set CaseSet
	if err := json.Unmarshal(data, &set); err != nil {
		return nil, err
	}
	return &set, nil
}
