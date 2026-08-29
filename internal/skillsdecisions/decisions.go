package skillsdecisions

// skillsdecisions — persistent per-skill decision history: the decision archive of each
// skill across multiple tasks/rounds of optimization, independent of per-task TaskState
// (cleared by PruneOldTasks on task end, and would pollute the resume view).
//
// A decision is recorded as a 4-tuple (diagnosis, revision, evidence, outcome) plus
// rationale and an associated commit/probe-run. Stored in the skill directory's
// decisions.md (markdown single source of truth: human+machine readable, git-diff
// friendly, shared with the skill) — decision history is part of the skill repo (whole-skill).
//
// Boundary: audit/reproducibility (let the next round agent understand the why), not
// generalized learning — does not violate Forge's decision to tear down the
// experience/knowledge loop (2026-07-09, 8cedc80).
//
// skillsdecisions — skill 级持久决策历史：每个 skill 历经多任务/多轮优化的决策档案，
// 独立于 per-task TaskState（task 结束会被 PruneOldTasks 清，且会污染 resume 视图）。
//
// 决策记录为四元组 (diagnosis, revision, evidence, outcome) + rationale + 关联
// commit/probe-run。存 skill 目录 decisions.md（markdown 单一真相源：人机可读 +
// git diff 友好 + 随 skill 共享）——决策历史是 skill repo 的一部分（whole-skill）。
//
// 边界：审计/可复现（让下一轮 agent 理解 why），不是泛化学习——不违背 Forge
// 拆除 experience/knowledge 闭环的决策（2026-07-09, 8cedc80）。

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/skillsfm"
	"github.com/MjxUpUp/Forge/internal/util"
)

// Four outcome states of a decision (o_t in {accept, revise, reject, defer}).
//
// 决策结果四态（o_t ∈ {accept, revise, reject, defer}）。
const (
	OutcomeAccept = "accept" // 修订接受，落地保留
	OutcomeReject = "reject" // 修订拒绝，撤销
	OutcomeRevise = "revise" // 修订需再改，继续迭代
	OutcomeDefer  = "defer"  // 诊断悬置，暂不决策
)

// ValidOutcome reports whether s is a valid outcome.
//
// ValidOutcome 判断是否合法 outcome。
func ValidOutcome(s string) bool {
	switch s {
	case OutcomeAccept, OutcomeReject, OutcomeRevise, OutcomeDefer:
		return true
	}
	return false
}

// SkillDecision is a single skill decision record (4-tuple plus metadata).
//
// Fields map onto the decision record h_t = (q_t, r_t, e_t, o_t):
//   - Diagnosis  = q_t (diagnosis: what failure mode / problem)
//   - Revision   = r_t (candidate revision: what changed)
//   - Evidence   = e_t (redacted evaluation evidence: probe pass rate / regression
//     comparison / diagnostic clues)
//   - Outcome    = o_t (accept/reject/revise/defer)
//
// CommitHash / ProbeRunID anchor the decision to a specific git commit and eval/probe
// run, supporting scoped revert (component D) and evidence traceback.
//
// SkillDecision 是单条 skill 决策记录（四元组 + 元数据）。
//
// 字段对应 decision record h_t = (q_t, r_t, e_t, o_t)：
//   - Diagnosis  = q_t（诊断：什么失败模式/问题）
//   - Revision   = r_t（候选修订：改了什么）
//   - Evidence   = e_t（评估证据：pass-rate / 回归比对 / 诊断线索）
//   - Outcome    = o_t（accept/reject/revise/defer）
//
// CommitHash / ProbeRunID 把决策锚到具体 git commit 和 eval run，支撑 scoped
// revert（D 组件）和证据回溯。
type SkillDecision struct {
	ID         string    `json:"id"`                     // d-<unixnano>-<randhex>
	Skill      string    `json:"skill"`                  // skill 名（== 目录名）
	Diagnosis  string    `json:"diagnosis"`              // 诊断
	Revision   string    `json:"revision"`               // 候选修订
	Evidence   string    `json:"evidence"`               // 脱敏评估证据
	Outcome    string    `json:"outcome"`                // accept|reject|revise|defer
	Rationale  string    `json:"rationale,omitempty"`    // 为什么这个 outcome（结合背景）
	CommitHash string    `json:"commit_hash,omitempty"`  // 修订关联 git commit（scoped revert 锚点）
	ProbeRunID string    `json:"probe_run_id,omitempty"` // 关联 eval/probe run
	By         string    `json:"by,omitempty"`           // 来源（claude-code/codex/...）
	DecidedAt  time.Time `json:"decided_at"`
	// Prediction is the testable prediction declared at edit time (AHE decision observability):
	// which observable signal should improve if the revision works. Declared BEFORE outcomes are
	// known; verified later by VerifyDecision — turns every edit into a falsifiable contract
	// instead of a retrospective narrative. Optional (legacy decisions predate the field).
	//
	// Prediction 是修改时刻声明的可检验预测（AHE 决策可观测）：修订若有效，哪个可观测信号
	// 应改善。在结果已知之前声明，之后由 VerifyDecision 回填对账——让每次修改成为可证伪
	// 契约，而非事后叙述。可选（存量决策早于该字段）。
	Prediction string `json:"prediction,omitempty"`
	// Verification is the backfilled verification of Prediction: what actual outcomes showed
	// (hit / miss / inconclusive + numbers). Empty until VerifyDecision fills it.
	//
	// Verification 是 Prediction 的回填验证：实际结果如何（命中/未命中/不可判 + 数字）。
	// VerifyDecision 回填前为空。
	Verification string `json:"verification,omitempty"`
	// VerifiedAt is when the verification was backfilled.
	//
	// VerifiedAt 是验证回填时刻。
	VerifiedAt time.Time `json:"verified_at,omitempty"`
}

// NewDecisionID generates a d-<unixnano-hex>-<randhex> identifier. Cross-process collision
// resistance comes from nanosecond time plus crypto/rand.
// The d prefix aligns with the taskpipeline continuity ID convention (d = decision).
//
// NewDecisionID 生成"d-<unixnano-hex>-<randhex>"。跨进程去碰撞：nano 时间 + crypto/rand。
// 前缀 d 对齐 taskpipeline continuity ID 约定（d=decision）。
func NewDecisionID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("d-%x-%s", time.Now().UnixNano(), hex.EncodeToString(b[:]))
}

// DecisionsFile returns the path canonical/<skill>/decisions.md.
//
// DecisionsFile 返回 canonical/<skill>/decisions.md 路径。
func DecisionsFile(canonical, skill string) string {
	return filepath.Join(canonical, skill, "decisions.md")
}

// LoadDecisions reads decisions.md and parses it into a decision list in write order.
// A missing file returns nil, nil. The skill name is validated (skillsfm.IsValidSkillName)
// before being joined into the path — it comes from external input (--skill flag), and an
// unvalidated name would be a path traversal into directories outside canonical.
//
// LoadDecisions 读 decisions.md 解析为按写入序的决策列表。文件不存在返回 nil,nil。
// skill 名先过 skillsfm.IsValidSkillName 校验再拼路径——它来自外部输入（--skill
// flag），不校验就是路径遍历写出 canonical 之外。
func LoadDecisions(canonical, skill string) ([]SkillDecision, error) {
	if !skillsfm.IsValidSkillName(skill) {
		return nil, fmt.Errorf("invalid skill name %q", skill)
	}
	data, err := os.ReadFile(DecisionsFile(canonical, skill))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return parseDecisions(string(data)), nil
}

// AppendDecision appends one decision to the end of decisions.md. It fills in
// ID/Skill/DecidedAt if empty and writes atomically.
// If the file does not exist it is created (with a header). Read-modify-write:
// decisions.md is a low-frequency development-time file, edited serially by convention
// (same convention as skillseval SetBaseline); add a file lock if high-frequency
// concurrency ever arises.
//
// AppendDecision 追加一条决策到 decisions.md 末尾。填 ID/Skill/DecidedAt（若空），原子写。
// 文件不存在则创建（带 header）。读-改-写：decisions.md 是开发期低频文件，约定串行
// 编辑（同 skillseval SetBaseline 的约定）；高频并发场景再加文件锁。
// skill 名同样先过 skillsfm.IsValidSkillName（同 LoadDecisions，防路径遍历）。
func AppendDecision(canonical, skill string, d SkillDecision) error {
	if !skillsfm.IsValidSkillName(skill) {
		return fmt.Errorf("invalid skill name %q", skill)
	}
	if d.ID == "" {
		d.ID = NewDecisionID()
	}
	if d.DecidedAt.IsZero() {
		d.DecidedAt = time.Now()
	}
	if d.Skill == "" {
		d.Skill = skill
	}
	path := DecisionsFile(canonical, skill)
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var sb strings.Builder
	if len(existing) == 0 {
		sb.WriteString(header(skill))
	} else {
		sb.Write(existing)
		if !strings.HasSuffix(string(existing), "\n") {
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n" + formatDecision(d))
	return util.AtomicWrite(path, []byte(sb.String()), 0644)
}

// header is the opening explanation for a freshly created decisions.md (not a decision
// section; parseDecisions skips it).
//
// header 是新建 decisions.md 的开头说明（非决策 section，parseDecisions 跳过）。
func header(skill string) string {
	return fmt.Sprintf("# %s — 持久决策历史\n\n"+
		"persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent "+
		"理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。"+
		"append-only：新决策追加到末尾。\n", skill)
}

// formatDecision renders a single decision as a markdown section (the write unit of
// decisions.md).
// Fixed fields use list items (- **Field**: value); multi-line content uses ### sub-sections
// — parseDecisions parses them symmetrically. Free-text fields are passed through
// escapeDecisionText first (see its comment).
//
// formatDecision 把单条决策渲染成 markdown section（decisions.md 的写入单元）。
// 固定字段用列表项（- **Field**: value），多行内容用 ### 子节——parseDecisions 对称解析。
// 自由文本字段先过 escapeDecisionText（见其注释）。
func formatDecision(d SkillDecision) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## [%s] %s\n\n", d.ID, d.Outcome)
	b.WriteString("- **Skill**: " + d.Skill + "\n")
	if !d.DecidedAt.IsZero() {
		b.WriteString("- **DecidedAt**: " + d.DecidedAt.UTC().Format(time.RFC3339) + "\n")
	}
	if d.By != "" {
		b.WriteString("- **By**: " + d.By + "\n")
	}
	if d.CommitHash != "" {
		b.WriteString("- **Commit**: " + d.CommitHash + "\n")
	}
	if d.ProbeRunID != "" {
		b.WriteString("- **ProbeRun**: " + d.ProbeRunID + "\n")
	}
	if !d.VerifiedAt.IsZero() {
		b.WriteString("- **VerifiedAt**: " + d.VerifiedAt.UTC().Format(time.RFC3339) + "\n")
	}
	b.WriteString("\n### Diagnosis\n\n" + escapeDecisionText(strings.TrimRight(d.Diagnosis, "\n")) + "\n\n")
	b.WriteString("### Revision\n\n" + escapeDecisionText(strings.TrimRight(d.Revision, "\n")) + "\n\n")
	// Prediction renders between Revision and Evidence: it is declared at edit time as part of
	// the change contract, while Evidence is the evaluation data backing the decision.
	//
	// Prediction 渲染在 Revision 与 Evidence 之间：它与修改同时声明、属变更契约的一部分，
	// 而 Evidence 是支撑决策的评估数据。
	if d.Prediction != "" {
		b.WriteString("### Prediction\n\n" + escapeDecisionText(strings.TrimRight(d.Prediction, "\n")) + "\n\n")
	}
	b.WriteString("### Evidence\n\n" + escapeDecisionText(strings.TrimRight(d.Evidence, "\n")) + "\n")
	if d.Rationale != "" {
		b.WriteString("\n### Rationale\n\n" + escapeDecisionText(strings.TrimRight(d.Rationale, "\n")) + "\n")
	}
	if d.Verification != "" {
		b.WriteString("\n### Verification\n\n" + escapeDecisionText(strings.TrimRight(d.Verification, "\n")) + "\n")
	}
	return b.String()
}

// decisionEscapePrefix is U+2060 (WORD JOINER, zero-width no-break space) — the
// line-start escape marker for free-text fields. Invisible in editors/renderers
// and legal inside markdown prose, so escaped lines stay human-readable while no
// longer starting with the structural characters parseDecisions keys on.
//
// decisionEscapePrefix 是 U+2060（WORD JOINER，零宽不换行空格）——自由文本字段的
// 行首转义标记。编辑器/渲染器中不可见且在 markdown 正文里合法，转义行保持人类
// 可读，同时不再以 parseDecisions 所识别的结构字符开头。
const decisionEscapePrefix = "\u2060"

// escapeDecisionText prefixes every structurally ambiguous line of a free-text
// field with decisionEscapePrefix. parseDecisions keys on line-start markers —
// `## [d-...` opens a decision, any `## `/`### ` heading switches or ends
// sections, `- **Field**:` extracts fields — so free text containing such lines
// (e.g. a diagnosis quoting `## [d-evil] accept`) would otherwise forge a
// PHANTOM decision and corrupt the round-trip. Prefixing # (covers #/##/###),
// `- **`, and decisionEscapePrefix itself defuses exactly those markers;
// parseDecisionText strips one prefix per line symmetrically (a value
// legitimately starting with U+2060 must be escaped too, so it keeps its own
// through the round trip — escape adds one, unescape removes one).
//
// escapeDecisionText 给自由文本字段里每一行结构歧义行加 decisionEscapePrefix
// 前缀。parseDecisions 以行首标记切分——`## [d-...` 开决策、任意 `## `/`### ` 标题
// 切换/结束小节、`- **Field**:` 取字段——自由文本含这类行（如诊断里引用
// `## [d-evil] accept`）会伪造出幻影决策并破坏往返。给 #（覆盖 #/##/###）、
// `- **` 以及 decisionEscapePrefix 本身加前缀恰好拆掉这些标记；parseDecisionText
// 对称地每行剥掉一个前缀（本身以 U+2060 开头的值也必须转义，才能在往返中保留
// 自己的那一个——转义加一、还原减一）。
func escapeDecisionText(s string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if strings.HasPrefix(ln, "#") || strings.HasPrefix(ln, "- **") || strings.HasPrefix(ln, decisionEscapePrefix) {
			lines[i] = decisionEscapePrefix + ln
		}
	}
	return strings.Join(lines, "\n")
}

// parseDecisionText reverses escapeDecisionText line by line: strip ONE leading
// decisionEscapePrefix. Lines without the prefix pass through unchanged.
//
// parseDecisionText 逐行反转 escapeDecisionText：剥掉一个行首
// decisionEscapePrefix。无前缀的行原样通过。
func parseDecisionText(s string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimPrefix(ln, decisionEscapePrefix)
	}
	return strings.Join(lines, "\n")
}

// parseDecisions parses the full decisions.md text into []SkillDecision.
// Sections are split at ^## [d-...]; within a section, - **Field**: value extracts fields
// and ### Subsection extracts multi-line content (unescaped via parseDecisionText —
// the symmetric inverse of the write side's escapeDecisionText). Sections that fail to
// parse are skipped (fault-tolerant, no panic) — decisions.md is primarily for humans/agents
// to read; parsing only supports scoped revert / list display, so losing one entry is not fatal.
//
// parseDecisions 解析 decisions.md 全文为 []SkillDecision。
// 按"^## [d-...]"切 section；section 内"- **Field**: value"取字段，"### Subsection"
// 取多行内容（经 parseDecisionText 还原——写入侧 escapeDecisionText 的对称逆）。
// 无法解析的 section 跳过（容错，不 panic）——decisions.md 主要给人/agent
// 读，解析只为 scoped revert / 列表展示，丢一条不致命。
func parseDecisions(md string) []SkillDecision {
	lines := strings.Split(md, "\n")
	var out []SkillDecision
	var cur *SkillDecision
	var body strings.Builder
	var section string // 当前多行子节名（Diagnosis/Revision/Evidence/Rationale）

	flushBody := func() {
		if cur == nil || section == "" {
			return
		}
		// Unescape AFTER TrimSpace: the U+2060 escape marker is not unicode
		// whitespace, so TrimSpace never eats it; stripping it per line here is
		// the symmetric inverse of escapeDecisionText on the write side.
		//
		// 先 TrimSpace 再还原：U+2060 转义标记不是 unicode 空白，TrimSpace 不会
		// 吃掉它；此处逐行剥离是写入侧 escapeDecisionText 的对称逆操作。
		content := parseDecisionText(strings.TrimSpace(body.String()))
		switch section {
		case "Diagnosis":
			cur.Diagnosis = content
		case "Revision":
			cur.Revision = content
		case "Evidence":
			cur.Evidence = content
		case "Rationale":
			cur.Rationale = content
		case "Prediction":
			cur.Prediction = content
		case "Verification":
			cur.Verification = content
		}
		body.Reset()
	}

	flushDecision := func() {
		if cur == nil {
			return
		}
		flushBody()
		section = ""
		if cur.ID != "" {
			out = append(out, *cur)
		}
		cur = nil
	}

	for _, line := range lines {
		// Any level-2 heading (## ) ends the current decision — prevents the lines that
		// follow a non-decision stray ## heading (e.g. a typoed ## [BAD-SECTION or a
		// future level-2 subsection) from being absorbed into the current decision's body
		// (subsection content), which would silently pollute the previous decision's
		// Evidence/Rationale.
		//
		// 任意 2 级标题（## ）都结束当前决策——避免非决策的 stray ## 标题（如手误的
		// ## [BAD-SECTION 或未来扩展的 2 级小节）的后续行被吸进当前决策的 body（子节
		// 内容），导致前一条决策的 Evidence/Rationale 静默污染。
		if strings.HasPrefix(line, "## ") {
			if !strings.HasPrefix(line, "## [d-") {
				// Non-decision level-2 heading: flush the current decision and drop this line
				// (open no new decision, write nothing to the body).
				//
				// 非决策头的 2 级标题：flush 当前决策，丢弃这行（不开新决策、不进 body）。
				flushDecision()
				continue
			}
			// Decision header: ## [d-xxx] outcome
			//
			// 决策头：## [d-xxx] outcome
			flushDecision()
			rest := strings.TrimPrefix(line, "## [")
			idx := strings.Index(rest, "]")
			if idx < 0 {
				continue
			}
			id := rest[:idx]
			outcome := normalizeOutcome(rest[idx+1:])
			cur = &SkillDecision{ID: id, Outcome: outcome}
			section = ""
			continue
		}
		if cur == nil {
			continue // header / 段落说明，跳过
		}
		// Subsection switch.
		//
		// 子节切换
		if strings.HasPrefix(line, "### ") {
			flushBody()
			section = strings.TrimSpace(strings.TrimPrefix(line, "### "))
			body.Reset()
			continue
		}
		// Field list item (only recognized outside subsections, to avoid treating list items
		// inside subsection prose as fields).
		//
		// 字段列表项（只在非子节段识别，避免把子节正文里的列表项误当字段）
		if section == "" && strings.HasPrefix(line, "- **") {
			parseField(cur, line)
			continue
		}
		// Multi-line subsection content.
		//
		// 多行子节内容
		if section != "" {
			body.WriteString(line)
			body.WriteString("\n")
		}
	}
	flushDecision()
	return out
}

// normalizeOutcome parses the outcome token from a decision header, tolerating trailing
// annotations typed by mistake — e.g. a parenthetical caveat after the outcome, or a
// trailing note, both resolve to the bare outcome token.
//
// When no valid outcome can be extracted, the original value is preserved (no data loss;
// the consumer decides).
//
// normalizeOutcome 解析决策头的 outcome token，容错手误的尾注：
//
//	## [d-x] accept (with caveat) → accept
//	## [d-x] accept  备注 → accept
//
// 无法提取合法 outcome 时保留原值（不丢数据，consumer 自行判断）。
func normalizeOutcome(raw string) string {
	outcome := strings.TrimSpace(raw)
	if ValidOutcome(outcome) {
		return outcome
	}
	// Strip the trailing annotation after the first whitespace or open-paren and check
	// whether the leading token is a valid outcome.
	//
	// 剥离首个空白或左括号后的尾注，看首 token 是否合法 outcome。
	if i := strings.IndexAny(outcome, " \t("); i > 0 {
		cand := strings.TrimSpace(outcome[:i])
		if ValidOutcome(cand) {
			return cand
		}
	}
	return outcome
}

// parseField parses a - **Field**: value list item into the decision.
//
// parseField 解析"- **Field**: value"列表项到决策。
func parseField(d *SkillDecision, line string) {
	rest := strings.TrimPrefix(line, "- **")
	idx := strings.Index(rest, "**:")
	if idx < 0 {
		return
	}
	field := rest[:idx]
	val := strings.TrimSpace(rest[idx+3:])
	switch field {
	case "Skill":
		d.Skill = val
	case "DecidedAt":
		if t, err := time.Parse(time.RFC3339, val); err == nil {
			d.DecidedAt = t
		}
	case "By":
		d.By = val
	case "Commit":
		d.CommitHash = val
	case "ProbeRun":
		d.ProbeRunID = val
	case "VerifiedAt":
		if t, err := time.Parse(time.RFC3339, val); err == nil {
			d.VerifiedAt = t
		}
	}
}
