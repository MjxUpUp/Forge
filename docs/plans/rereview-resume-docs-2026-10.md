---
status: review
owner: MjxUpUp
ask: 批准给文档评审等四处复审路径补「续用原评审者」分档的口径
tier: mini
---

# 修复方案 | 复审路径补「续用原评审者」分档（2026-10）

## TL;DR
代码审查的复审早已分档（小修复续用首轮 reviewer，大改新派，单一真相源 `review.ReReviewGuidance`）。同类的其余复审路径没跟上，打回后默认新派冷启动子代理。本批补四处，建议批准：

- 文档评审（doc-review）：SKILL 步骤 5 明文要求打回后「重新派独立子代理评审」（c7bad7f1 的 `skills/doc-review/SKILL.md` 第 69 行），文档门禁的打回文案也只说「重新评审」。新增 `review.DocReReviewGuidance` 作门禁文案的口径源，SKILL 与门禁文案按它分增量复核、全量评审两档。
- 非 task 模式 Stop hook（`gateGuidance`）：审过之后又改了码，仍输出与首次审查相同的「派只读子 agent」。文案内嵌 `ReReviewGuidance`，带「本会话已派过 reviewer 且能续用」的条件前缀。
- code-review-gate 大变更档并行派两个 reviewer（`cheat-detector` + `eng-reviewer`），复审规则只说「续用首轮 reviewer」（单数）。补按轨道续用、不新派补位，修复触及测试或断言时作弊轨也续用。
- adversarial-verification 协议 B：「修完必须整体重验」没有分档。改为修复轮增量重验，宣布 PASS 前整体重验一遍并重新实跑；协议 A 复检轮续用原红队。

收益没有基线数字（待量化）：仓库没有按任务统计子代理启动次数的遥测，动机来自用户反馈「每轮新开子代理、token 浪费明显」。可确定的上限：文档门禁最多 3 轮，第 3 轮按本方案仍新派全量评审，所以文档侧每个任务最多续用 1 次（第 2 轮）。

不改：research-workflow 的核验子代理（另议）；门禁判定逻辑与独立性归因（`independence.go` 按盖章会话判，不看子代理身份，续用不改变会话，不触发自审标记）。

## 问题与目标
- 实证：用户在使用中发现，子代理评审出问题、修复后又新开一个子代理重审。排查确认 doc-review 是打回后明文要求新派的那个 skill，其余三处是同构缺口。
- 目标：低风险的小修订续用首轮评审者做增量复核；高风险、大改、即将进行第 3 轮评审时仍新派全量复审，不削弱独立性（独立性来自评审者看不到作者推理，不来自冷启动）。

## Non-goals
- 不改 research-workflow 的核验子代理。
- 不改文档门禁的判定（得分阈值 75、轮次上限、自审标记），只改打回时给 agent 的指引文案。
- 不改 `ReReviewGuidance` 既有的两档判据，只补多 reviewer 并行时的续用口径。

## 备选
- 什么都不做（保持每轮新派）：弃用。3 轮上限下，打回后的每一轮都是冷启动，代码侧已验证分档可行。
- 只改 SKILL，不改门禁文案：弃用。门禁文案是 agent 被打回时实际读到的位置，只改 SKILL 会被「重新评审」措辞盖过。
- 文档评审一律续用、不设升档：弃用。重写结论、大改、同类发现不收敛时需要换视角，续用同一评审者会被锚定。

## 需求与验收
- REQ-1 doc-review SKILL 的打回步骤分增量复核与全量评审两档，旧的「重新派独立子代理评审」字面量消失
  accept[REQ-1]: go test ./internal/review/ -run ^TestSkillsCarryResumeTier$/^doc-review$ -count=1 :: --- PASS: TestSkillsCarryResumeTier/doc-review (
- REQ-2 `DocReReviewGuidance` 承载两档语义、增量输入契约，且不含会破坏 Sprintf 格式串与原因串拼接的 `%` 和 `; `
  accept[REQ-2]: go test ./internal/review/ -run ^TestDocReReviewGuidanceContract$ -count=1 :: --- PASS: TestDocReReviewGuidanceContract (
- REQ-3 文档门禁在评审未过、评审过期（HEAD 与指纹两条）、得分低于阈值时引用 `DocReReviewGuidance`，升级人工与自审两条不引用
  accept[REQ-3]: go test ./internal/taskpipeline/ -run ^TestDocGate_RetryReasonsCarryReReviewGuidance$ -count=1 :: --- PASS: TestDocGate_RetryReasonsCarryReReviewGuidance (
- REQ-4 非 task 模式 `gateGuidance` 内嵌 `ReReviewGuidance`，带条件前缀与否则分支，位于派发步与标记步之间
  accept[REQ-4]: go test ./internal/cli/ -run ^TestGateGuidanceCarriesReReviewTier$ -count=1 :: --- PASS: TestGateGuidanceCarriesReReviewTier (
- REQ-5 `ReReviewGuidance` 与 code-review-gate SKILL 写明首轮并行多个 reviewer 时按轨道续用、不新派补位、修复触及测试时作弊轨也续用
  accept[REQ-5]: go test ./internal/review/ -run ^TestReReviewGuidanceContract$ -count=1 :: --- PASS: TestReReviewGuidanceContract (
  accept[REQ-5]: go test ./internal/review/ -run ^TestSkillsCarryResumeTier$/^code-review-gate$ -count=1 :: --- PASS: TestSkillsCarryResumeTier/code-review-gate (
- REQ-6 adversarial-verification 协议 B 分增量重验与整体重验两档（整体重验重新实跑），协议 A 复检轮续用原红队，自检清单与 Gotchas 同步
  accept[REQ-6]: go test ./internal/review/ -run ^TestSkillsCarryResumeTier$/^adversarial-verification$ -count=1 :: --- PASS: TestSkillsCarryResumeTier/adversarial-verification (
- REQ-7 生成的 forge-quality 文档回检段提到复审续用
  accept[REQ-7]: go test ./internal/skillgen/ -run ^TestQualityDocGateMentionsResume$ -count=1 :: --- PASS: TestQualityDocGateMentionsResume (
- INV-1 既有 `ReReviewGuidance` 契约（两档、`--note` 刷新基线、无严重性措辞）不退化
  accept[INV-1]: go test ./internal/review/ -count=1 :: ok

## 设计
### 触点
（每行一个路径：PlanScope 按行取第一个路径解析。生成物不入触点：`plugins/forge/skills/*` 镜像由 `forge plugin pack` 产出、`compat.snapshot.json` 由 `forge compat snapshot` 产出，已用 `forge task scope add` 收编。）
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
- `internal/skillintegrate/notes/doc-review.md`
- `docs/plans/rereview-resume-docs-2026-10.md`

### 做法
- `rereview.go` 新增 `DocReReviewGuidance`：
  - 增量档判据：修订 diff 小（单文档，新增与删除行之和不超过全文行数的三成）、未重写结论或判定段、L1 lint 全过。续用首轮评审者，只喂上轮 findings 与修订 diff；得分以上轮全文评审为基线，只对修订段落改分；落在 70-79 另派第二评委。
  - 升档触发：重写结论或判定段、修订 diff 大或多文档、即将进行第 3 轮评审、同类发现连续两轮不收敛；升档时新派并换视角。
  - `ReReviewGuidance` 增补一句多 reviewer 并行时按轨道续用。
- `docgate.go` 在「文档回检基于旧代码、基于旧内容、未通过、得分低于阈值」四条原因后追加 `review.DocReReviewGuidance`；轮次打满升级人工与同会话自审两条不加（前者是人工裁决，后者需要独立评审者）。
- `review.go` 的 `gateGuidance` 在第 2 步后追加一行复审分档，内嵌 `review.ReReviewGuidance`，前缀写明「本会话内已对本分支派过 reviewer 且手里有句柄」，并写明首审与新会话接手按第 2 步新派。不新增 stamp 状态：stamp 按内容寻址，没有可靠的「本分支此前已审」读法，条件句交给 agent 判断。
- 三份 SKILL 与 `qualitygen.go`、集成笔记按上述口径改文字；`TestSkillsCarryResumeTier` 钉关键短语并禁旧字面量回归，含 adversarial-verification 自检清单与 Gotchas 里的旧表述。

### 关键取舍
- 文档复审单独成常量，不复用 `ReReviewGuidance`：后者的措辞绑定代码（鉴权、数据迁移、并发、公共 API、`forge review pass`），套到文档上判据不成立。
- 门禁文案以常量为口径源，SKILL 步骤 5 是同口径镜像：SKILL 不能引用 Go 常量，两处由 `TestSkillsCarryResumeTier` 钉关键短语，没有逐字一致性检查。
- 协议 B 保留整体重验，且整体重验要重新实跑、不得引用前轮结果：续用的验收者记得上轮「已通过」，不加这条会退化成确认式验收。代价：修复一轮的任务多一次增量重验，成本略增；修复两轮及以上、且增量重验明显小于整体重验时才省。
- 多 reviewer 续用以「发现所在轨道」为主、「修复触及测试或断言」为补：只看发现归属，会让修复 eng-reviewer 的发现时顺手弱化测试漏过作弊轨。
- Stop hook 不读 stamp 判断复审：改状态机的代价大于一句条件指引。新会话接手同一分支时 agent 没有首轮 reviewer 的句柄，按条件句回退新派，节省限于同会话内。

### 已知局限
- 续用依赖宿主（Claude Code 的 `SendMessage`）；宿主不支持续用时退回新派并粘贴上轮 findings 全文，节省有限。
- 首次 `SendMessage` 续用可能整段缓存未命中（anthropics/claude-code#44724，之后的续用才命中；该 issue 2026-05-24 被 stale bot 以 not planned 关闭，未见上游修复，记载见 `skills/agent-delegation/decisions.md`），省的主要是重新探索的开销。
- 文档侧每个任务最多续用 1 次（第 2 轮），第 3 轮按规则新派；HEAD 与指纹过期两条原因不带轮次，agent 需自行判断当前是第几轮。
- `independence.go` 按盖章会话判独立性，同宿主子代理共用会话 id 时可能被标自审，与续用无关，沿用既有处理。
- 协议 B 增量重验的「共享接缝」靠验收者判断，判错会漏验；宣布 PASS 前的整体重验兜底。

## 验证与回滚
- `go vet ./...`；`go test ./... -count=1 -timeout 30m`。
- skills 改动后：重建二进制、`forge plugin pack`、`forge compat snapshot` 重新固定。
- 回滚即 revert 本分支的提交；只改文案与常量，不涉及数据面与命令面。
