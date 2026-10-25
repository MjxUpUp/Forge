package review

// ReReviewGuidance is the single source of truth for the "what to do after the
// code changed post-review" instruction shown by the task-complete snapshot
// gate, the `forge review pass` rejection and `--acknowledge-changes` hint,
// `forge review status`, the non-task Stop-hook gateGuidance, and the generated
// CLAUDE.md common-errors table.
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
// task-complete 快照门禁、`forge review pass` 拒绝报错与 `--acknowledge-changes`
// 提示、`forge review status`、非 task 模式 Stop hook 的 gateGuidance、生成的
// CLAUDE.md 常见错误表等处引用。复审分两档而非「每轮新派 fresh reviewer」：小而低风险、确定性检查全绿的
// 修复由首轮 reviewer 续用做增量复核（只喂上轮 findings + 修复 diff + 测试结果，
// 只审修复 diff 及其波及面——收敛靠范围约束而非严重性过滤，code-review-gate 明令
// 不分级）；高风险/大改/终审/第 3 轮起新派只读 reviewer 全量复审。依据：
// 独立性来自「看不到作者推理」而非冷启动，反复全量重审只加噪声、并按轮次放大
// 无缓存子 agent 成本（arXiv 2603.16244 / 2603.12123；Claude Code REVIEW.md 复审
// 收敛规则；CodeRabbit 增量复审）。
const ReReviewGuidance = "复审分两档：修复 diff 小、未触及高风险面（鉴权/数据迁移/并发/公共 API）且测试等确定性检查全绿 → 续用首轮 reviewer（宿主支持续用时，如 Claude Code SendMessage）做增量复核，只喂上轮 findings + 修复 diff + 测试结果、不给作者推理，只审修复 diff 及其波及面与未解决的原发现、不重审未改动代码（首轮并行派了多个 reviewer 时按轨道续用：有发现的那一轨必续用以核原发现是否真正解决，eng-reviewer 始终续用以核回归与调用方，cheat-detector 无发现时在修复命中作弊模式速查表任一项（含新增吞错/类型压制/永假分支、新增而无测试的分支）或触及测试断言/mock 时那一轨也续用，只审修复 diff，首轮零发现时两个都续用，不新派补位）；否则（高风险/大改/终审/第 3 轮起）新派只读子 agent 全量复审。复审结论记入 `forge review pass --note \"<复审结论>\"` 刷新审查基线"

// DocReReviewGuidance is the single source of truth for "what to do after a
// doc-review round failed or the document changed after review", shown by the
// doc gate's retry reasons and mirrored in the doc-review skill. It is
// separate from ReReviewGuidance because the code-bound wording (auth, data
// migration, concurrency, public API, `forge review pass`) does not apply to
// documents. Same principle: independence comes from hiding the author's
// reasoning, not from a cold start, so a small revision is re-checked by the
// first-round reviewer (resumed, fed only prior findings + revision diff) and
// only a rewritten conclusion / large diff / round 3+ gets a fresh read-only
// reviewer (a same-family borderline pass also gets a fresh second judge).
//
// DocReReviewGuidance 是「文档评审未过、或评审后文档又改了，该怎么复审」指引的
// 唯一真相源，由文档门禁的打回文案引用、doc-review skill 同口径镜像。与
// ReReviewGuidance 分开：后者措辞绑定代码（鉴权/数据迁移/并发/公共 API、
// `forge review pass`），套到文档上判据不成立。原则相同：独立性来自「看不到作者
// 推理」而非冷启动，所以小修订由首轮评审者续用做增量复核（只喂上轮 findings +
// 修订 diff）；重写结论/判定段、大改、第 3 轮起才新派只读评审者（同家族 borderline
// 放行前另派第二评委，也是新派）。
const DocReReviewGuidance = "文档复审分两档（L1 lint 未全过先修 L1，不进评审）：修订 diff 小（单文档，新增与删除字符数之和不超过改后全文字符数的三成）且未重写结论或判定段 → 续用首轮评审者（宿主支持续用时，如 Claude Code SendMessage；不支持或句柄丢失则新派只读子代理，并把上轮 findings 全文含编号粘进 prompt）做增量复核，派审前留存被评审版本（放仓库外临时目录；复审时先对照旧快照生成修订 diff，再用当前版本覆盖快照）用于生成修订 diff，只喂上轮 findings（含编号）+ 修订 diff、不给作者推理，对抗立场不变，只核查未决项是否真正解决、修订段落及其波及面、不重读未改动段落，四维分以上轮为底、逐维标注因哪处修订升降、未触及的维度不得升档，评审与产出同家族（家族未知按同家族）且总分落在 75-79 即将放行时另派第二评委（新派、全量）；否则（重写结论或判定段/修订 diff 大或多文档/即将进行第 3 轮评审）新派只读子代理全量评审并换视角（只喂文档本身，不喂上轮 findings），不 fork 作者会话"
