---
name: implementation-discipline
description: "代码实施任务的全程纪律编排（从动手到交付）。Use when: 接到范式级重构/交互重设计任务时（先过 prototype-confirmation 原型门，由其自身 SKIP 判断是否真触——纯后端无视觉形态不触）、接到编码任务准备动手时、写实现代码前、准备 git commit / push / 提 PR 前、声称\"完成了/搞定了/可以了/验证通过了\"前、用户问\"做完了吗/验证了吗/能提交吗/提交代码\"时、长任务中途怀疑自己走偏或凭记忆猜类型 API 时。本 skill 是全程编排器，各阶段指向专门 skill（审查用 code-review-gate、测试用 tdd-cycle、验证用 verification-driver）。SKIP: 纯调研出报告（research-workflow）、单一运行时 bug 根因（systematic-debugging）、纯编译错误修复（compile-fix-loop）、只改一行配置验证一下、纯架构决策记录（architecture-decision-record）、只需规划不需执行纪律（用 dev-workflow）。"
metadata:
  pattern: pipeline + gate
  domain: development-discipline
  composes: prototype-confirmation, evidence-based-proposal, tdd-cycle, test-discipline, verification-driver, code-review-gate, systematic-debugging, dev-lookup, release-readiness
---

# 代码实施纪律链

从计划到交付，**每个阶段都有门控**：过了门才能进下一阶段。纪律不是"记住规则"，是"在门控点强制检查"——因为纪律在压力下（长任务、疲惫、上下文压缩）会崩塌，门控是最后防线。

**核心原则：声称"完成"前，必须有刚跑出来的证据，不是"应该通过"。**

## When to Use / When NOT to Use

**Use**：任何编码任务从动手到交付的全程；每次想说"完成了""验证通过""可以提交"之前；准备改代码、提交代码之前；长任务中段自检。

**SKIP**：纯调研（research-workflow）、单 bug 根因（systematic-debugging）、纯编译错（compile-fix-loop）、纯架构决策（architecture-decision-record）、只改一行配置。

## 阶段门控链（每阶段过门才进下一个）

### 前置门 — 范式确认（条件触发，最高频违反）

**若是范式级改动**（起底重构 / 重新设计前端交互 / 多功能点交互方案），进阶段 0 之前先过原型确认门：把方案做成可逐功能点确认的 HTML 原型，让用户对每个功能点的形态拍板（认可/调整/推翻）后再动手。**自决范式、写完代码再让用户看 = 越权 + 返工。**

→ 用 **prototype-confirmation**（含自带 HTML 模板，硬要求导出+持久化）。

**红线**：范式级改动直接写代码不先出原型 / 心里"定好范式"就动手 / 把"best-judgment proceed"当默认。

**不是范式级改动**（bug 修复、单功能点小改、纯后端逻辑）→ 跳过本门，直接进阶段 0。

### 阶段 0 — 计划：方案要有依据

**门控**：提方案前先验证实际环境（API 签名、运行时行为、现有实现），不凭通用知识推荐。方案必须回答"解决当前什么具体断点"。

→ 详细规则用 **evidence-based-proposal**。

**红线**：凭通用行业知识直接推荐方案 / 方案不回答"当前断点在哪" / 没看现有代码就 redesign。

### 阶段 1 — 实施前：先确认再写 + 选最省力路径（现有 skill 未专管，最高频违反）

**动键盘前必须确认 4 件事，缺一不可：**

1. **现有类型/枚举/字段名** —— 用项目内自定义类型（Error 变体、Config 字段）前先 `grep` 确认确切名字，不凭记忆。
2. **不熟悉的 API 签名** —— 用没把握的 crate/库/框架前先查文档或源码，不猜签名。
3. **修改意图先说出口** —— 改之前先用一句话说"我要改 X，因为 Y，改成 Z"，再动手。
4. **选最省力的实现路径（懒惰阶梯）** —— 动键盘前按序问，第一个成立的就停：
   1. 真需要建吗？（YAGNI——投机需求跳过，一行说明）
   2. 代码库已有？复用——先 `grep` / dev-lookup，重写几步外的工具是 AI 最常见的 slop
   3. 标准库能做？用它
   4. 原生平台特性？`<input type="date">` vs picker 库；CSS vs JS；DB constraint vs app code
   5. 已装依赖能做？不为几行事引新依赖
   6. 一行能搞定？一行
   7. 最后才：最小可工作实现

   第 1、2 件事（现有类型 / API）是阶梯第 2 级（复用现有）的具象化；阶梯是"确认没有更省力路径"的总纲。**bug 修 = 根因不是症状**：`grep` 所有 caller，修共享函数一次，别只补 ticket 路径——更小 diff 且不留坏 sibling（最小 diff 补在错的层 = 第二个 bug）。

**红线**：凭记忆拼类型名 / 猜 API 签名 / 边想边改不说意图 / 不看源码就断言"某功能不存在或某行为如此" / 跳过阶梯直接写"最小实现"之外的东西（为单一实现造抽象层、引依赖做几行事、手写标准库已有的）。

**边界（照 ponytail "When NOT to be lazy"）**：信任边界校验 / 错误处理 / 安全 / 可访问性 / 硬件校准 / 用户显式要求的完整版本**不在砍削之列**——阶梯只缩短解决方案，不缩短阅读、不砍安全。非平凡逻辑仍留一个可运行 check（呼应"测试伴随变更"；但一行 `assert` 能覆盖的事不引框架）。

**查 API 签名/库用法** → 用 **dev-lookup**。

### 阶段 2 — 实施：先看测试失败再写代码

**门控**：RED（写失败测试，看着它失败）→ GREEN（最小代码通过）→ REFACTOR。没看过测试失败 = 不知道它测对了东西。

→ 详细循环用 **tdd-cycle**。

**红线**：先写代码后补测试 / 跳过 RED 直接 GREEN / 想着"这次跳过 TDD"。

### 阶段 3 — 验证：最高级别测试通过才叫完成

**门控**：跑项目已有的**最高级别**自动化验证。单元测试通过 ≠ 完成。
- 有 E2E → E2E 全过；有集成测试 → 集成测试全过；都没有 → 先写一个再报告完成。
- 涉及 HTTP/IPC/渲染/DB 的变更 → 必须端到端验证，不能只 mock。
- 断言不能为让测试过而弱化（t.Fatal→t.Log、严格→宽松、跳过）。

→ 端到端验证用 **verification-driver**；断言守卫用 **test-discipline**。

**红线（最高频）**：用"应该通过"代替实际运行 / 只跑单元测试就声称完成 / 弱化断言让测试过 / 声称"项目没有 e2e"而没去查代码里有没有 `#[ignore]` live 测试或集成测试目录。

### 阶段 4 — 审查：提交前 diff 自检

**门控**：审查自己的 diff，**同时看 `+` 行和 `-` 行**，专查：
- 断言是否被降级（t.Fatal→t.Log、`toBe(x)`→`toBeTruthy()`、删 expect）
- 测试是否被跳过（t.Skip / #[ignore] / it.skip）
- 新增 `@ts-ignore` / `eslint-disable` / `#[allow(...)]` 藏警告
- 改名的符号是否更新了所有调用点

→ 完整审查用 **code-review-gate**；diff 断言检查用 **test-discipline** 铁律 3。

**红线**：只看新增行不看删除行 / 没查断言强度变化 / "改了几行不用审"。

### 阶段 5 — 交付：提交卫生 + 门控顺序（现有 skill 未专管）

**门控 A — 提交卫生**：
- `git add` 只加与当前任务直接相关的**源代码**文件。
- **禁止混入**：`docs/` `doc/` `research/` `notes/` 等文档目录、设计文档/调研报告、`.idea/` `.vscode/` 编辑器配置、`.claude/` `.pi/` `.openclaw/` 等工具工作目录、生成产物（`dist/` `target/` `build/`）。
- 提交前必跑 `git diff --cached --name-only`，逐个确认无无关文件。

**门控 B — 门控顺序**：
- 遵循项目既定的门控顺序，不跳步、不颠倒。例：若项目有质量门（如 `forge`），顺序是 `task-implement → task-verify → task-complete → git commit`——**commit 必须在 complete 之前**（complete 会清空 active task ref，之后提交源码会被 quarantine）。
- 不确定顺序时，先查项目的质量工作流文档，不凭记忆排。

**红线**：`git add .` / `git add -A` 一把梭 / 提交了 docs 或 .claude / 门控跳步或颠倒。

### 阶段 6 — 发布（条件触发：本次任务含发布/上线动作）

**门控**：编码到 commit（阶段 0-5）≠ 能安全上线。发布是不可逆动作（tag 推、镜像发、迁移跑），按下按钮前必须过发布 readiness 清单。

→ 发布门禁用 **release-readiness**（M1-M7 mandatory + R1-R5 recommended）。

**触发条件**：任务范围含打 tag / 发版 / 上线 / 灰度 / 生产迁移 / 镜像 push。纯功能开发不触发本阶段，止于阶段 5 commit。

**边界**：本 skill 管编码到 commit 的纪律，release-readiness 管 commit 后到上线的发布风险，两者衔接不重叠（release-readiness 的 SKIP 也把编码交付纪律指回本 skill）。

**红线**：跳过 release-readiness 直接发版 / "代码测过了就能上线"（发布风险单测管不到）/ tag 推错想 force push 修。

## Common Rationalizations（纪律型核心——堵借口的最高信号）

| 借口 | 现实 |
|---|---|
| "范式我心里清楚了，直接写代码更快" | 范式级改动写完用户说"不是我要的"→ 整块返工。先出原型让用户逐项拍板（prototype-confirmation） |
| "改几行不用先查类型/API" | 几行里的幻觉类型名/猜错的签名就是 bug 源头。grep + 查文档 10 秒，调试 1 小时 |
| "先写代码再补测试也一样" | 不一样。没看过测试失败，你不知道它测对了东西。先写代码后补的测试常迁就实现 |
| "单元测试过了应该没问题" | 单元测试只证模块内部逻辑。HTTP/IPC/DB/渲染必须端到端，mock 表演不算 |
| "这个测试先 skip 回头补" | 跳过的测试永不回头补。要么修代码让测试过，要么标 xfail 跟踪 |
| "t.Fatal 降级成 t.Log 只是提醒" | 降级 = 失败不再阻断 = 测试名存实亡。这是断言弱化，回去修代码 |
| "应该能通过" | "应该"不是验证。运行命令、看到输出、断言状态，才是验证 |
| "项目没有 e2e 所以单测够了" | 先去代码里查有没有 `#[ignore]` live 测试 / `tests/` 集成目录 / playwright 配置。没查就声称"没有"= 偷懒 |
| "git add . 方便点" | 一把梭会混入 docs/.claude/生成产物。`git diff --cached --name-only` 逐个确认 |
| "这个 .claude/ 文件也提交了吧" | 工具工作目录绝不提交。加 .gitignore，不是 add |
| "门控顺序差不多就行" | 差不多 = quarantine 风险。complete 前 commit 是硬顺序，查项目工作流文档 |
| "这次跳过审查，改动很小" | 单行的断言弱化/类型抑制就在小改动里。小改动更要审 |
| "用户催得急先提交" | 急不是降低纪律的理由。提交后回滚成本 >> 提交前查 10 分钟 |
| "这个抽象以后会用到" | 以后 = YAGNI 的温床。只有一个实现就别抽，等第二个出现再抽——那时你才知道抽象该抽在哪 |
| "先引个依赖快速搞定" | 几行能做的事别引依赖。新依赖 = 供应链风险 + 包体 + 版本锁定。先问标准库 / 已装依赖 / 原生能不能做 |
| "这个功能将来可能需要" | 将来可能 = 现在不需要 = YAGNI。需要时再加，那时需求才清晰。预留的 hook 多半永不被第二种用法用 |

## Red Flags — STOP（看到这些想法，你在 rationalize）

- "这个范式我定好了，直接写代码" → STOP，先出原型让用户逐项确认（prototype-confirmation）
- "这个类型名我记得是…" → STOP，grep 确认
- "这个 API 大概是这么调" → STOP，查文档/源码
- "先写代码，测试回头补" → STOP，先写失败测试
- "测试失败了，把它改成 t.Log" → STOP，断言弱化，去修代码
- "单元测试全绿，完成了" → STOP，跑最高级别测试 / 端到端
- "应该能通过" → STOP，运行它看输出
- "项目没有 e2e" → STOP，先查代码里有没有 live/集成测试
- "git add . 一下" → STOP，只 add 相关源文件
- "门控顺序我记不清了大概…" → STOP，查项目工作流文档
- "改了几行不用审 diff" → STOP，小改动的作弊密度最高
- "先建个 AbstractX 基类以备扩展" → STOP，单实现别抽象，内联到第二个出现
- "这个功能将来可能需要，先加上" → STOP，YAGNI，需要时再加
- "引个库快速搞定这几行" → STOP，先查标准库 / 已装依赖 / 原生能不能做

## Gotchas（来自真实长会话失败——最高信号）

- **凭猜测下结论被用户当场打脸**：agent 不查 pi 源码就断言"pi 没有探测 model 参数的手段"，用户纠正"看下 pi 源码怎么处理的不用猜""那 pi 是不是有探测手段"。→ 凡涉及第三方项目行为，读源码验证，不靠通用知识。
- **声称完成但没做 e2e，被追问才发现**：agent 报"修复完成"，用户问"是否有进行真实的 e2e 验证？"——答不上来。→ "完成"前必须有端到端证据，单元测试不算。
- **声称"项目没有 e2e"其实是没查**：agent 说项目无 e2e 基础设施，用户纠正"那你之前是怎么做 e2e 的，之前不是跑通过么"——项目其实有 `#[ignore]` live GLM 测试。→ 声称"没有 X"前先 grep 代码。
- **把已被否定的假设当新结论**：用户已否决"闪屏是用了旧安装包"，agent 压缩后又提类似假设，用户再纠正"闪是透明终端"。→ 上下文压缩后，已纠正的结论会混回事实，对用户纠正过的事要显式标记"已被否决"。
- **凭假设用户行为**：agent 假设用户用旧安装包，用户"我就是用的新的安装包"。→ 涉及用户环境/行为的判断，问或查，不假设。
- **断言弱化的假阳性**：某测试只断言 `done`（任务结束）不断言 `status==Completed`，于是 auth 失败导致 `Done(Failed)` 仍"通过"。→ 测试要断言**终态正确**，不只是"走到了 Done"。
- **提交混入工具目录**：长会话里"禁止提交 docs/、.claude/、.idea/、.vscode/"被反复强调——说明反复发生。→ 默认不 add 这些，养成 `git diff --cached --name-only` 习惯。
- **门控顺序颠倒**：先 `forge task complete` 后 `git commit`，导致源码被 file-sentinel quarantine。→ complete 前 commit 是硬顺序。
- **委托子代理后未验证**：子代理说"完成"≠ 真完成。至少读它改过的文件 + 跑全项目编译。→ 见 agent-delegation 的两阶段审查。

## 一次性完成原则

接到任务全力执行完毕，不主动拆 Phase/阶段。只有在单次执行确实无法完成（需等待外部资源）时才标记断点。**写计划/策略文档不等于完成任务**——代码运行并通过验证才算完成。

## 与其他 skill 的分工（不重复，各阶段指向）

| 阶段 | 本 skill 做什么 | 指向谁做细节 |
|---|---|---|
| 前置 范式确认 | 门控：范式级改动先出原型让用户逐项拍板 | prototype-confirmation |
| 0 计划 | 门控：方案要有依据 | evidence-based-proposal |
| 1 实施前 | **门控：先确认再写 + 选最省力实现路径（懒惰阶梯）**（本 skill 专管） | dev-lookup（查 API） |
| 2 实施 | 门控：先失败测试 | tdd-cycle |
| 3 验证 | 门控：最高级别测试 + 不弱化断言 | verification-driver + test-discipline |
| 4 审查 | 门控：diff 自检断言变化 | code-review-gate + test-discipline |
| 5 交付 | **门控：提交卫生 + 门控顺序**（本 skill 专管） | — |
| 6 发布 | 门控：发布前 readiness（条件触发：任务含发布动作） | release-readiness |

**与 dev-workflow 的区别**：dev-workflow 管"做什么/谁来做"（创意提炼、技术栈检测、路由委托），在计划明确后退场；本 skill 管"怎么做才不踩纪律坑"（执行期门控），在动手后接管。
