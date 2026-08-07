package checklog

import "strings"

// DetailForSkillTrigger builds the Detail string for a CheckSkillTrigger entry. It is the SINGLE source of
// truth for the skill-trigger Detail format — both the writer (cli/recordSkillTriggerHits) and the reader
// (skillseval.SkillCountsFromChecklog via SkillFromTriggerDetail) go through this contract, so a format
// change cannot silently desync the two. Without this, a hand-mirrored format string on each side could
// drift (writer adds a field, reader keeps the old shape) and passive skill-trigger signals would silently
// vanish from usage analytics — the same cross-package contract-seam class as the skill-trigger P0
// embed-fallback outage. Co-located writer+reader is the structural fix; the pair stays in sync by
// construction (TestDetailForSkillTrigger_RoundTrip pins it).
//
// DetailForSkillTrigger 构造 CheckSkillTrigger 条目的 Detail 字符串。它是 skill-trigger Detail 格式的
// 唯一真相源——写方（cli/recordSkillTriggerHits）与读方（skillseval.SkillCountsFromChecklog 经
// SkillFromTriggerDetail）都走本契约，故格式变更不可能让两侧静默失配。否则两侧手工镜像的格式串可能
// 漂移（写方加字段、读方仍旧形状），被动 skill-trigger 信号会从 usage 分析里静默消失——与 skill-trigger
// P0 embed-fallback 停服同类的跨包契约断层。写方+读方同位置是结构性修复；这对函数构造性地保持同步
// （TestDetailForSkillTrigger_RoundTrip 钉死）。
func DetailForSkillTrigger(skill, event, reason string) string {
	return "skill-trigger: " + skill + " hit (event=" + event + " " + reason + ")"
}

// SkillFromTriggerDetail inverts DetailForSkillTrigger: extracts the skill name from a CheckSkillTrigger
// entry's Detail. Returns "" for any Detail that does not match the contract (wrong prefix / no marker) —
// callers skip unparseable entries rather than crashing or misattributing. Co-located with
// DetailForSkillTrigger so the inverse stays correct by construction.
//
// SkillFromTriggerDetail 反转 DetailForSkillTrigger：从 CheckSkillTrigger 条目的 Detail 提取 skill 名。
// 不匹配契约的 Detail（前缀错 / 无 marker）返回 ""——调用方跳过不可解析条目，而非崩溃或误归。与
// DetailForSkillTrigger 同位置，使反转构造性地保持正确。
func SkillFromTriggerDetail(detail string) string {
	const prefix = "skill-trigger: "
	const marker = " hit ("
	if !strings.HasPrefix(detail, prefix) {
		return ""
	}
	rest := detail[len(prefix):]
	if i := strings.Index(rest, marker); i >= 0 {
		return rest[:i]
	}
	return ""
}
