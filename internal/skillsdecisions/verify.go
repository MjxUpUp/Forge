package skillsdecisions

// verify.go — prediction→验证闭环（AHE 决策可观测，支柱 3）。
//
// 决策四元组（诊断/修订/证据/结果）是回溯式的——记录的是事后观察到什么。VerifyDecision
// 闭环前瞻的那一半：修改时刻声明的 Prediction（「信号 X 应改善」）被下一轮真实结果回填
// 对账（命中/未命中/不可判）。预测被证伪的修改可见地被证伪；下轮 agent 看到的是哪些方向
// 被证伪，而不只是哪些被「接受」。
//
// 边界：对人工/agent 驱动的修改做审计/可复现，不是自主进化——Forge 记录并验证预测；
// 不自行接受或拒绝 harness 变更（验收门在 skillseval.JudgeSkillAccept / forge skills battery）。

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/skillsfm"
	"github.com/MjxUpUp/Forge/internal/util"
)

// VerifyDecision backfills a verification onto an existing decision in decisions.md: appends a `- **VerifiedAt**` field line into the section's field list and a `### Verification` subsection at the section end, in place.
//
// VerifyDecision 把验证回填到 decisions.md 的一条既有决策上：向该 section 的字段列表
// 追加 `- **VerifiedAt**` 字段行，在 section 末尾追加 `### Verification` 子节，原地完成。
//
// 原地 section 补丁（而非全文件重渲染）：decisions.md 是 append-only 档案，可能带手写
// 批注；从解析结果整体重渲染会静默丢掉容错解析器跳过的一切。只动目标 section，
// 其余字节逐字保留。
//
// 每条决策只验证一次：对已验证决策再验证是错误。档案精神上是 append-only——第二次
// 意见应记一条引用前者的新决策（确需改就手改验证文本），而非静默改写历史。
// fail-closed：文件缺失、决策 ID 未知、result 为空、skill 名非法（路径遍历守卫，
// 同 Append/Load）。
func VerifyDecision(canonical, skill, decisionID, result string, verifiedAt time.Time) error {
	if !skillsfm.IsValidSkillName(skill) {
		return fmt.Errorf("invalid skill name %q", skill)
	}
	if decisionID == "" {
		return fmt.Errorf("decision id 不能为空")
	}
	if strings.TrimSpace(result) == "" {
		return fmt.Errorf("验证结果不能为空")
	}
	if verifiedAt.IsZero() {
		verifiedAt = time.Now()
	}
	path := DecisionsFile(canonical, skill)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("skill %q 无 decisions.md——先 forge skills decide 记录决策", skill)
		}
		return err
	}
	lines := strings.Split(string(data), "\n")

	// 定位目标 section [start, end)：start = `## [<id>]` 头行，end = 下一个 `## ` 行或 EOF。
	headerPrefix := `## [` + decisionID + `]`
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, headerPrefix) {
			start = i
			break
		}
	}
	if start < 0 {
		return fmt.Errorf("决策 %q 不在 %s 的 decisions.md——用 'forge skills decide' 查看可用决策 ID", decisionID, skill)
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], `## `) {
			end = i
			break
		}
	}

	// 已验证守卫 + 插入锚点，均限定在 [start, end) 内。
	lastField := -1    // last `- **Field**:` line in the contiguous field block at the section top
	fieldBlock := true // false once the first `### ` subsection starts
	for i := start + 1; i < end; i++ {
		line := lines[i]
		// 两种已验证形态都守：forge 写的 `- **VerifiedAt**` 字段行，与手写无字段行的
		// `### Verification` 子节。漏检的形态会让二次 verify 追加重复子节，解析器的
		// last-wins 静默偏好后者——台账自相矛盾。
		if strings.HasPrefix(line, `- **VerifiedAt**`) || strings.HasPrefix(line, `### Verification`) {
			return fmt.Errorf("决策 %q 已验证过（每条决策只验证一次；二次意见记新决策引用它）", decisionID)
		}
		if strings.HasPrefix(line, `### `) {
			// 子节正文从这里开始。字段行只存在于首个子节之前——Diagnosis/Evidence 正文里的
			// `- **粗体**: 值` 列表项是内容不是字段（parseDecisions 只在 ### 子节前取字段）。
			// 把 VerifiedAt 锚到那里会插进正文，永远解析不回来。
			fieldBlock = false
			continue
		}
		if fieldBlock && strings.HasPrefix(line, `- **`) {
			lastField = i
		}
	}

	// VerifiedAt 字段：插在连续字段块最后一个字段行之后（字段保持 section 顶部，对齐
	// formatDecision 布局）。尚无字段行（手写极简 section）→ 紧跟头行。
	fieldAt := lastField + 1
	if lastField < 0 {
		fieldAt = start + 1
	}
	verifiedLine := `- **VerifiedAt**: ` + verifiedAt.UTC().Format(time.RFC3339)

	// Verification 子节：插在 section 末尾。唯一的尾部 "" 两种情形都承重——文件中部
	// 它成为下一个 `## ` 头前的空行分隔（section 自身的尾空行位于 end-1，已在
	// lines[:end] 里）；EOF 时经 strings.Join 成为文件末换行。不加条件性额外空行：
	// section 本就以一个空行收尾，再补一个会在下一个头前留下双空行。
	verifBlock := []string{
		`### Verification`,
		``,
		strings.TrimRight(result, "\n"),
		``,
	}
	// 手改 section 可能缺 forge 必写的尾空行（F5）：此时代入一个分隔空行，`### Verification`
	// 不至于紧贴正文。
	if end-1 > start && lines[end-1] != "" {
		verifBlock = append([]string{``}, verifBlock...)
	}

	// 拼接：一次遍历建新切片（先插字段、再在旧 section 末尾插子节——索引基于原切片，
	// 先插靠后的位置保前面的索引仍有效）。
	out := make([]string, 0, len(lines)+len(verifBlock)+1)
	out = append(out, lines[:end]...)
	out = append(out, verifBlock...)
	out = append(out, lines[end:]...)
	// 插入 VerifiedAt 字段行（fieldAt 恒 <= end：字段锚点在 section 内）。
	out = append(out[:fieldAt], append([]string{verifiedLine}, out[fieldAt:]...)...)

	return util.AtomicWrite(path, []byte(strings.Join(out, "\n")), 0644)
}
