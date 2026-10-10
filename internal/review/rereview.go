package review

// ReReviewGuidance is the single source of truth for the "what to do after the
// code changed post-review" instruction shown by the task-complete snapshot
// gate, `forge review status`, and the generated CLAUDE.md common-errors table.
// Re-review is tiered instead of "always dispatch a fresh reviewer": a small,
// low-risk fix with green deterministic checks is re-checked incrementally by
// the first-round reviewer (resumed, fed only prior findings + fix diff + test
// results, scoped to the fix diff and its blast radius — a scope bound, not a
// severity filter, since code-review-gate is severity-free); high-risk / large
// / final / round-3+ fixes get a
// fresh read-only reviewer. Rationale: independence comes from hiding the
// author's reasoning, not from a cold start, and repeated full re-reviews add
// noise while multiplying uncached subagent cost (arXiv 2603.16244 / 2603.12123;
// Claude Code REVIEW.md re-review convergence; CodeRabbit incremental review).
//
// ReReviewGuidance 是「审查通过后又改了码，该怎么复审」指引的唯一真相源，由
// task-complete 快照门禁、`forge review status`、生成的 CLAUDE.md 常见错误表三处
// 引用。复审分两档而非「每轮新派 fresh reviewer」：小而低风险、确定性检查全绿的
// 修复由首轮 reviewer 续用做增量复核（只喂上轮 findings + 修复 diff + 测试结果，
// 只审修复 diff 及其波及面——收敛靠范围约束而非严重性过滤，code-review-gate 明令
// 不分级）；高风险/大改/终审/第 3 轮起新派只读 reviewer 全量复审。依据：
// 独立性来自「看不到作者推理」而非冷启动，反复全量重审只加噪声、并按轮次放大
// 无缓存子 agent 成本（arXiv 2603.16244 / 2603.12123；Claude Code REVIEW.md 复审
// 收敛规则；CodeRabbit 增量复审）。
const ReReviewGuidance = "复审分两档：修复 diff 小、未触及高风险面（鉴权/数据迁移/并发/公共 API）且测试等确定性检查全绿 → 续用首轮 reviewer（宿主支持续用时，如 Claude Code SendMessage）做增量复核，只喂上轮 findings + 修复 diff + 测试结果、不给作者推理，只审修复 diff 及其波及面与未解决的原发现、不重审未改动代码；否则（高风险/大改/终审/第 3 轮起）新派只读子 agent 全量复审。复审结论记入 `forge review pass --note \"<复审结论>\"` 刷新审查基线"
