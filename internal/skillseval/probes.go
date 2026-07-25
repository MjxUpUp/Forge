package skillseval

// probes.go — behavior probe 数据层：<skill>/probes.yaml 加载 + oracle 判定。
//
// behavior probe 测「skill 行为对不对」（给定输入，输出是否满足 oracle），补足现有
// trigger/not-trigger 的路由级测试——路由对不代表行为对（skill 被正确触发后，输出
// 可能仍漏检/错判）。
//
// C 组件（权限分离 + 脱敏）：oracle 是判定标准，跑 probe 的 agent 不应看到 oracle 原文
// （防过拟合 oracle 而非真正改进 skill）。forge 内部用 oracle 判定，对外（eval-cases
// 命令）只吐 ProbeInput + id + rationale，Oracle 字段 redact。物理上 oracle 与 case
// 同文件但访问层脱敏——half-automatic 的「半」靠纪律 + skill-evolution skill 指引补强。

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// <skill>/probes.yaml 的磁盘格式（人可读 + git-diff 友好）。
type probeFile struct {
	Skill  string      `yaml:"skill"`
	Probes []probeSpec `yaml:"probes"`
}

// probeSpec 是单条 behavior probe 声明。skill 作者手写，描述「给定 input，skill 输出
// 应满足 oracle」。
type probeSpec struct {
	ID        string `yaml:"id"`        // 稳定标识；空则按 input+oracle 算
	Input     string `yaml:"input"`     // 跑给 skill 的输入 prompt
	Oracle    string `yaml:"oracle"`    // contains:/not-contains:/regex:/exact:（见 judgeBehavior）
	Rationale string `yaml:"rationale"` // 为什么这个 oracle（脱敏后可显，不含答案原文）
}

// ProbesFile 返回 canonical/<skill>/probes.yaml 路径。
func ProbesFile(canonical, skill string) string {
	return filepath.Join(canonical, skill, "probes.yaml")
}

// LoadProbes 读 <skill>/probes.yaml → []EvalCase（Kind=behavior）。文件不存在返回 nil,nil。
// behavior case 的 DescHash 留空——它独立于 description 维护（description 改不影响 probe），
// 故不参与 SubmitRun 的 DescHash 一致性校验（见 SubmitRun：behavior case 跳过该校验）。
func LoadProbes(canonical, skill string) ([]EvalCase, error) {
	data, err := os.ReadFile(ProbesFile(canonical, skill))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var pf probeFile
	if err := yaml.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("parse probes.yaml: %w", err)
	}
	now := time.Now()
	out := make([]EvalCase, 0, len(pf.Probes))
	for _, p := range pf.Probes {
		id := p.ID
		if id == "" {
			id = caseID(skill, p.Input+p.Oracle)
		}
		out = append(out, EvalCase{
			ID:             id,
			Skill:          skill,
			Kind:           KindBehavior,
			Prompt:         p.Input, // behavior 的「prompt」即 input（Prompt 字段向后兼容）
			ProbeInput:     p.Input,
			Oracle:         p.Oracle,
			ProbeRationale: p.Rationale,
			CreatedAt:      now,
		})
	}
	return out, nil
}

// judgeBehavior 按 oracle 前缀判定 actualOutput 是否 pass。
//
// oracle 格式（前缀大小写不敏感）：
//
//	contains:子串       → actualOutput 含子串
//	not-contains:子串   → actualOutput 不含子串
//	regex:模式          → actualOutput 匹配正则（坏正则 → false，让 probe 失败暴露问题）
//	exact:字符串        → trim 后完全相等
//	无前缀              → 默认 contains
//
// 空输出 → false（视为未跑/未产出，不判 pass）。
func judgeBehavior(actualOutput, oracle string) bool {
	act := strings.TrimSpace(actualOutput)
	if act == "" {
		return false
	}
	spec := strings.TrimSpace(oracle)
	if spec == "" {
		return false // 无 oracle 的 probe 无法判定，不判 pass
	}
	prefix, rest, found := strings.Cut(spec, ":")
	if !found {
		return strings.Contains(act, spec) // 无前缀默认 contains
	}
	if rest == "" {
		return false // 坏 oracle（前缀无值，如 "contains:"）不假 pass
	}
	switch strings.ToLower(prefix) {
	case "contains":
		return strings.Contains(act, rest)
	case "not-contains":
		return !strings.Contains(act, rest)
	case "exact":
		return strings.TrimSpace(act) == strings.TrimSpace(rest)
	case "regex":
		re, err := regexp.Compile(rest)
		if err != nil {
			return false
		}
		return re.MatchString(act)
	default:
		// 未知前缀（如拼错的 contain: 而非 contains:）视为配置错误：return false
		// 让 probe 失败暴露问题，而非 fallback contains 静默判定（假 pass/fail 都误导）。
		// 「无前缀默认 contains」由上方 !found 分支处理，到这里必带前缀。
		return false
	}
}
