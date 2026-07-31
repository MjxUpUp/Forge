package skillseval

// cases.go is one of the data layers of the skill eval loop: derivation, persistence, and readback of structured case sets.
//
// eval-gen originally produced only markdown checklists (EvalSkill, run manually, not persisted). The loop requires
// machine-readable, comparable case sets as regression baselines: EvalCases derives []EvalCase from the SKILL.md
// description and persists them to ~/.pi/research/skill-eval/cases/<skill>.json (atomic write).
//
// Case IDs are anchored on the unsubstituted raw trigger/skip fragment, not on the rendered prompt — so that
// evolutions of GenerateEvalPrompts's rendering rules (e.g. user-says → empty) do not cause case-ID drift en
// masse, which would otherwise be misreported as a full case-set turnover and destroy the regression signal.
//
// cases.go 是 skill eval 闭环的数据层之一：结构化 case 集的派生与存读。
//
// eval-gen 原本只产出 markdown 清单（EvalSkill，人工跑、不落盘）。闭环需要
// 可机读、可比对的 case 集作为回归基准：EvalCases 从 SKILL.md description
// 派生 []EvalCase，落盘到 ~/.pi/research/skill-eval/cases/<skill>.json（原子写）。
//
// case ID 锚定在「未替换的原始 trigger/skip 片段」上，而非渲染后的 prompt——
// 这样 GenerateEvalPrompts 的渲染规则演进（用户说→空 等替换）不会让 case ID
// 集体漂移，否则会被误报成全量 case 换血、毁掉回归信号。

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

// Two semantic kinds of cases.
//
// case 的两类语义。
const (
	KindTrigger    = "trigger"     // 该 prompt 应触发本 skill
	KindNotTrigger = "not-trigger" // 该 prompt 不应触发本 skill（误触发检测）
)

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
	CreatedAt      time.Time `json:"created_at"`
}

// CaseSet is the on-disk format of the case store. Carries DescHash for consistency checks at submit time.
//
// CaseSet 是 case 存储的磁盘格式。带 DescHash 便于 submit 时一致性校验。
type CaseSet struct {
	Skill    string     `json:"skill"`
	DescHash string     `json:"desc_hash"`
	Cases    []EvalCase `json:"cases"`
}

// EvalDir returns the root directory of the eval-cases loop data: ~/.pi/research/skill-eval.
//
// This is the independent storage path of the eval-cases system (used by skillhone),
// retaining the historical directory convention. It is a separate data line from the usage source (toollog,
// agent-neutral) — usage has switched to toollog, and migrating the eval-cases path into the forge namespace
// involves existing data migration, handled in a separate task.
//
// EvalDir 返回 eval-cases 闭环数据根目录 ~/.pi/research/skill-eval。
//
// 这是 eval-cases 体系（skillhone 用）的独立存储路径，沿用历史目录
// 约定。与 usage 数据源（toollog，agent-neutral）是不同数据线——usage 已切到 toollog，
// eval-cases 路径迁移至 forge 命名空间涉及存量数据迁移，另开 task 处理。
func EvalDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".pi", "research", "skill-eval"), nil
}

func casesFile(dir, skill string) string { return filepath.Join(dir, "cases", skill+".json") }
func runsFile(dir, skill string) string  { return filepath.Join(dir, "runs", skill+".jsonl") }
func baselinesFile(dir string) string    { return filepath.Join(dir, "baselines.json") }

// DescHash returns the sha1[:12] of the description. Used as the consistency fingerprint between the case set
// and runs — when the description changes, the old case set must be regenerated, otherwise submit will reject it.
//
// DescHash 返回 description 的 sha1[:12]。用作 case 集与 run 的一致性指纹——
// description 变更后旧 case 集必须重新生成，否则 submit 会拒绝。
func DescHash(description string) string {
	h := sha1.Sum([]byte(description))
	return hex.EncodeToString(h[:])[:12]
}

// caseID produces a stable ID using the unsubstituted raw fragment. Anchored on rawFragment rather than the
// rendered prompt, to avoid ID drift when rendering rules evolve (see file header comment).
//
// caseID 用未替换的原始片段生成稳定 ID。锚定 rawFragment 而非渲染后 prompt，
// 避免渲染规则演进导致 ID 漂移（见文件头注释）。
func caseID(skill, rawFragment string) string {
	h := sha1.Sum([]byte(skill + ":" + rawFragment))
	return hex.EncodeToString(h[:])[:12]
}

// currentDescHash reads the current description fingerprint of canonical/<skill>/SKILL.md.
//
// currentDescHash 读 canonical/<skill>/SKILL.md 的当前 description 指纹。
func currentDescHash(canonical, skill string) (string, error) {
	data, err := os.ReadFile(filepath.Join(canonical, skill, "SKILL.md"))
	if err != nil {
		return "", err
	}
	return DescHash(skillsfm.Parse(data).Description), nil
}

// EvalCases derives a structured case set from canonical/<name>/SKILL.md.
// Reuses ExtractTriggers to obtain raw trigger/skip fragments (does not rewrite extraction logic); each fragment
// is rendered into a prompt and given a stable ID computed from the raw fragment. triggers≤5, skips≤3 are
// guaranteed by ExtractTriggers.
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
// Invariant: all cases share the same DescHash (guaranteed by EvalCases — one derivation uses
// the fingerprint of the same description). CaseSet.DescHash takes the first non-empty value
// (firstDescHash). Callers must not manually splice cases with mismatched fingerprints.
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

// firstDescHash returns the first non-empty DescHash. Cases derived by EvalCases all carry the
// same DescHash, so this is the case set's description fingerprint.
//
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

// LoadCases reads the case set. A missing file returns nil,nil (the skill has never had cases generated).
//
// LoadCases 读 case 集。文件不存在返回 nil,nil（skill 未生成过 case）。
func LoadCases(dir, skill string) ([]EvalCase, error) {
	data, err := os.ReadFile(casesFile(dir, skill))
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
	return set.Cases, nil
}

// LoadCaseSet reads the full CaseSet (including DescHash), for use by submit-time validation.
//
// LoadCaseSet 读完整 CaseSet（含 DescHash），供 submit 校验用。
func LoadCaseSet(dir, skill string) (*CaseSet, error) {
	data, err := os.ReadFile(casesFile(dir, skill))
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
