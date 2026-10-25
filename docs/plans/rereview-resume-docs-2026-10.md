---
status: review
owner: MjxUpUp
ask: 批准给四处复审路径补「续用原评审者」口径；范围是口径统一，不承诺收益；协议 B 续用一档（REQ-6）可单独否决
tier: mini
---

# 修复方案 | 复审路径补「续用原评审者」分档（2026-10）

## TL;DR
请批准：在 doc-review、非 task 模式 Stop hook、code-review-gate 的双 reviewer、adversarial-verification 四处补「小修订续用首轮评审者」的口径。范围是口径统一，不承诺收益：没有子代理启动次数的遥测，没有基线数字，本批也不度量。

风险最高、收益最小的一档是协议 B 续用验收者（REQ-6）：续用者上下文里有前轮输出，防假绿只靠一句无法核验的指令。批准时可以单独否决这一档，其余三处不受影响。

代价（相对基线 c7bad7f1，复现命令 `git diff --stat c7bad7f1..HEAD`）：24 个文件，Go 非测试源码净增约 45 行，其余是测试、镜像、decisions 与生成物；`ReReviewGuidance` 从 267 字增到 451 字（+184，+69%），它会进入每个项目生成的 CLAUDE.md 常见错误表；SKILL 步骤 5 变长。收益上限：文档门禁最多 3 轮，第 3 轮仍新派，每个任务最多省 1 次冷启动（第 2 轮）、外加 HEAD 前移而文档未变时的一次确认；75-79 区间还要多派一个全量第二评委。

## 问题与目标
- 动机：用户口述反馈（无记录）：子代理评审出问题、修复后又新开一个重审，token 浪费明显。排查确认 doc-review 步骤 5 明文要求打回后「重新派独立子代理评审」（c7bad7f1 的 `skills/doc-review/SKILL.md` 第 69 行），其余三处是同构缺口。
- 目标：小修订续用首轮评审者做增量复核；重写结论、大改、即将进行第 3 轮评审时仍新派全量。独立性来自评审者看不到作者推理，不来自冷启动；代码评审已有同口径分档（`review.ReReviewGuidance`，见 `internal/review/rereview.go` 注释与 `TestReReviewGuidanceContract`），外推到文档的依据是同一机制，文档侧收益未验证。

## Non-goals
- 不改 research-workflow 的核验子代理；不改文档门禁的判定（阈值 75、轮次上限、自审标记），只改给 agent 的指引文案；不改 `ReReviewGuidance` 既有两档判据。
- 不给协议 B 加增量重验档：「修一轮就过」的常见路径上要先增量、PASS 前再整体，比原流程多一次，且削弱每个修复轮整体重验的防假绿底线。

## 备选
- 什么都不做：弃用。3 轮上限下打回后的每一轮都是冷启动，代码侧已有同口径分档；净收益算式见 TL;DR 的代价与上限。
- 最小口径（SKILL 与门禁文案各加一句「小修订可续用」）：弃用。没有判据，agent 无从判断何时新派，也没有防锚定约束（不给作者推理、对抗立场不变、记分口径）。
- 只改 SKILL、不改门禁文案：弃用。门禁文案是被打回时实际读到的位置。
- 文档评审一律续用、不设升档：弃用。重写结论、大改时需要新视角。
- 协议 B 增量档：弃用；协议 B 维持新派验收者：未选，新派者要重读验收点与代码，省不了探索。

## 需求与验收
- REQ-1 doc-review SKILL 的打回步骤分增量复核与全量评审两档，旧的「重新派独立子代理评审」字面量消失
  accept[REQ-1]: go test ./internal/review/ -run ^TestSkillsCarryResumeTier$/^doc-review$ -count=1 :: --- PASS: TestSkillsCarryResumeTier/doc-review (
- REQ-2 `DocReReviewGuidance` 承载两档语义、输入契约、对抗立场、记分口径，且不含会破坏 Sprintf 格式串与原因串拼接的 `%` 和 `; `
  accept[REQ-2]: go test ./internal/review/ -run ^TestDocReReviewGuidanceContract$ -count=1 :: --- PASS: TestDocReReviewGuidanceContract (
- REQ-3 文档门禁在评审未过、评审过期（HEAD 与指纹）、得分低于阈值时引用 `DocReReviewGuidance`，升级人工与自审不引用；终态原因（轮次打满未过、自审）优先于两条过期原因；HEAD 过期时只有「指纹相同且上轮通过并达阈值」才写「文档内容与评审时一致」，任何情形都写明不得凭自述重录旧分数；两条过期原因与低分原因带「已评 N 轮」
  accept[REQ-3]: go test ./internal/taskpipeline/ -run ^TestDocGate_RetryReasonsCarryReReviewGuidance$ -count=1 :: --- PASS: TestDocGate_RetryReasonsCarryReReviewGuidance (
- REQ-4 非 task 模式 `gateGuidance` 分 (a)(b) 两种情形，(b) 内嵌 `ReReviewGuidance`，顺序为派发步、(a)、(b)、标记步，且不断言非 task 路径不存在的「裸标记会被拒」
  accept[REQ-4]: go test ./internal/cli/ -run ^TestGateGuidanceCarriesReReviewTier$ -count=1 :: --- PASS: TestGateGuidanceCarriesReReviewTier (
- REQ-5 `ReReviewGuidance` 与 code-review-gate SKILL 写明首轮并行多 reviewer 时按轨道续用：有发现的轨必续用、`eng-reviewer` 始终续用、`cheat-detector` 无发现时按触发条件续用、首轮零发现两个都续用、不新派补位
  accept[REQ-5]: go test ./internal/review/ -run ^TestReReviewGuidanceContract$ -count=1 :: --- PASS: TestReReviewGuidanceContract (
  accept[REQ-5]: go test ./internal/review/ -run ^TestSkillsCarryResumeTier$/^code-review-gate$ -count=1 :: --- PASS: TestSkillsCarryResumeTier/code-review-gate (
- REQ-6 adversarial-verification 协议 A 复检轮续用原红队，协议 B 保持修完整体重验并补重新实跑、喂验收点清单与续用原验收者
  accept[REQ-6]: go test ./internal/review/ -run ^TestSkillsCarryResumeTier$/^adversarial-verification$ -count=1 :: --- PASS: TestSkillsCarryResumeTier/adversarial-verification (
- REQ-7 生成的 forge-quality 文档回检段提到复审续用
  accept[REQ-7]: go test ./internal/skillgen/ -run ^TestQualityDocGateMentionsResume$ -count=1 :: --- PASS: TestQualityDocGateMentionsResume (
- INV-1 既有 `ReReviewGuidance` 契约（两档、`--note` 刷新基线、无严重性措辞）不退化
  accept[INV-1]: go test ./internal/review/ -count=1 :: ok

验收只钉关键短语存在与旧字面量消失，不证明 SKILL 上下文自洽，自洽由评审承担；本批不度量收益。若合并后要度量，可抽查若干文档任务的第 2 轮是否用了 `SendMessage`，作为后续观察，不作验收项。

## 设计
### 触点
（每行一个路径：PlanScope 按行取第一个路径解析。`plugins/forge/skills/*` 镜像、`compat.snapshot.json`、`skills-inventory.lock` 是生成物，不入触点，已用 `forge task scope add` 收编。）
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
- `DocReReviewGuidance`（`internal/review/rereview.go`，门禁文案的口径源）与 doc-review SKILL 步骤 5 同口径，逐条内容以 SKILL 为准；常量比 SKILL 少的是：满档只能由全量评审给出、`--score` 取四维合计、详略维度升降要列可删段、字符度量命令、「首轮评审者」术语定义、轮次计数规则、HEAD 前移确认的细则。
- `ReReviewGuidance` 增补多 reviewer 续用口径，同时出现在 task-complete 拒绝文案、`forge review pass` 拒绝报错与 `--acknowledge-changes` 提示、`forge review status`、非 task 模式 `gateGuidance`、生成的 CLAUDE.md 常见错误表。
- `docgate.go`：四条原因后追加 `review.DocReReviewGuidance`；终态原因优先；HEAD 过期时按指纹与上轮通过态分两种口径，「内容一致」口径不追加分档指引（其「第 3 轮升档」会在 Round=2 时与「确认即可」冲突）；任何口径都不开免评审通道（`Passed` 与分数由调用方自录）。
- `review.go` 的 `gateGuidance`：(a) 首审、新会话接手、无句柄或非修复（新增功能）按第 2 步新派；(b) 本会话内复审内嵌常量，并注明非 task 模式没有基线刷新、`--note` 只作留痕（常量是 task 与非 task 共用的措辞，这条冲突是有意保留）。不新增 stamp 状态：stamp 内容寻址，读不出「本分支此前已审」。
- 三份 SKILL 与 `qualitygen.go`、集成笔记同步，三份 `decisions.md` 各记一条，镜像、快照、lock 重新生成。

### 关键取舍
- 文档复审单独成常量，不复用 `ReReviewGuidance`：后者措辞绑定代码（鉴权、数据迁移、并发、公共 API、`forge review pass`）。
- 门禁常量与 SKILL 是镜像，不是同一份文本：SKILL 不能引用 Go 常量，两处靠 `TestSkillsCarryResumeTier` 钉关键短语，没有逐字一致检查。
- 多 reviewer 取三条并列（有发现的轨必续用、`eng-reviewer` 始终续用、`cheat-detector` 无发现时按条件续用）：只看发现归属会漏作弊轨与回归核查，只看触发条件会让作弊轨自己的发现无人核验。
- rubric 纪律 6 的 70-79 区间里，70-74 本就未过，所以第二评委只在会放行的 75-79 触发；集成笔记第 4 步已对齐，rubric 本身未改。

### 已知局限
- 续用依赖宿主（Claude Code 的 `SendMessage`）；不支持时退回新派并粘贴 findings。首次续用可能整段缓存未命中（anthropics/claude-code#44724，之后才命中；该 issue 2026-05-24 被 stale bot 以 not planned 关闭，记载见 `skills/agent-delegation/decisions.md`）。
- 每次 `forge task doc-review` 记录让 Round 加 1，仅因 HEAD 前移而确认也消耗一轮；本批只在原因里带出「已评 N 轮」，不改计数逻辑。
- 「命中作弊模式速查表」由复审的 agent 判断，不是机械判据（触及测试、断言、mock 才可机械判断）；作弊轨自己有发现时必续用，不依赖该判断。
- 协议 B 续用验收者没有本轮新鲜度的可核验证据（如运行标识），见 TL;DR 的可单独否决。
- 独立性归因按盖章会话判（`internal/taskpipeline/independence.go` 头注释）：同宿主子代理共用会话 id 时可能被标自审，与续用无关；评审给出分数后由用户在自己的终端记录，是既有流程。
- 「三成」按字符估算，仍是粗口径：一句话改写结论段可能不足三成字符，靠「未重写结论或判定段」判据兜底。

## 验证与回滚
- `go vet ./...`；`go test ./... -count=1 -timeout 30m`。
- skills 改动后：重建二进制、`forge plugin pack`、`forge compat snapshot`、`forge skills inventory --lock` 与 `--verify`。
- 回滚即 revert 本分支的提交；只改文案与常量，不涉及数据面与命令面。
