package checklog

import "strings"

// MetaKey* 是 skill-trigger Entry.Meta 的键——结构化载荷契约的单一真相源，与
// DetailForSkillTrigger 同位置（同款契约缝纪律：写方 cli/recordSkillTriggerHits 与
// 读方 skillseval/* 不得手工镜像键字符串）。写方只写已知项；读方必须把缺键当
// 「未知」而非零值（Meta 之前的旧条目 map 为 nil）。
const (
	// MetaKeyMatchedKeyword: the specific keyword that fired (absent for condition-only triggers).
	//
	// MetaKeyMatchedKeyword：实际命中的具体关键词（condition-only 触发缺省）。
	// per-keyword 命中/遵循统计以此为主键。
	MetaKeyMatchedKeyword = "matched_keyword"
	// MetaKeyMatchSource: which source text the keyword hit — prompt|command|stdout|stderr| output.
	//
	// MetaKeyMatchSource：关键词命中的来源文本——prompt|command|stdout|stderr|output。
	// 跨源边界命中已消灭（分源判定，v2 语义变更），每条命中必可归到单一来源。
	MetaKeyMatchSource = "match_source"
	// MetaKeyWhen: the named condition that evaluated true (absent for keyword-only).
	//
	// MetaKeyWhen：判定为真的命名 condition（keyword-only 缺省）。
	MetaKeyWhen = "when"
	// MetaKeyTriggerIndex: index of the fired rule in the skill's triggers array (first matching rule).
	//
	// MetaKeyTriggerIndex：命中规则在 skill triggers 数组中的下标（首条命中）。
	// 与 MetaKeyTriggerSig 配对构成抗漂移的规则身份。
	MetaKeyTriggerIndex = "trigger_index"
	// MetaKeyTriggerSig: sha1[:8] of the declared rule JSON — rule identity independent of array order (index shifts on edit do not break longitudinal stats).
	//
	// MetaKeyTriggerSig：声明规则 JSON 的 sha1[:8]——不依赖数组顺序的规则身份
	//（编辑导致下标平移不破坏纵向统计）。
	MetaKeyTriggerSig = "trigger_sig"
	// MetaKeyPromptHash: sha1[:12] of (project-salt + prompt).
	//
	// MetaKeyPromptHash：(项目盐 + prompt) 的 sha1[:12]。盐 = 项目根路径，项目内可聚类、
	// 跨项目不可关联、低熵 prompt 无法批量字典反推（辩论 G4：session 级盐会杀死跨
	// session 去重，故只用项目盐）。
	MetaKeyPromptHash = "prompt_hash"
	// MetaKeyPromptLen: rune length of the prompt at hit time (hash-adjacent context).
	//
	// MetaKeyPromptLen：命中时 prompt 的 rune 长度（hash 的伴随上下文）。
	MetaKeyPromptLen = "prompt_len"
	// MetaKeyExcerpt: redacted ±96-rune window around the match (opt-in only, FORGE_TRIGGER_EXCERPT=1; never written by default — triage/mining aid, never enters VCS via golden).
	//
	// MetaKeyExcerpt：命中点 ±96 rune 的脱敏窗口（仅 opt-in，FORGE_TRIGGER_EXCERPT=1；
	// 默认永不写——triage/挖矿辅助，绝不经 golden 进 VCS）。
	MetaKeyExcerpt = "excerpt"
	// MetaKeySuppressedSinceLast: count of cooldown-suppressed near-hits since this skill's previous actual fire in the same session (backfilled at fire time; the last suppression burst before session end is unrecorded — honest gap, see SuppressedHit docs).
	//
	// MetaKeySuppressedSinceLast：本 session 内该 skill 上次真实触发以来的 cooldown 抑制
	// 近似命中计数（触发时回填；session 末段的抑制突发不被记录——诚实缺口，见
	// SuppressedHit 文档）。
	MetaKeySuppressedSinceLast = "suppressed_since_last"
	// MetaKeyCause labels suppression/infrastructure entries (e.g. "stop-max-rounds") so the warn-level advisory is machine-distinguishable from real hits (its Detail deliberately lacks the " hit (" marker so SkillFromTriggerDetail skips it in counts).
	//
	// MetaKeyCause 标注抑制/基建条目（如 "stop-max-rounds"），warn 级 advisory 与真实命中
	// 机器可分（其 Detail 刻意不含 " hit (" 标记，SkillFromTriggerDetail 在计数中跳过）。
	MetaKeyCause = "cause"
	// MetaKeySkills: comma-joined skill list (used by the stop-max-rounds advisory entry).
	//
	// MetaKeySkills：逗号连接的 skill 列表（stop-max-rounds advisory 条目使用）。
	MetaKeySkills = "skills"
)

// DetailForSkillTrigger builds the Detail string for a CheckSkillTrigger entry.
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

// SkillFromTriggerDetail inverts DetailForSkillTrigger: extracts the skill name from a CheckSkillTrigger entry's Detail.
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
