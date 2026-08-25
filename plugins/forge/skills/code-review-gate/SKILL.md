---
name: code-review-gate
description: "通用研发代码审查门控（提交前/合并前强制拦截）。Use when: 开发任务完成准备 git commit / push / 提 PR 前、说\"审查代码 / code review / 检查代码质量 / 看看能不能提交 / 代码写得怎么样 / 帮我 review\"时、想拦截 AI 生成的屎山进入主干时、审查任意语言代码变更时。SKIP: 纯测试质量守卫（用 test-discipline）、编译报错（用 compile-fix-loop）、查单一 API/库用法（用 dev-lookup）、运行时 bug 排查（用 systematic-debugging）、项目级验收（用 project-acceptance）、Rust 专项结构化审查（用 rust-code-review，与门控叠加不替代）。"
metadata:
  pattern: reviewer + gate
  domain: code-review
---

# 代码审查门控

开发完成 → 提交前的强制审查门控。**双轨检查**：① AI 作弊模式扫描（防止 AI 为"看起来完成"而注水代码）+ ② 传统软件工程规范（SOLID / 设计模式 / Clean Code / 可维护性）。

## 核心原则

- **不只是查 bug**：更要查"这是不是在制造未来维护负担"。能跑 ≠ 没屎。
- **AI 代码有可识别的作弊指纹**：断言弱化、错误吞没、假重构、类型抑制——人类审查常漏，是 AI 屎山的核心来源。
- **提交前是成本最低的拦截点**：技术债只增不减，"以后再优化" = 永不优化。
- **门控而非建议**：所有发现的问题都必须解决（修复，或结合背景论证为何不需修）才能提交——没有"可跳过"的级别。分级（block/fix/suggest）会暗示"低级别可忽略"，与"每个发现都值得认真对待"冲突，故移除。
- **独立上下文审查**：派只读子 agent 审，不自审。主 agent 刚写完代码有"它是对的"确认偏误——单行的 `@ts-ignore`、空 catch、删断言就在自审盲区。独立上下文是底线，不是规模函数。

## When to Use / When NOT to Use

**Use when:**
- 开发任务完成，准备 `git commit` / `push` / 提 PR
- 用户说"审查一下" / "code review" / "检查下代码质量" / "看看能不能提交" / "代码写得怎么样"
- 想防止 AI 生成的代码进入主干
- 任何语言的代码变更审查（TypeScript/Python/Go/Java/Rust/前端/后端）

**SKIP（路由到更专业的 skill）:**
- **测试断言防注水** → `test-discipline`
- **编译报错** → `compile-fix-loop`
- **查 API 签名 / 库用法** → `dev-lookup`
- **运行时 bug 根因排查** → `systematic-debugging`
- **整个项目验收** → `project-acceptance`
- **Rust 专项结构化审查**（unsafe/异步/workspace 深度）→ `rust-code-review`（与门控叠加，不替代双轨）

## 审查流程

### 步骤 1：确定范围（变更行 + 波及面 + 环境）

**只审 diff 变更行会漏掉「修复 A 引入副作用表现在 B」**——B 是未变更的调用方、数据流下游或运行时环境，恰是修复引入 bug 的高发区（纯 diff 行审查 = 只看「改了什么字符」，不看「改动是否真的有效」）。范围必须三层覆盖：

**① 变更行（基础）**

- 有 diff/PR → 审变更行
- 有文件路径 → 审该文件
- "审查全部" → 聚焦自上次提交修改的源文件（`src/`、`lib/`、`app/` 等，跳过 `node_modules`/`dist`/`vendor`）

```bash
git diff --stat                    # 看改了哪些文件
git diff                           # 看具体变更（含未暂存）
git diff --cached                  # 看已暂存的变更
```

**重要：审查要同时看 `+` 行和 `-` 行。** AI 作弊常在删除行里（删断言、降级匹配器、删测试块）。

**② 波及面（必须，最易漏）**

变更行动了函数签名 / 类型 / 常量 / 行为时，追影响范围：

- **调用方**：谁调了它？参数 / 返回值 / 副作用变化会不会让调用方失效？——改名/删符号/改签名时的完整检查规则见下方「改名/删符号后的调用方检查」节（全库唯一真相源，此处不复制）
- **数据流下游**：改动涉及的 DB schema / API 响应 / JWT claims / 配置结构 / 共享状态，下游消费方的类型与语义还匹配吗？（跨层数据类型不一致是「单测过、集成挂」的经典根因）
- **共用符号**：被改的常量 / 类型 / 工具函数被哪些模块共用？改一处是否波及多处？

**③ 环境依赖（必须，纯 diff 行审查的盲区）**

改动依赖运行时环境时，主动验证环境行为是否如代码假设——这部分**不在 diff 行里**，逻辑推演发现不了，只能主动查：

- **文件来源**：代码用 `git diff` / `git ls-files` / `git status` 取文件列表时，gitignored 文件（`docs/`、`dist/`、生成物）会被系统性漏掉——验证或加文件系统兜底。
- **路径解析**：跨平台分隔符、`filepath.Base` 对盘根、符号链接、Windows 反斜杠。
- **配置 / 平台**：默认值与缺失字段、跨 shell 行为（bash vs cmd/PowerShell）、引号腐蚀（ASCII 双引号被转中文弯引号）。

**原则**：能用测试 / 实跑验证的就别只靠推演——推演是 reviewer 的下限，不是上限。逻辑推演发现不了的（环境行为、数据流实际类型），必须有测试或实跑兜底。

## 改名/删符号后的调用方检查（全库唯一真相源）

变更对 export / 函数 / 类型 / 常量做了**改名、删除或改签名**时，调用方检查是强制项。本节是全库唯一真相源——其他 skill（agent-delegation / implementation-discipline / review-batch 等）引用本节，不复制规则。

1. **全仓 grep，含 gitignored**：`grep -rn "oldName" .`——**不要用 `git grep`**（漏 gitignored 的调用方：`docs/`、生成代码、`.env`、构建产物里的引用）；**也不只看本次 diff / SHA 内变更**（调用方常在改动文件之外）。
2. **三种符号变动都要查**：改名（假重构高发：符号改了名但调用者没更新 → 运行时 ReferenceError 或死代码）、删符号、改签名（参数 / 返回值变化让旧调用方静默失效）。
3. **完成判定**：grep 结果零残留旧符号引用才算同步完成；有残留即发现（必须解决，不是建议）。

> Forge 项目：若任务是设计阶段产物审查（非纯代码实现），`task-verify` 已按文件路径推断 DesignPhases 并落盘，须加载对应 phase checklist 作为补充检查项——条件化细节见 [references/forge-integration.md](references/forge-integration.md)「环节感知加载」节。非 forge 项目跳过。

### 步骤 2 前置：证据强度校准（forge 项目）

> Forge 项目：双轨审查前先做证据强度校准——`forge review status` 的 ratio 档位（Strong/Weak/Unverified）决定审查动作（Weak 加核、Unverified 必核声称的验证是否真发生，并升级跨模型对抗式 critic）。机制与操作详见 [references/forge-integration.md](references/forge-integration.md)「证据强度校准」节。非 forge 项目跳过本步（fallback 到纯静态双轨）。

### 步骤 2：双轨审查（核心，缺一不可）

#### 轨道 A：AI 作弊模式扫描（最高信号，先做）

加载 [references/ai-cheat-patterns.md](references/ai-cheat-patterns.md)，逐条检查 11 类 AI 作弊指纹。

**这是人类审查最容易漏、危害最大的模式。** 命中任一条即必须解决（修复或论证），不依赖其他维度即可否决提交。

#### 轨道 B：传统软件工程规范

加载 [references/review-checklist.md](references/review-checklist.md)，按维度检查：
- **正确性 & 逻辑**（边界、分支覆盖、竞态）
- **错误处理**（不吞错、错误传播、边界输入）
- **可维护性**（SRP、DRY、YAGNI、抽象层级、依赖方向；过度工程——重造轮子/不必要抽象/可删死代码/引依赖做几行事，delete-list 格式见 [references/over-engineering-checklist.md](references/over-engineering-checklist.md)）
- **设计模式**（滥用 vs 缺失、过度抽象 vs 概念泄漏）
- **安全**（AI 安全反模式 Top 10：硬编码密钥、SQL 拼接、缺输入验证、缺限流；破坏性 SQL——DROP/TRUNCATE/无 WHERE DELETE/GRANT ALL/生产直连——详见 [references/sql-safety-checklist.md](references/sql-safety-checklist.md)）
- **性能**（N+1、循环内 I/O、不必要的克隆）
- **测试有效性**（断言是否验证真实行为，不只看覆盖率）
- **可读性**（命名、函数长度、控制流、注释价值）

### 步骤 3：发现分析与解决要求（不分级）

每条发现必须包含**四要素**，缺一不可——这是"结合背景分析"的硬要求：

1. **位置**（`文件:行号`）
2. **问题**（引用具体代码片段，不是泛泛而谈）
3. **背景分析**：为什么是问题——结合这段代码的上下文（它在做什么、调用关系、数据流、与设计的偏离）。不是"违反了 SRP"这种标签，而是"这个类同时管 X 和 Y，改 X 时会牵连 Y，因为…"。
4. **解决方向**：具体怎么修（可执行），或结合背景论证为何不需修（如"看似重复但分属不同抽象层，合并会耦合"）。

**不分级**：不给发现打 block/fix/suggest 或 major/minor/nit 标签。分级会暗示"低级别可忽略或推迟"，导致 suggest/nit 永不被修——而 AI 屎山常藏在这些"看着不大"的细节里（单行 `@ts-ignore`、空 catch、删断言）。每个发现都是真实问题，都需认真回应。

**叠加专项审查的输出协议（裁决）**：专项审查 skill（rust-code-review / frontend-code-review / ai-generated-ui-review）保留 block/fix/suggest 分级，但与本 gate 叠加执行时，分级只表达处理顺序、不表达可忽略——**block 以下级别（fix/suggest）也必须逐条显式回应**（修复，或结合背景论证不需修），最终门控以步骤 5「全部解决才可提交」为准。不允许把专项的 suggest 当"可选"悬置。

### 步骤 4：产出结构化报告（发现清单，不分组分级）

```markdown
## 代码审查报告

**审查范围**：N 个文件，M 行变更
**结论**：✅ 可提交（所有发现已解决） / 🚫 不可提交（有 N 项未解决）

### 发现清单（每项含 位置 + 问题 + 背景分析 + 解决方向）

1. `path/file.ext:42`
   - **问题**：[引用代码片段，具体描述]
   - **背景分析**：[结合上下文，为什么是问题——不是贴标签]
   - **解决方向**：[具体怎么修，或论证为何不需修]

2. `path/file.ext:88`
   - **问题**：…
   - **背景分析**：…
   - **解决方向**：…

### 整体评价（一句话，不打分）
- **最突出的风险/改进点**：[列出最突出的几项（顺序仅供修复参考，所有发现都必须解决）]
```

**不分级 = 不打分**：移除 1-10 评分（设计质量 / 可维护性 / AI 屎山风险）。评分是另一种分级，会暗示"7 分还行"，同样稀释门控力度。报告聚焦"发现清单 + 是否全部解决"。

每个发现必须含：**位置 + 问题 + 背景分析 + 解决方向** 四要素。

### 步骤 5：门控决策（全部解决才可提交）

- **有未解决的发现** → **"🚫 不可提交，以下 N 项必须先解决"** + 逐项列 位置 + 背景分析 + 解决方向
- **所有发现都已解决**（修复，或结合背景论证不需修）→ **"✅ 可提交"**

**没有中间态**：不存在"可提交但建议先修"——所有发现都必须在提交前解决。分级体系下 fix/suggest 的"可推迟"是技术债复利的入口，本 skill 明确关闭它。若某发现确实不该修（误报 / 超范围），在报告里结合背景论证清楚，使其进入"已解决（论证不需修）"状态，而非悬置。

**禁止模糊结论**：不说 `基本可以`、`问题不大`、"看着改改"。给出明确的提交/不提交判断。

> Forge 项目：审查通过（所有发现已解决）后须运行 `forge review pass` 标记当前 diff 已审——未标记则 task-complete 门禁 / Stop hook 会拦截提交与会话结束（机制见 [references/forge-integration.md](references/forge-integration.md)「自动触发」节）。

## 子 agent 化：独立上下文审查（防自审盲区）

主 agent 写完代码直接审 = 自己批改自己的作业，确认偏误让单行作弊（`@ts-ignore`、空 catch、删断言）漏过。**步骤 2 的双轨审查必须派只读子 agent 执行**，子 agent 不共享主 agent 的实现上下文。

**规模只决定 1 vs 2，不决定是否派子 agent**（独立上下文是底线，不是规模函数）：
- **小变更**（<100 行 / 单文件）→ 派 **1 个**独立子 agent，跑双轨 A+B
- **大变更**（≥100 行 / 多文件）→ 派 **2 个并行**子 agent：`cheat-detector`（轨道 A 11 指纹）+ `eng-reviewer`（轨道 B 8 维度），独立上下文 + 交叉验证

预设契约（职责 / 只读工具 / 结构化输出 schema）见 [references/subagent-contract.md](references/subagent-contract.md)。子 agent **只读不写**——审查与修复分离，避免"边审边改"妥协。派发方式按所在 agent：Claude Code 用 Task tool（`subagent_type: general-purpose`，prompt 注入契约）；codex 等用各自子任务机制，契约相同。

> Forge 项目：本 skill 有两条自动挡让审查成为提交/结束的硬前置——task-complete 门禁的 ReviewPassed 硬前置 + 非 task 流程的 Stop hook（未审变更拦截会话结束，最多拦 3 次后 advisory 放行）。机制与误触发防护见 [references/forge-integration.md](references/forge-integration.md)「自动触发」节。

### 审查-修复-复审闭环（多轮盖章的复审义务）

审查发现的问题修复之后，**必须重新派只读子 agent 复审修复本身**，才能再次 `forge review pass`——修复者不能自证修复合格（第一轮的确认偏误在修复时同样存在：改错位置、只修表面、修复引入新问题，都是修复者自己看不见的）。快照机制（task-complete 门禁）只强制闭环的**形状**（改码后必须重新盖章），不检验复审**实质**；复审实质由本协议 + `forge review pass` 的自助刷新守卫共同承担——距上次盖章基线有**源码内容变更**时，裸 `forge review pass` 会被**拒绝**，须用 `--note "<复审结论>"` 记录复审结论后才放行。

- 复审范围：修复 diff 本身 + 修复是否真正解决了原发现（对照第一轮报告逐项核）+ 修复引入的新问题（回归、调用方、边界）。
- 复审输出与首轮同协议（结构化报告 + 结论行）；复审发现新问题 → 再修复 → 再复审，直到复审 PASS。
- **禁止**：修复后不派复审直接盖章（自证）；把"测试通过"当复审（测试验证行为，复审验证修复的正确性与完整性——两者不可互替）；用同一个子 agent "继续看看"（复审需要新鲜独立上下文对修复 diff 施加第一轮同等的怀疑）。

> Forge 项目：`forge review pass` 检测到距上次审查基线有源码变更时，裸盖章会被拒绝——正规路径是复审后 `forge review pass --note "<复审结论>"`；确认变更无需复审时可用 `--acknowledge-changes` 显式自我承担（checklog 记 WARN 级 self-refresh 审计，事后审计可见）。非源码变更（amend commit message、保内容 rebase 等）不改变内容指纹，无需确认；同状态重复盖章（瞬态重试）保持静默。

## AI 作弊模式速查（核心，先扫这个表）

来自 327 个真实 AI PR 的挖掘（27 个被维护者明确确认为作弊，工具召回率 93%）。**命中任一即必须解决（不得跳过）：**

> Forge 项目：`task-verify` 的 cheat-scan 已机械扫任务新增行的 7 类模式（type-suppression / error-swallow / dead-branch / comment-only-fix / comment-as-debt / phantom-import / path-assumption）并记 `checklog:cheat-scan`——审查前先查 `forge trace`，这 7 类已被 deterministic 判过，子 agent **跳过它们**，把精力放到下表其余需语义判断的模式和轨道 B 上（机制详见 [references/forge-integration.md](references/forge-integration.md)「cheat-scan 预扫」节）。

| 作弊类型 | 指纹 | 为什么是问题 |
|---|---|---|
| **断言弱化** assertion-strip | 测试文件中断言数量净下降 | 看起来测试还在，实际保护没了 |
| **错误吞没** error-swallow | 空 catch / 只注释的 catch / `let _ = err` | 静默吃掉错误，问题永不暴露 |
| **假修复** no-op-fix | 改了测试没改源码，或反之 | 声称修复但根本没动问题点 |
| **假重构** fake-refactor | 符号改名但调用者没更新 | 运行时 ReferenceError 或死代码 |
| **覆盖率侵蚀** coverage-erosion | 加源码分支没加测试 | 新逻辑零保护 |
| **测试松绑** test-relaxation | 严格匹配→宽松匹配（`toBe`→`toBeTruthy`）、`toEqual`→`toMatchObject` 部分 | 测试通过但不再验证真实行为 |
| **类型抑制** type-suppression | 新增 `@ts-ignore` / `eslint-disable` / `#[allow(...)]` / `type: ignore` | 把警告藏起来而非解决 |
| **幻觉 mock** mock-of-hallucination | mock 项目中不存在的模块/API | 测试通过因为测的是假东西 |
| **注释充数** comment-only-fix | 声称修复，改动全是注释 | 包装成工作但没动逻辑 |
| **异常上下文丢失** exception-rethrow-lost-context | `throw err` → `throw new Error(msg)` 丢了 `cause`/原始栈 | 调试时丢失根因 |
| **死分支** dead-branch-insertion | `if (false)` / `if (1 === 2)` 等永假分支 | 看起来处理了边界，实际永不执行 |

**检测要点**：审查 diff 时专门看"删除的断言行""新增的 ignore/disable""改名的符号是否更新了所有调用点"（规则见「改名/删符号后的调用方检查」节）。详见 [references/ai-cheat-patterns.md](references/ai-cheat-patterns.md)。

## Rationalizations（堵借口）

| 借口 | 现实 |
|---|---|
| "代码能跑就行" | 能跑 ≠ 没屎。AI 作弊代码也能跑过 CI，但埋了维护炸弹 |
| "这只是风格问题" | 设计味道不是风格。SRP 违反会让下一个改动牵连 5 个类 |
| "测试已经覆盖了" | 断言弱化的测试"覆盖"了但零保护。查断言强度不查覆盖率 |
| "用户没要求查 SOLID" | 用户要"保证代码质量"时就是要查。不查 = 放任屎山 |
| "这部分以后再优化" | 以后 = 永不。提交前是成本最低的拦截点 |
| "AI 写的都是这样" | AI 作弊是可识别的固定模式，不是不可抗力。门控就是拦截它们 |
| "只改了几行不用审" | 单行的 AI 作弊（断言弱化、类型抑制）就在这几行里 |
| "这是用户的需求" | 用户要 WHAT，HOW 的质量是审查职责。需求合理不代表实现合理 |
| "重构风险大先不动" | 不动的成本（技术债复利）通常 > 重构成本。至少在报告中列出并解决 |
| "没时间查那么细" | 提交后回滚的成本 >> 提交前查 10 分钟。这是门控存在的意义 |

## Red Flags（看到这些想法 = STOP，你在放任屎山）

- "这个 catch 空着应该没事" → error-swallow，必须解决
- "测试断言少了几个没关系" → assertion-strip，必须解决
- "`@ts-ignore` 先加上以后再说" → type-suppression，必须解决
- "只看新增行就行" → AI 作弊常在删除行/修改行里
- "这个类有点大但还能用" → SRP 违反，必须解决
- "这段重复代码先复制粘贴" → DRY 违反，必须解决
- "用户没提安全就不用查" → 安全是底线，默认查
- "类型是 any 但能跑" → 类型系统被绕过，必须解决
- "跑过测试就行" → 断言弱化的测试全绿但零保护
- "这个 mock 让测试通过了" → 先确认 mock 的模块真实存在

## Gotchas（从实际失败积累——最高信号）

- **审查 diff 不只看 `+` 行**：AI 作弊常在 `-` 行（删断言、降级匹配器、删测试块）。`git diff` 的删除侧是高发区。
- **跑测试 ≠ 测试有效**：断言弱化的测试全绿但零保护。看断言数量和强度，不只看通过状态。
- **"重构"是高频作弊词**：AI 喜欢用 rename 包装成重构，实际没更新调用者。看到 rename 必查全局引用（规则见「改名/删符号后的调用方检查」节）。
- **设计模式不是越多越好**：过度抽象（YAGNI 违反）也是屎山。单例/工厂/抽象层在不需要时就是负担。
- **不要纠结能自动化的事**：格式化交给 prettier/rustfmt/gofmt，lint 交给 eslint/clippy。审查聚焦设计/正确性/作弊/安全。
- **AI 安全是默认项**：硬编码密钥、SQL 字符串拼接、缺输入验证、缺限流——无论用户有没有提都要查。AI 代码 XSS 失败率 86%，72% Java AI 代码含漏洞（[Veracode 2025 GenAI Code Security Report](https://www.veracode.com/blog/genai-code-security-report/)）。
- **破坏性 SQL 默认查**：DROP TABLE/TRUNCATE/无 WHERE 的 DELETE/GRANT ALL/生产直连/不可逆迁移——AI 生成迁移或数据修复脚本时高频，一次失误清空生产库。SQL **注入**（拼接）和**破坏性**（语法合法但摧毁数据）是两类，都要查。见 [references/sql-safety-checklist.md](references/sql-safety-checklist.md)。
- **过度工程默认查**：重造标准库已有的东西、为单一实现造抽象层（AbstractRepository 只一个实现）、引新依赖做几行能做的事、永不改值的 config——AI 高频过度构建（ponytail 实测单任务可达 94% 冗余）。不是标记"有问题"就完事，是给 delete-list（删什么 + 换什么），让 diff 变短。见 [references/over-engineering-checklist.md](references/over-engineering-checklist.md)。
- **"看起来无害"的小改动最危险**：一行 `@ts-ignore`、一个空 catch、一个删掉的 `expect`——这些是 AI 最爱的捷径。
- **检查错误路径而非快乐路径**：正常路径通常能工作。审查出错时会发生什么——日志记了吗？错误传播了吗？还是被吞了？
- **跨层数据类型一致性**：DB → 后端结构体 → API 响应 → 前端类型，任一层不一致 = 运行时 bug。这是 AI 代码高频问题。
- **依赖即风险**：AI 会幻觉不存在的包（Slopsquatting，5-21% 的 AI 建议包不存在）。新增依赖必查是否真实存在、是否可信。

## 与其他 skill 的分工

- **test-discipline**：测试质量守卫（断言防注水）。本 skill 检测到 `assertion-strip` 时可联动深查。
- **compile-fix-loop**：编译报错修复。本 skill 不处理编译错误（那是另一类问题）。
- **systematic-debugging**：审查中发现的运行时 bug 用它排查根因。
- **rust-code-review / frontend-code-review / ai-generated-ui-review**：语言与来源专项审查，保留 block/fix/suggest 分级——叠加时输出协议按步骤 3 的裁决段执行（block 以下也必须显式回应）。
- **evidence-based-proposal**：审查给出的修复建议要基于实际，不凭空想。
- **dev-lookup**：审查中遇到不确定的 API 签名/库用法，用它快速确认。

## 参考

- AI 作弊模式详解（11 类 + 检测方法 + 示例）：[references/ai-cheat-patterns.md](references/ai-cheat-patterns.md)
- 传统维度完整审查清单：[references/review-checklist.md](references/review-checklist.md)
- DB/SQL 破坏性操作审查清单（DROP/TRUNCATE/无 WHERE DELETE/GRANT ALL/生产直连/不可逆迁移）：[references/sql-safety-checklist.md](references/sql-safety-checklist.md)
- 过度工程审查清单（重造轮子/单实现抽象/引依赖做几行事/投机灵活性/死代码，delete-list 5 类 tag + 懒惰阶梯根因诊断）：[references/over-engineering-checklist.md](references/over-engineering-checklist.md)
- 子 agent 预设契约（职责 / 只读工具 / 输出 schema）：[references/subagent-contract.md](references/subagent-contract.md)
- Forge 集成（环节感知加载 / 证据强度校准 / cheat-scan 预扫 / 自动触发，仅 forge 项目适用）：[references/forge-integration.md](references/forge-integration.md)

**数据来源**：swarm-orchestrator（327 真实 AI PR 挖掘，93% 召回率）、mgreiler/code-review-checklist（1060⭐ 业界权威清单）、Arcanum-Sec/sec-context（150+ 源提炼的 AI 代码安全反模式）、ponytail（懒惰阶梯 + delete-list，MIT，实测 ~54% 更少代码、过度构建场景达 94%）。
