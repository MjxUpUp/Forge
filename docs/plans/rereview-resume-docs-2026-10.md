---
status: review
owner: MjxUpUp
ask: 审文档评审等四处复审路径补「续用原评审者」分档的口径
tier: mini
---

# 修复方案 | 复审路径补「续用原评审者」分档（2026-10）

## TL;DR
代码审查的复审早已分档（小修复续用首轮 reviewer，大改新派，单一真相源 `review.ReReviewGuidance`）。同类的其余复审路径没跟上，每轮打回都新派冷启动子代理，按轮次放大 token 成本。本批补四处：

- P0 文档评审（doc-review）：SKILL 明文要求打回后「重新派独立子代理评审」，文档门禁的打回文案也只说「重新评审」。新增 `review.DocReReviewGuidance` 作文档复审的单一真相源，SKILL 与门禁文案按它分档。
- P1 非 task 模式 Stop hook（`gateGuidance`）：审过之后又改了码，仍输出与首次审查相同的「派只读子 agent」，不区分复审。文案内嵌 `ReReviewGuidance`。
- P1 code-review-gate 大变更档并行派两个 reviewer（`cheat-detector` + `eng-reviewer`），复审规则只说「续用首轮 reviewer」（单数）。补按轨道续用、不新派补位。
- P1 adversarial-verification 协议 B：「修完必须整体重验」没有分档。改为修复轮增量重验，交付前整体重验一遍。

不改：research-workflow 核验子代理（P2，另议）；门禁判定逻辑与独立性归因（`independence.go` 按会话判，不看子代理身份，续用不触发自审标记）。

## 问题与目标
- 实证：用户在使用中发现，子代理评审出问题、修复后又新开一个子代理重审，token 浪费明显。排查（本会话）确认 doc-review SKILL 第 5 步写死「重新派」，是唯一没有续用档的评审类 skill。
- 目标：低风险的小修订续用首轮评审者做增量复核；高风险、大改、第 3 轮起仍新派全量复审，不削弱独立性（独立性来自评审者看不到作者推理，不来自冷启动）。

## Non-goals
- 不改 research-workflow 的核验子代理（P2）。
- 不改文档门禁的判定（得分阈值 75、轮次上限、自审标记），只改打回时给 agent 的指引文案。
- 不改 `ReReviewGuidance` 既有的两档判据，只补多 reviewer 并行时的续用口径。

## 需求与验收
- REQ-1 doc-review SKILL 的打回步骤分增量复核与全量评审两档，旧的「重新派独立子代理评审」字面量消失
  accept[REQ-1]: go test ./internal/review/ -run ^TestSkillsCarryResumeTier$/^doc-review$ -count=1 :: --- PASS: TestSkillsCarryResumeTier/doc-review (
- REQ-2 `DocReReviewGuidance` 承载两档语义、增量输入契约，且不含竖线
  accept[REQ-2]: go test ./internal/review/ -run ^TestDocReReviewGuidanceContract$ -count=1 :: --- PASS: TestDocReReviewGuidanceContract (
- REQ-3 文档门禁在评审未过与评审过期时引用 `DocReReviewGuidance`
  accept[REQ-3]: go test ./internal/taskpipeline/ -run ^TestDocGate_RetryReasonsCarryReReviewGuidance$ -count=1 :: --- PASS: TestDocGate_RetryReasonsCarryReReviewGuidance (
- REQ-4 非 task 模式 `gateGuidance` 内嵌 `ReReviewGuidance`
  accept[REQ-4]: go test ./internal/cli/ -run ^TestGateGuidanceCarriesReReviewTier$ -count=1 :: --- PASS: TestGateGuidanceCarriesReReviewTier (
- REQ-5 `ReReviewGuidance` 与 code-review-gate SKILL 写明首轮并行多个 reviewer 时按轨道续用、不新派补位
  accept[REQ-5, INV-1]: go test ./internal/review/ -run ^TestReReviewGuidanceContract$ -count=1 :: --- PASS: TestReReviewGuidanceContract (
- REQ-6 adversarial-verification 协议 B 分增量重验与整体重验两档，协议 A 复检轮续用原红队
  accept[REQ-6]: go test ./internal/review/ -run ^TestSkillsCarryResumeTier$/^adversarial-verification$ -count=1 :: --- PASS: TestSkillsCarryResumeTier/adversarial-verification (
- REQ-7 生成的 forge-quality 文档回检段提到复审续用
  accept[REQ-7]: go test ./internal/skillgen/ -run ^TestQualityDocGateMentionsResume$ -count=1 :: --- PASS: TestQualityDocGateMentionsResume (
- INV-1 既有 `ReReviewGuidance` 契约（两档、`--note` 刷新基线、无严重性措辞）不退化
  accept[INV-1]: go test ./internal/review/ -count=1 :: ok

## 设计
### 触点
（每行一个路径：PlanScope 按行取第一个路径解析。）
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
- `rereview.go` 新增 `DocReReviewGuidance`：增量档判据为修订 diff 小（单文档且改动不超过三成行）、未重写结论或判定段、L1 lint 全过，续用首轮评审者，只喂上轮 findings 与修订 diff；升档触发为重写结论或判定段、修订 diff 大或多文档、第 3 轮起、同类发现连续两轮不收敛。`ReReviewGuidance` 增补一句多 reviewer 并行时按轨道续用。
- `docgate.go` 在「文档回检基于旧代码、基于旧内容、未通过」三条原因后追加 `review.DocReReviewGuidance`；自审与得分过低两条不加。
- `review.go` 的 `gateGuidance` 在第 2 步后追加复审分档一行，内嵌 `review.ReReviewGuidance`，不新增 stamp 状态（stamp 按内容寻址，没有可靠的「本分支此前已审」读法，条件句交给 agent 判断）。
- 三份 SKILL 与 `qualitygen.go`、集成笔记按上述口径改文字；`DocReReviewGuidance` 与 SKILL 文字由 `TestSkillsCarryResumeTier` 钉住关键短语。

### 关键取舍
- 文档复审单独成常量，不复用 `ReReviewGuidance`：后者的措辞绑定代码（鉴权、数据迁移、并发、公共 API、`forge review pass`），套到文档上判据不成立。
- 协议 B 保留整体重验：交付前整体重验一遍，仍是防假绿的底线；只把修复轮的重复全量改为增量。单轮修复的任务总成本不变，多轮修复才省。
- Stop hook 不读 stamp 判断复审：改状态机的代价大于一句条件指引。

### 已知局限
- 续用依赖宿主（Claude Code 的 `SendMessage`）；宿主不支持续用时退回新派并粘贴上轮 findings 全文，节省有限。
- 首次 `SendMessage` 续用可能整段缓存未命中（anthropics/claude-code#44724，之后的续用才命中），省的主要是重新探索的开销。
- 协议 B 的增量重验判据靠 agent 判断「接缝」，判错会漏验；整体重验兜底。

## 验证与回滚
- `go vet ./...`；`go test ./... -count=1 -timeout 30m`。
- skills 改动后：重建二进制、`forge plugin pack`、`forge compat snapshot` 重新固定。
- 回滚即 revert 本分支的提交；只改文案与常量，不涉及数据面与命令面。
