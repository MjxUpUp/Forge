package review

// ReReviewGuidance is the single source of truth for "what to do when source
// changed after review", shown by the task-complete snapshot gate, `forge
// review pass` refusals and the generated CLAUDE.md error table.
//
// 复审只有一档：由同一个 code-review 评审者承接（首次 `--reviewer <id>` 绑定，
// 之后复用同一 id）——换新 reviewer 每轮都可能扫出新问题，评审无法收敛。确实
// 需要换视角时用 `--rotate-reviewer --reason` 显式换人并留痕；`--note` 不能再
// 刷新审查基线。
const ReReviewGuidance = "复审由同一个 code-review 评审者承接（首次用 `--reviewer <id>` 绑定，之后复用同一 id）；需要换人时用 `--rotate-reviewer --reason` 显式留痕；`--note` 不能刷新审查基线"

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
const DocReReviewGuidance = "文档复审分两档（L1 lint 未全过先修 L1，不进评审）：修订 diff 小（本轮修订只涉及单文档，新增与删除字符数之和不超过改后全文字符数的三成）且未重写结论或判定段 → 续用首轮评审者（宿主支持续用时，如 Claude Code SendMessage；不支持或句柄丢失则新派只读子代理，并把上轮 findings 全文含编号粘进 prompt）做增量复核，派审前留存被评审版本（放仓库外临时目录；复审时先对照旧快照生成修订 diff，再用当前版本覆盖快照；快照缺失按升档）用于生成修订 diff，只喂上轮 findings（含编号）+ 修订 diff、不给作者推理，对抗立场不变，只核查未决项是否真正解决、修订段落及其波及面、不重读未改动段落，评审者认为修订实质改了结论则声明需升档，四维分以上轮为底、逐维标注因哪处修订升降、未触及的维度不得升档，评审与产出同家族（家族未知按同家族）且总分落在 75-79 即将放行时另派第二评委（新派、全量，不改变续用对象）；否则（重写结论或判定段/修订 diff 大或多文档/即将进行第 3 轮评审）新派只读子代理全量评审并换视角（只喂文档本身，不喂上轮 findings），不 fork 作者会话；未决 Critical 须评审者判定已解决后才可标记，作者不得自行标记"
