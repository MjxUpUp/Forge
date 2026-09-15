// Package doclint is the L1 (machine-checkable) layer of the output-readability constraint system: deterministic lint over AI-produced markdown artifacts.
//
// Package doclint 是输出可读性约束体系的 L1（机器可判）层：对 AI 产出的
// markdown 产物做确定性 lint。禁令短语、必填章节、结论枚举与篇幅上限以
// 本表为单一真相源——skillgen 从同一张表渲染进 forge-quality skill 文本，
// 而不是维护第二份手写副本（设计背景见 docs/design/output-readability-gates.md）。
package doclint

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Severity distinguishes hard failures (gate/CLI exit 2) from advisories.
//
// Severity 区分硬失败（门禁/CLI exit 2）与建议（仅记录不阻断）。
type Severity string

const (
	Hard     Severity = "hard"
	Advisory Severity = "advisory"
)

// bannedPhrase 是一条 D1/D2 规则：正则 + 禁用理由。
type bannedPhrase struct {
	Pattern *regexp.Regexp
	Reason  string
}

// BannedPhrases are D1 空转措辞: filler that adds length without adding information.
//
// BannedPhrases 是 D1 空转措辞：只加长度不加信息量的填充词。正则对
// 「已剥除行内代码、且不在代码块内」的行匹配。
var BannedPhrases = []bannedPhrase{
	{regexp.MustCompile(`综上所述`), "空转总结词——总结应压缩为可执行结论，不应以套话开场"},
	{regexp.MustCompile(`基本可以`), "模糊限定语——通过/不通过须给出明确判定与证据"},
	{regexp.MustCompile(`问题不大`), "模糊限定语——风险要么量化要么列为待确认项，不写「不大」"},
	// Reason 文本引用兄弟短语必须用反引号：这些 Reason 会渲染进 forge-quality
	// skill（RenderBannedPhrasesForSkill），散文形态的引用会让 init 自产资产
	// 命中自家 D1 硬门禁——新项目开箱即被 doc gate 卡死（2026-08-29 审查轮
	// 功能实证，init 自伤）。
	{regexp.MustCompile(`大致没问题`), "模糊限定语——同 `问题不大`"},
	{regexp.MustCompile(`差不多可以`), "模糊限定语——同 `基本可以`"},
	{regexp.MustCompile(`值得注意的是`), "空转引导词——补充应内联进相关论述，不以套话另起一段"},
	{regexp.MustCompile(`总的来说`), "模糊总结词——总结应压缩为可执行结论，给出明确判定"},
	{regexp.MustCompile(`不难发现`), "推断伪装观察——给出指向结论的证据或命令，不写「不难」"},
	{regexp.MustCompile(`众所周知`), "无出处断言——常识也要给指针，给不出就删"},
}

// EvidenceFreeConclusions are D2 无证据整体性结论: whole-document verdicts that preempt evidence.
//
// EvidenceFreeConclusions 是 D2 无证据整体性结论：先于证据给出的全局判词。
// 判词要么带指针放在开头，要么放在证据链末尾——不能作为独立安抚存在。
var EvidenceFreeConclusions = []bannedPhrase{
	{regexp.MustCompile(`整体良好`), "无证据整体判词——每个「良好」须落到具体章节/指标"},
	{regexp.MustCompile(`整体上?(没有|无)(问题|异常)`), "无证据整体判词——逐项给出验证结果"},
	{regexp.MustCompile(`看起来没有问题`), "主观推断判词——「看起来」不是验证，给出实跑证据"},
	{regexp.MustCompile(`应该没有问题`), "主观推断判词——「应该」不是验证，给出实跑证据"},
	{regexp.MustCompile(`一切正常`), "无证据整体判词——正常项逐条列出"},
	{regexp.MustCompile(`基本没问题`), "无证据整体判词——同 `整体良好`，逐项给出验证结果"},
	{regexp.MustCompile(`大体正常`), "无证据整体判词——正常项逐条列出"},
	{regexp.MustCompile(`看起来正常`), "主观推断判词——同 `看起来没有问题`，给出实跑证据"},
}

// diffFingerprints 识别围栏外的原始 unified diff 粘贴（D3）。
var diffFingerprints = []*regexp.Regexp{
	regexp.MustCompile(`^diff --git `),
	regexp.MustCompile(`^\+\+\+ b/`),
	regexp.MustCompile(`^@@ -\d+(,\d+)? \+`),
}

const diffFingerprintThreshold = 3

// passClaimRe 匹配通过性断言（D4 触发词）。
var passClaimRe = regexp.MustCompile(`(测试|自测|验证|验收|回归)(全部)?通过|已验证`)

// evidenceMarkerRes 匹配可算作证据指针的标记（D4 豁免项）：行内代码/命令、
// file:line、URL、百分比。
var evidenceMarkerRes = []*regexp.Regexp{
	regexp.MustCompile("`[^`]+`"),
	regexp.MustCompile(`[\w./-]+\.\w+:\d+`),
	regexp.MustCompile(`https?://`),
	regexp.MustCompile(`\d+(\.\d+)?%`),
}

// DocType scopes D5-D7: filename substring match → required headings, a conclusion enum, and an advisory body-line cap.
//
// DocType 约束 D5-D7 的作用域：文件名子串匹配 → 必填章节、结论枚举与
// 建议性篇幅上限。
type DocType struct {
	ID               string
	FilenameContains []string // lowercase substring match against the base filename
	RequiredHeadings []string // heading text fragment (contains-match, case-insensitive)
	ConclusionEnum   []string // doc text must contain at least one
	MaxBodyLines     int      // advisory cap; 0 = none
}

// DocTypes is the v1 type table.
//
// DocTypes 是 v1 类型表。PR 描述与 commit message 不是仓库文件（存在于
// forge/宿主 UI），由撰写期模板 + rubric 约束，不进本表。
var DocTypes = []DocType{
	{
		ID:               "test-report",
		FilenameContains: []string{"test-report", "测试报告"},
		RequiredHeadings: []string{"结论"},
		MaxBodyLines:     200,
	},
	{
		ID:               "retrospective",
		FilenameContains: []string{"retrospective", "postmortem", "复盘"},
		RequiredHeadings: []string{"行动"},
		MaxBodyLines:     150,
	},
	{
		ID:               "checklist",
		FilenameContains: []string{"release-checklist"},
		ConclusionEnum:   []string{"GO", "NO-GO"},
	},
}

// matchDocType 返回首个文件名命中（仅 BASE 名）的 DocType，未命中返回 nil。
// 只匹配文件名不匹配全路径：按路径匹配会把
// skills/session-retrospective/SKILL.md 误判成「复盘报告」——类型命名的是
// 交付物文件，不是 skill 恰好所在的目录。
func matchDocType(filename string) *DocType {
	lower := strings.ToLower(filepath.Base(filename))
	for i := range DocTypes {
		for _, hint := range DocTypes[i].FilenameContains {
			if strings.Contains(lower, strings.ToLower(hint)) {
				return &DocTypes[i]
			}
		}
	}
	return nil
}

// RenderBannedPhrasesForSkill renders the D1/D2 tables into skill/protocol text (skillgen calls this — the banned list must not be hand-copied into a second location).
//
// RenderBannedPhrasesForSkill 把 D1/D2 表渲染为 skill/协议文本（skillgen 调用
// 本函数——禁令清单不允许手抄到第二处）。短语反引号包裹：生成的 skill 文本
// 须能过自身的 doclint（行内代码豁免），与 claudemd 规则 7 同构。
func RenderBannedPhrasesForSkill() string {
	var sb strings.Builder
	for _, p := range BannedPhrases {
		fmt.Fprintf(&sb, "- `%s` —%s\n", p.Pattern.String(), p.Reason)
	}
	for _, p := range EvidenceFreeConclusions {
		fmt.Fprintf(&sb, "- `%s` —%s\n", p.Pattern.String(), p.Reason)
	}
	return sb.String()
}
