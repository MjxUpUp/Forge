---
name: code-review-gate
description: "通用研发代码审查门控（提交前/合并前强制拦截）。Use when: 开发任务完成准备 git commit / push / 提 PR 前、说\"审查代码 / code review / 检查代码质量 / 看看能不能提交 / 代码写得怎么样 / 帮我 review\"时、想拦截 AI 生成的屎山进入主干时、审查任意语言代码变更时。SKIP: 纯测试质量守卫（用 test-discipline）、编译报错（用 compile-fix-loop）、查单一 API/库用法（用 dev-lookup）、运行时 bug 排查（用 systematic-debugging）、项目级验收（用 project-acceptance）。"
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

## 审查流程

### 步骤 1：确定范围

- 有 diff/PR → 只审变更行
- 有文件路径 → 审该文件
- "审查全部" → 聚焦自上次提交修改的源文件（`src/`、`lib/`、`app/` 等，跳过 `node_modules`/`dist`/`vendor`）

```bash
git diff --stat                    # 看改了哪些文件
git diff                           # 看具体变更（含未暂存）
git diff --cached                  # 看已暂存的变更
```

**重要：审查要同时看 `+` 行和 `-` 行。** AI 作弊常在删除行里（删断言、降级匹配器、删测试块）。

### 步骤 1.5：环节感知加载（phase-aware，如有设计产物）

如果任务是**设计阶段产物审查**（非纯代码实现），`task-verify` gate 的 `inferDesignPhases` 已根据文件路径推断设计阶段并落盘 `state.DesignPhases`。据此加载对应 checklist，而非只加载通用 `review-checklist.md`：

```bash
# 检查任务是否有 DesignPhases（需有 active task）
forge task status --json 2>&1 | grep -i "design_phase\|DesignPhases"
```

有 `DesignPhases` 时，加载对应环节 checklist 作为补充检查项：

| DesignPhase | 加载 checklist | 说明 |
|---|---|---|
| `requirement` | [references/phase-requirement.md](references/phase-requirement.md) | 需求设计产物（PRD/需求文档）审查 |
| `api` | [references/phase-api.md](references/phase-api.md) | API 设计产物（OpenAPI/proto/接口定义）审查 |
| 其他/无 | 通用 `review-checklist.md` | 代码级审查（默认） |

**加载方式**：把对应 checklist 作为附加检查项，与轨道 A+B 一起执行。不替换而是补充——设计产物审查与代码实现审查关注点不同。

> 无 `DesignPhases` 时（普通代码任务）跳过此步，直接进入步骤 2。

### 步骤 2 前置：加载动态经验库（项目积累，必做）

轨道 A 的 11 类指纹是**静态通用**的。本项目/技术栈还会积累**具体的 gotchas**（存在 `~/.forge/knowledge/gotchas/`）——从过往低分 task、踩坑复盘提炼，比通用规则更贴合实际。先加载它们，作为下面双轨审查的补充检查表：

```bash
forge knowledge list --category gotchas    # 加载全部 gotchas（人审语义核对）
forge knowledge check                       # 机器扫 diff 是否命中已知违规模式（有 patterns 的）
```

- `forge knowledge check` 报违规（exit≠0）→ 这些是被反复确认的坑，直接列为必须解决项，无需再判。
- `forge knowledge list` 每条 gotcha → 作为补充检查项，**和轨道 A 的 11 类指纹一起逐条核对 diff**。无 patterns 的（如"跨层数据类型一致""错误路径要测"）靠语义判断。

**这是经验闭环的消费端**：低分 task 触发的提案（`forge experience`）经 `accept` 进入 `~/.forge/knowledge/`，本步骤在此消费——审查越用越准。闭环全貌见 [references/experience-loop.md]。**无 forge** 时跳过本步（fallback 到纯静态双轨）。

### 步骤 2 前置：证据强度校准（forge 项目，必做）

双轨审查之前，先看本任务的「完成」声明有多少 deterministic 证据支撑——证据弱时，审查重心要从「找代码 bug」扩到「核验声称的验证是否真发生过」。这正对冲 LLM-judge 看不出 agent 跳过前置就声明完成的盲区：deterministic 占比是完成可信度的硬信号（hook/gate 实跑，不可伪造），而 agent 自述可信度低。

```bash
forge review status        # task 模式输出末尾含「证据强度: ratio=X <档位>」+ 校准指令
forge trace <task-ref>     # 证据链分桶行 + Weak/Unverified 警告（同样驱动校准）
```

档位与审查动作：

| 档位 | 含义 | 审查动作 |
|---|---|---|
| **Strong**（ratio≥0.5） | deterministic 占多数，声明可信 | 正常双轨审查即可 |
| **Weak**（ratio<0.5） | agent 自述占多数 | **加核**：声称的测试是否真跑过（找 `test-run` 条目）、门禁是否经 deterministic 路径过，而非纯 agent-claim |
| **Unverified**（零 deterministic） | 声明全无实跑支撑 | **必核**：把「声称做了的验证是否真发生」列为首要审查项，必须见到 deterministic 证据才放行 |

证据弱（Weak/Unverified）时，把 `forge review status` 末尾的校准指令注入子 agent 的审查 prompt（见下「子 agent 化」）。无 forge 时跳过本步（fallback 到纯静态双轨）。

### 步骤 2：双轨审查（核心，缺一不可）

#### 轨道 A：AI 作弊模式扫描（最高信号，先做）

加载 [references/ai-cheat-patterns.md]，逐条检查 11 类 AI 作弊指纹。

**这是人类审查最容易漏、危害最大的模式。** 命中任一条即必须解决（修复或论证），不依赖其他维度即可否决提交。

#### 轨道 B：传统软件工程规范

加载 [references/review-checklist.md]，按维度检查：
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

**禁止模糊结论**：不说"基本可以""问题不大""看着改改"。给出明确的提交/不提交判断。

**审查通过（所有发现已解决）后**：运行 `forge review pass` 标记当前 diff 已审——满足 task-complete 门禁的 ReviewPassed 前置，也解除非 task 模式的 Stop hook 拦截。未标记则门禁/hook 会拦截提交与会话结束。

## 子 agent 化：独立上下文审查（防自审盲区）

主 agent 写完代码直接审 = 自己批改自己的作业，确认偏误让单行作弊（`@ts-ignore`、空 catch、删断言）漏过。**步骤 2 的双轨审查必须派只读子 agent 执行**，子 agent 不共享主 agent 的实现上下文。

**规模只决定 1 vs 2，不决定是否派子 agent**（独立上下文是底线，不是规模函数）：
- **小变更**（<100 行 / 单文件）→ 派 **1 个**独立子 agent，跑双轨 A+B
- **大变更**（≥100 行 / 多文件）→ 派 **2 个并行**子 agent：`cheat-detector`（轨道 A 11 指纹）+ `eng-reviewer`（轨道 B 8 维度），独立上下文 + 交叉验证

预设契约（职责 / 只读工具 / 结构化输出 schema）见 [references/subagent-contract.md](references/subagent-contract.md)。子 agent **只读不写**——审查与修复分离，避免"边审边改"妥协。派发方式按所在 agent：Claude Code 用 Task tool（`subagent_type: general-purpose`，prompt 注入契约）；codex 等用各自子任务机制，契约相同。

## 自动触发：Stop hook 与 task-complete 门禁

本 skill 不再只靠手动唤起——forge 两条自动挡（2026-06-27 落地），让审查成为提交/结束的硬前置：

- **task 流程**：task-complete 门禁有 ReviewPassed 硬前置。派子 agent 审查通过后须运行 `forge review pass` 标记，否则过不了 task-complete 门禁、无法 complete。
- **非 task 流程**：会话结束前 Stop hook 自动检测未审的源码变更，未审则拦截会话结束，additionalContext 指引加载本 skill + 派子 agent + `forge review pass`。同一 diff 反复未审最多拦截 3 次后 advisory 放行（防 Stop 死循环）。

手动查状态：`forge review status`。

**误触发已防护**：纯文档/配置/生成物变更、无变更、commit 后干净工作区、task 模式（由门禁管而非 Stop hook）都不触发拦截。

## AI 作弊模式速查（核心，先扫这个表）

来自 327 个真实 AI PR 的挖掘（27 个被维护者明确确认为作弊，工具召回率 93%）。**命中任一即必须解决（不得跳过）：**

**Forge 项目——4 类已 deterministic 扫描，子 agent 不重复判断**：`task-verify` 的 cheat-scan 已机械扫任务新增行（`+` 行）的 `type-suppression`（`@ts-ignore`/`eslint-disable`/`#[allow]`/`type: ignore`）、`error-swallow`（空 `catch{}`/`except:pass`）、`dead-branch`（`if(false)`/`if(1===2)`）、`comment-only-fix`（某文件新增行全注释零逻辑），命中记 `checklog:cheat-scan`（`forge trace` 可见）并 stderr 列出。审查前先看 `forge trace` 的 cheat-scan 条目——这 4 类已被 deterministic 判过，子 agent **跳过它们**，把精力放到下表其余模式（断言弱化/假重构/幻觉 mock/测试松绑等需语义判断的）和轨道 B 的设计/架构上。这正是"每轮 review 冒新问题"的根因对策：机械模式一次判准，不靠 LLM 每轮重采样。

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

**检测要点**：审查 diff 时专门看"删除的断言行""新增的 ignore/disable""改名的符号是否更新了所有调用点"。详见 [references/ai-cheat-patterns.md]。

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
- **"重构"是高频作弊词**：AI 喜欢用 rename 包装成重构，实际没更新调用者。看到 rename 必查全局引用。
- **设计模式不是越多越好**：过度抽象（YAGNI 违反）也是屎山。单例/工厂/抽象层在不需要时就是负担。
- **不要纠结能自动化的事**：格式化交给 prettier/rustfmt/gofmt，lint 交给 eslint/clippy。审查聚焦设计/正确性/作弊/安全。
- **AI 安全是默认项**：硬编码密钥、SQL 字符串拼接、缺输入验证、缺限流——无论用户有没有提都要查。AI 代码 XSS 失败率 86%，72% Java AI 代码含漏洞。
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
- **evidence-based-proposal**：审查给出的修复建议要基于实际，不凭空想。
- **dev-lookup**：审查中遇到不确定的 API 签名/库用法，用它快速确认。

## 参考

- AI 作弊模式详解（11 类 + 检测方法 + 示例）：[references/ai-cheat-patterns.md](references/ai-cheat-patterns.md)
- 传统维度完整审查清单：[references/review-checklist.md](references/review-checklist.md)
- 经验闭环（experience → knowledge → review）：[references/experience-loop.md](references/experience-loop.md)
- DB/SQL 破坏性操作审查清单（DROP/TRUNCATE/无 WHERE DELETE/GRANT ALL/生产直连/不可逆迁移）：[references/sql-safety-checklist.md](references/sql-safety-checklist.md)
- 过度工程审查清单（重造轮子/单实现抽象/引依赖做几行事/投机灵活性/死代码，delete-list 5 类 tag + 懒惰阶梯根因诊断）：[references/over-engineering-checklist.md](references/over-engineering-checklist.md)
- 子 agent 预设契约（职责 / 只读工具 / 输出 schema）：[references/subagent-contract.md](references/subagent-contract.md)

**数据来源**：swarm-orchestrator（327 真实 AI PR 挖掘，93% 召回率）、mgreiler/code-review-checklist（1060⭐ 业界权威清单）、Arcanum-Sec/sec-context（150+ 源提炼的 AI 代码安全反模式）、ponytail（懒惰阶梯 + delete-list，MIT，实测 ~54% 更少代码、过度构建场景达 94%）。
