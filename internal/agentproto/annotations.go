package agentproto

// Annotations 是「命令 → 一句话 agent 语义」的唯一来源。
//
// 只给协议块散文真正教到的命令写注解：这里是投递面的诚实清单，不是命令大全——
// 没注解的命令不进附录，也不会被假装教过。新增/删除命令时，这份 map 与命令树
// 的一致性由 TestAnnotationsMatchTree 盯着。
var Annotations = map[string]string{
	"change agents":  "重同步 AGENTS.md 托管块（命令面与块头指纹由命令树生成）",
	"change clarify": "记录边界与非目标（必须 --by-human，agent 不得自批）",
	"change gate":    "只跑 tasks.md 里某条任务引用的判据（节点证据不封印整个 change）",
	"change init":    "声明 spec/forge.yaml 契约并同步托管块",
	"change list":    "列 change 与证据新鲜度（证据已失效会标出来）",
	"change new":     "新建 change（goal/change/acceptance 三份产物）",
	"change phase":   "看还缺哪些产物、下一步该补哪个文件",
	"change status":  "回环入口：只执行它给的 next steps，不自己挑顺序",
	"change verify":  "实跑 acceptance 并封印证据（唯一能产出「已验证」的命令）",
	"docs closure":   "能力面闭合表：命令树与活文档的漂移（--strict 有漂移退出非 0）",
	"docs lint":      "文档闸：改 .md 后先过一遍结构/禁令检查",
	"impact":         "看改动影响面（哪些文档、哪些源码同目录没有测试）",
	"next":           "只在继续/接手已有任务时用；新需求直接开工，别把它当默认第一步",
	"review pass":    "记录评审结论（--reviewer 绑定评审者，复审复用同一 id）",
	"task":           "任务状态总览",
	"task abort":     "只清 forge state，不动代码",
	"task complete":  "完结任务（硬前置：review pass 已记录、验收已实跑未过期）",
	"task gate":      "跑门禁：task-implement / task-complete",
	"task report":    "出交付单（完成后带 --ref）",
	"task start":     "起任务并附基线（验收预跑）；--accept 的 run 按空白切 argv，不经 shell",
	"task wild":      "申报刻意的一次性小改，别用「顺手」绕过门禁",
}
