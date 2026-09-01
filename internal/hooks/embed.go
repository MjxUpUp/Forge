package hooks

// forge init 嵌入的 hook 脚本——同包分文件名册（2026-09 普查 P7：2322 行
// 单文件按职能拆为 embed_quality（评分/验证）、embed_task（任务生命周期/
// resume）、embed_guard（护栏）、embed_scan（扫描观察）四文件；脚本内容
// 逐字节不变，embeddedHooks 名册与 guard test 钉住等价性）。
// 在项目初始化时写入 .forge/hooks/。
//
// 协议：bash 脚本向 stdout 输出纯文本。
// - 以 PASS 起首的行 = 检查通过，行其余部分为可选 detail。
// - 以 FAIL 起首的行 = 检查失败，行其余部分为原因。
// - 多行时以最后一条 PASS/FAIL 行决定结果。
// - 任何到 stderr 的输出都会被捕获用于调试。
// Go 侧把结果包装成结构化 JSON 给 Claude Code。
//
// Go 侧把 tool_input 字段提取到 env var（FORGE_FILE_PATH、FORGE_CONTENT、
// FORGE_COMMAND、FORGE_OLD_STRING、FORGE_NEW_STRING、FORGE_TOOL_NAME），bash 脚本无需自己解析 JSON。
