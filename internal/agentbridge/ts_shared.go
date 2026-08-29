package agentbridge

import _ "embed"

// tsSharedForgeSpawn 是嵌入 opencode 插件的 TypeScript `runForge` 函数
// （opencode.go；pi translator 已走自己的原生集成，当前单一消费方——保留共享文件
// 是因为任何第二个 TS host 都必须以完全相同的方式从 JSON decision 字段读 forge
// 的 block verdict）。生成的 TS 用 Claude-Code-shape stdin spawn `forge hook <name>`；
// 在此集中维护意味着 spawn 协议（或 block 检测契约）的变更只改一处。
//
// 从 forge_spawn.ts（真实 .ts 文件）embed，而非用 Go raw string，这样共享片段本身
// TypeScript 合法、并被 generator 测试（TestGeneratedTSCompiles）类型检查——raw string
// 里嵌反引号既脆弱又未经验证。
//
// 契约（见 internal/cli/hook.go runHook）：forge 总是在 stdout 输出一行 JSON，
// 形如 {decision:`approve`|`block`, hookSpecificOutput?:{additionalContext}}。
// block 从 decision 读——不是 exit code，因为 cobra 把 forge 的内部错误统一报告为
// exit 1，与 deny 不可区分。解析失败与 spawn 错误 fail open（放行），以免 forge 故障
// 把 agent 锁在自己的工具之外。
//
//go:embed forge_spawn.ts
var tsSharedForgeSpawn string
