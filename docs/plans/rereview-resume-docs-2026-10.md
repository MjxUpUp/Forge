---
status: review
owner: MjxUpUp
ask: 批准给四处复审路径补「续用原评审者」口径；范围是口径统一，不承诺收益
tier: mini
---

# 修复方案 | 复审路径补「续用原评审者」分档（2026-10）

## TL;DR
请批准：在 doc-review、非 task 模式 Stop hook、code-review-gate 的双 reviewer、adversarial-verification 四处补「小修订续用首轮评审者」的口径。范围是口径统一，不承诺收益：文档侧收益未验证且可能很小，仓库没有子代理启动次数的遥测，没有基线数字，本批也不度量（见「验收与度量」）。

代码审查的复审早已分档（口径源 `review.ReReviewGuidance`，依据 arXiv 2603.16244、2603.12123，出处与测试见 `internal/review/rereview.go` 注释与 `TestReReviewGuidanceContract`），其余四处没跟上，打回后默认新派冷启动子代理：

- 文档评审：SKILL 步骤 5 明文要求打回后「重新派独立子代理评审」（c7bad7f1 的 `skills/doc-review/SKILL.md` 第 69 行），文档门禁的重评原因也只说「重新评审」。新增 `review.DocReReviewGuidance` 作门禁文案的口径源，SKILL 步骤 5 同口径分增量复核、全量评审两档。
- 非 task 模式 Stop hook（`gateGuidance`）：审过之后又改了码，仍输出与首次审查相同的「派只读子 agent」。分 (a) 首审或新会话接手、(b) 本会话内复审两种情形，(b) 内嵌 `ReReviewGuidance`。
- code-review-gate 大变更档并行派 `cheat-detector` + `eng-reviewer`，复审规则只说「续用首轮 reviewer」（单数）。补按轨道续用、不新派补位。
- adversarial-verification：协议 A 复检轮续用原红队；协议 B 保持「修完必须整体重验」，补「重新实跑、不得引用前轮结果」与「复验续用原验收者」。

收益预期：文档门禁最多 3 轮，第 3 轮按本方案仍新派全量，所以文档侧每个任务最多续用 1 次（第 2 轮）；被评审文档通常较短，重新探索的开销本就有限；首次 `SendMessage` 续用还可能整段缓存未命中（见「已知局限」）。

不改：research-workflow 的核验子代理（另议）；门禁判定逻辑（得分阈值 75、轮次上限、自审标记）。

## 问题与目标
- 动机：用户口述反馈（无记录）：子代理评审出问题、修复后又新开一个子代理重审，token 浪费明显。排查确认 doc-review 是打回后明文要求新派的 skill，其余三处是同构缺口。
- 目标：低风险的小修订续用首轮评审者做增量复核；重写结论、大改、即将进行第 3 轮评审时仍新派全量复审。独立性来自评审者看不到作者推理，不来自冷启动（依据同上）；把这条从代码评审外推到文档评审，依据是同一机制，文档侧收益本身未验证。

## Non-goals
- 不改 research-workflow 的核验子代理。
- 不改文档门禁的判定，只改打回与评审过期时给 agent 的指引文案。
- 不改 `ReReviewGuidance` 既有的两档判据，只补多 reviewer 并行时的续用口径。
- 不给协议 B 加增量重验档：该档在「修一轮就过」的常见路径上要先增量重验、PASS 前再整体重验，比原流程多一次，且削弱「每个修复轮整体重验」的防假绿底线。

## 备选
- 什么都不做（保持每轮新派）：弃用。3 轮上限下，打回后的每一轮都是冷启动；代码侧已有同口径分档与测试。
- 最小口径（只在 doc-review 步骤 5 与门禁文案各加一句「小修订可续用首轮评审者」）：弃用。没有判据，agent 无从判断何时该新派，也没有「不给作者推理、对抗立场不变、记分口径」这些防锚定约束；代价是本批维护成本更高——17 个文件，Go 非测试源码净增数十行，其余是测试、镜像与生成物，SKILL 步骤 5 变长。批准即接受该维护成本换口径统一。
- 只改 SKILL，不改门禁文案：弃用。门禁文案是 agent 被打回时实际读到的位置，只改 SKILL 会被「重新评审」措辞盖过。
- 文档评审一律续用、不设升档：弃用。重写结论、大改时需要新视角，续用同一评审者会被锚定。
- 协议 B 增量重验加整体重验两档：弃用，理由见 Non-goals。

## 需求与验收
- REQ-1 doc-review SKILL 的打回步骤分增量复核与全量评审两档，旧的「重新派独立子代理评审」字面量消失
  accept[REQ-1]: go test ./internal/review/ -run ^TestSkillsCarryResumeTier$/^doc-review$ -count=1 :: --- PASS: TestSkillsCarryResumeTier/doc-review (
- REQ-2 `DocReReviewGuidance` 承载两档语义、增量输入契约、对抗立场不变与记分口径，且不含会破坏 Sprintf 格式串与原因串拼接的 `%` 和 `; `
  accept[REQ-2]: go test ./internal/review/ -run ^TestDocReReviewGuidanceContract$ -count=1 :: --- PASS: TestDocReReviewGuidanceContract (
- REQ-3 文档门禁在评审未过、评审过期（HEAD 与指纹两条）、得分低于阈值时引用 `DocReReviewGuidance`，升级人工与自审两条不引用；HEAD 过期时只有指纹相同才说「内容与评审时一致」，任何情形都写明不得凭自述重录旧分数；两条过期原因带「已评 N 轮」
  accept[REQ-3]: go test ./internal/taskpipeline/ -run ^TestDocGate_RetryReasonsCarryReReviewGuidance$ -count=1 :: --- PASS: TestDocGate_RetryReasonsCarryReReviewGuidance (
- REQ-4 非 task 模式 `gateGuidance` 分 (a)(b) 两种情形，(b) 内嵌 `ReReviewGuidance`，顺序为派发步、(a)、(b)、标记步，且不断言非 task 路径不存在的「裸标记会被拒」
  accept[REQ-4]: go test ./internal/cli/ -run ^TestGateGuidanceCarriesReReviewTier$ -count=1 :: --- PASS: TestGateGuidanceCarriesReReviewTier (
- REQ-5 `ReReviewGuidance` 与 code-review-gate SKILL 写明首轮并行多个 reviewer 时按轨道续用、不新派补位、`eng-reviewer` 始终续用、命中作弊模式速查表或触及测试时 `cheat-detector` 也续用、首轮零发现时两个都续用
  accept[REQ-5]: go test ./internal/review/ -run ^TestReReviewGuidanceContract$ -count=1 :: --- PASS: TestReReviewGuidanceContract (
  accept[REQ-5]: go test ./internal/review/ -run ^TestSkillsCarryResumeTier$/^code-review-gate$ -count=1 :: --- PASS: TestSkillsCarryResumeTier/code-review-gate (
- REQ-6 adversarial-verification 协议 A 复检轮续用原红队，协议 B 保持修完整体重验并补重新实跑、喂验收点清单与续用原验收者
  accept[REQ-6]: go test ./internal/review/ -run ^TestSkillsCarryResumeTier$/^adversarial-verification$ -count=1 :: --- PASS: TestSkillsCarryResumeTier/adversarial-verification (
- REQ-7 生成的 forge-quality 文档回检段提到复审续用
  accept[REQ-7]: go test ./internal/skillgen/ -run ^TestQualityDocGateMentionsResume$ -count=1 :: --- PASS: TestQualityDocGateMentionsResume (
- INV-1 既有 `ReReviewGuidance` 契约（两档、`--note` 刷新基线、无严重性措辞）不退化
  accept[INV-1]: go test ./internal/review/ -count=1 :: ok

### 验收与度量
REQ-1、REQ-5、REQ-6 的 accept 只钉关键短语存在与旧字面量消失，不证明 SKILL 上下文自洽；自洽由评审承担。本批不度量收益，不承诺收益。若合并后要度量，可抽查若干文档任务的第 2 轮是否用了 `SendMessage`，作为后续观察，不作验收项。

## 设计
### 触点
（每行一个路径：PlanScope 按行取第一个路径解析。生成物不入触点：`plugins/forge/skills/*` 镜像由 `forge plugin pack` 产出，`compat.snapshot.json` 与 `skills-inventory.lock` 由 `forge compat snapshot`、`forge skills inventory --lock` 产出，均已用 `forge task scope add` 收编。）
- `internal/review/rereview.go`
- `internal/review/rereview_test.go`
- `internal/taskpipeline/docgate.go`
- `internal/taskpipeline/docgate_test.go`
- `internal/cli/review.go`
- `internal/cli/review_gateguidance_test.go`
- `internal/skillgen/qualitygen.go`
- `internal/skillgen/qualitygen_resume_test.go`
- `skills/doc-review/SKILL.md`
- `skills/code-review-gate/SKILL.md`
- `skills/adversarial-verification/SKILL.md`
- `skills/doc-review/decisions.md`
- `skills/code-review-gate/decisions.md`
- `skills/adversarial-verification/decisions.md`
- `internal/skillintegrate/notes/doc-review.md`
- `docs/plans/rereview-resume-docs-2026-10.md`

### 做法
- `rereview.go` 新增 `DocReReviewGuidance`：
  - 前置：L1 lint 未全过先修 L1，不进评审。
  - 增量档判据：修订 diff 小（单文档，新增与删除字符数之和不超过改后全文字符数的三成；文档常一段一行，按行数会低估大改）、未重写结论或判定段。
  - 增量档做法：续用首轮评审者，不支持续用或句柄丢失则新派并粘贴上轮 findings 全文；派审前把被评审版本留存在仓库外临时目录，复审用 `diff -u` 生成修订 diff（`DocReview` 只存 HEAD 与内容指纹，评审通过前文档往往未提交；快照若落成仓库内 `.md`，会被 doc gate 当文档）；只喂上轮 findings 与修订 diff；对抗立场不变；四维分以上轮为底、逐维标注升降、未触及的维度不得升档；评审与产出同家族（家族未知按同家族）且增量评分后总分落在 75-79 即将放行时，另派第二评委（新派、全量）。rubric 纪律 6 的 70-79 区间里，70-74 本就未过，所以只有会放行的 75-79 触发。
  - 升档触发：重写结论或判定段、修订 diff 大或多文档、即将进行第 3 轮评审；升档时新派、换视角，只喂文档本身，不喂上轮 findings（防锚定）。
- `ReReviewGuidance` 增补多 reviewer 并行时的续用口径：`eng-reviewer` 始终续用（回归与调用方核查由它承担，两档都必须核），`cheat-detector` 在修复命中作弊模式速查表或触及测试断言 mock 时也续用，首轮零发现时两个都续用。该常量同时出现在 task-complete 拒绝文案、`forge review pass` 拒绝报错、`forge review status` 与生成的 CLAUDE.md 常见错误表，变长后这些位置随之变长；单 reviewer 用户读到时可忽略该句。
- `docgate.go` 在「文档回检基于旧代码、基于旧内容、未通过、得分低于阈值」四条原因后追加 `review.DocReReviewGuidance`。HEAD 过期时 HEAD 前移不等于文档变了（评审、提交、complete 是主流程）：`DocsFingerprint` 非空且与当前指纹相同才写「内容与评审时一致，续用首轮评审者确认即可」，缺失或不同不下该断言；任何情形都写明仍须评审后记录、不得凭自述重录旧分数（Passed 与分数由调用方自录，不开免评审通道）。两条过期原因带「已评 N 轮」。轮次打满升级人工与同会话自审两条不加：前者是人工裁决；后者的补救是由独立会话重新记录（自审标记按盖章会话判，见已知局限），与续用与否无关。
- `review.go` 的 `gateGuidance` 在第 2 步后分 (a) 首审、新会话接手或没有首轮 reviewer 句柄：按本步新派；(b) 本会话内已派过 reviewer 且句柄可续用：内嵌 `review.ReReviewGuidance`，并注明非 task 模式下 `--note` 只作审计留痕。不新增 stamp 状态：stamp 按内容寻址，没有可靠的「本分支此前已审」读法，由 agent 按情形判断。「裸标记会被拒」只存在于 task 模式的 `runReviewPassAt`，不出现在该文案里。
- 三份 SKILL 与 `qualitygen.go`、集成笔记按上述口径改文字，三份 `decisions.md` 各记一条决策，`skills-inventory.lock` 重新固定；`TestSkillsCarryResumeTier` 钉关键短语并禁旧字面量回归。

### 关键取舍
- 文档复审单独成常量，不复用 `ReReviewGuidance`：后者的措辞绑定代码（鉴权、数据迁移、并发、公共 API、`forge review pass`），套到文档上判据不成立。
- 门禁文案以常量为口径源，SKILL 步骤 5 是同口径镜像：SKILL 不能引用 Go 常量，两处由 `TestSkillsCarryResumeTier` 钉关键短语，没有逐字一致性检查；门禁常量比 SKILL 少「满档只能由全量评审给出」「`--score` 取四维合计」「说明查了哪几处」几项细则，以 SKILL 为准。
- 协议 B 不加增量档：保留「修完必须整体重验」，只补重新实跑与续用原验收者。省的是重新探索，不是重跑。
- 多 reviewer 续用让 `eng-reviewer` 始终续用、`cheat-detector` 按触发条件续用：只看发现归属，会让修复 eng-reviewer 的发现时顺手加吞错、类型压制或弱化测试而漏过作弊轨，也会漏掉回归与调用方核查。
- Stop hook 不读 stamp 判断复审：改状态机的代价大于分情形的文案。新会话接手同一分支时 agent 没有首轮 reviewer 句柄，按 (a) 新派，节省限于同会话内。

### 已知局限
- 续用依赖宿主（Claude Code 的 `SendMessage`）；宿主不支持续用时退回新派并粘贴上轮 findings 全文，节省有限。
- 首次 `SendMessage` 续用可能整段缓存未命中（anthropics/claude-code#44724，之后的续用才命中；该 issue 2026-05-24 被 stale bot 以 not planned 关闭，未见上游修复，记载见 `skills/agent-delegation/decisions.md`），省的主要是重新探索的开销。
- 每次 `forge task doc-review` 记录都让 Round 加 1，仅因 HEAD 前移而重录也会消耗一轮，使下一次评审更早进入「即将进行第 3 轮」；本批只在原因里带出「已评 N 轮」，不改计数逻辑。
- 独立性归因按盖章会话判（`internal/taskpipeline/independence.go` 头注释）：同宿主子代理共用会话 id 时可能被标自审，与续用无关，沿用既有处理；评审者给出分数后由用户在自己的终端记录，是既有流程。
- 「三成」按字符计，用 `git diff --no-index --word-diff=porcelain --word-diff-regex=.` 统计，三成附近按升档；仍是粗口径：一句话改写结论段也可能不足三成字符，靠「未重写结论或判定段」这一条判据兜底。
- rubric 纪律 6 与集成笔记第 4 步仍写「borderline 70-79」，本方案的 75-79 是其中会放行的子集，不是新口径，未改这两处。

## 验证与回滚
- `go vet ./...`；`go test ./... -count=1 -timeout 30m`。
- skills 改动后：重建二进制、`forge plugin pack`、`forge compat snapshot`、`forge skills inventory --lock`、`forge skills inventory --verify`。
- 回滚即 revert 本分支的提交；只改文案与常量，不涉及数据面与命令面。
