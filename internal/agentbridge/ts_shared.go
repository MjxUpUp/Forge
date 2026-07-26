package agentbridge

import _ "embed"

// tsSharedForgeSpawn is the TypeScript `runForge` function shared by the opencode and pi translators.
// Both generated TS files spawn `forge hook <name>` with Claude-Code-shape stdin and must read
// forge's block verdict from the JSON decision field in the same way — centralizing it here means
// a change to the spawn protocol (or block-detection contract) lands in one place.
//
// tsSharedForgeSpawn 是 opencode 与 pi translator 共享的 TypeScript `runForge` 函数。
// 两个生成的 TS 文件都用 Claude-Code-shape stdin spawn `forge hook <name>`，并须以同样
// 方式从 JSON decision 字段读 forge 的 block verdict——在此集中维护意味着 spawn 协议
// （或 block 检测契约）的变更只改一处。
//
// Embedded from forge_spawn.ts (a real .ts file) rather than a Go raw string, so the shared
// snippet is itself valid TypeScript and is type-checked by the generator test
// (TestGeneratedTSCompiles) — nesting backticks inside a raw string is fragile and unverified.
//
// 从 forge_spawn.ts（真实 .ts 文件）embed，而非用 Go raw string，这样共享片段本身
// TypeScript 合法、并被 generator 测试（TestGeneratedTSCompiles）类型检查——raw string
// 里嵌反引号既脆弱又未经验证。
//
// Contract (see internal/cli/hook.go runHook): forge always emits one JSON line on stdout,
// shaped like {decision:`approve`|`block`, hookSpecificOutput?:{additionalContext}}.
// block is read from decision — not the exit code, because cobra uniformly reports forge's
// internal errors as exit 1, indistinguishable from deny. Parse failures and spawn errors fail
// open (allow) so a forge outage does not lock the agent out of its own tools.
//
// 契约（见 internal/cli/hook.go runHook）：forge 总是在 stdout 输出一行 JSON，
// 形如 {decision:`approve`|`block`, hookSpecificOutput?:{additionalContext}}。
// block 从 decision 读——不是 exit code，因为 cobra 把 forge 的内部错误统一报告为
// exit 1，与 deny 不可区分。解析失败与 spawn 错误 fail open（放行），以免 forge 故障
// 把 agent 锁在自己的工具之外。
//
//go:embed forge_spawn.ts
var tsSharedForgeSpawn string
