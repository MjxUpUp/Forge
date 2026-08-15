# 过度工程审查清单（delete-list）

## 目录

- [核心产出格式：delete-list](#核心产出格式delete-list)
- [根因诊断：懒惰阶梯（7 rung）](#根因诊断懒惰阶梯7-rung)
- [1. 重造标准库轮子（stdlib）](#1-重造标准库轮子stdlib)
- [2. 引依赖做几行事（native）](#2-引依赖做几行事native)
- [3. 单实现抽象层（yagni）](#3-单实现抽象层yagni)
- [4. 投机灵活性（yagni）](#4-投机灵活性yagni)
- [5. 死代码与未用分支（delete）](#5-死代码与未用分支delete)
- [6. 可压缩的手写循环（shrink）](#6-可压缩的手写循环shrink)
- [7. bug 修症状不修根因（shrink）](#7-bug-修症状不修根因shrink)
- [与 ai-cheat-patterns / review-checklist 的分工（不要重复查）](#与-ai-cheat-patterns-review-checklist-的分工不要重复查)
- [边界：什么不是过度工程（不要 flag）](#边界什么不是过度工程不要-flag)
- [检测要点速查](#检测要点速查)

AI 生成代码时高频出现的**过度工程**——重造标准库轮子、为单一实现造抽象层、引新依赖做几行事、投机灵活性、死代码。与 [review-checklist.md](review-checklist.md) 第 3 维度的 YAGNI/DRY 散点 + 第 4 维度的滥用检测是**升级关系**——这里把"是不是过度工程"从散点 checkbox 升级为可执行的 **delete-list**：每条发现给"删什么 + 替代什么"，目标是让 diff 变短。AI 高频过度构建（ponytail 实测单任务可达 94% 冗余代码）。

**铁律：发现过度工程 → 必须解决（删代码/内联/换标准库，降低维护负担）——所有过度工程发现一视同仁，不分级。**

`category` 归 `maintainability`（少数纯结构性的归 `design`）——**不新增** over-engineering 类，保持与 [subagent-contract.md](subagent-contract.md) 的 category 维度正交（过度工程本质是可维护性问题，和 sql-safety 归 security 同模式）。

---

## 核心产出格式：delete-list

每条发现一行，照 ponytail-review 格式：

`L<line>: <tag> <what>. <replacement>.`

多文件 diff 用 `<file>:L<line>: <tag> ...`。

结束给唯一量化指标：`net: -<N> lines possible.`（N = 删改后净减行数）。无可删 → `Lean already. Ship.` 并停。

5 类 tag：

- `delete:` 死代码 / 未用的灵活性 / 投机功能。替代：nothing。
- `stdlib:` 手写标准库已有的东西。替代：指出标准库函数名。
- `native:` 引依赖做平台已做的事。替代：指出原生特性。
- `yagni:` 单实现的抽象 / 无人设过的 config / 单调用的层。替代：内联，直到第二个出现。
- `shrink:` 同逻辑更少行。替代：给出更短形式。

---

## 根因诊断：懒惰阶梯（7 rung）

delete-list 标的是"删什么"，懒惰阶梯标的是"为什么该删"——反向用，问代码违反了哪一级。**阶梯只在"先读懂问题"后用**，不替代理解（懒解决方案，不懒阅读——不理解的 diff 是第二个 bug）。

按序问，第一个成立的就停：

1. **真需要建吗？**（YAGNI——投机需求跳过，一行说明）
2. **代码库已有？** 复用——先 grep，重写几步外的工具是 AI 最常见的 slop。
3. **标准库能做？** 用它。
4. **原生平台特性？** `<input type="date">` vs picker 库；CSS vs JS；DB constraint vs app code。
5. **已装依赖能做？** 用它——不为几行事引新依赖。
6. **一行能搞定？** 一行。
7. **最后才：** 最小可工作实现。

阶梯是反射不是研究项目——但它运行在理解问题**之后**，不是之前。

---

## 1. 重造标准库轮子（stdlib）

**指纹**：手写标准库已经提供的功能（校验、解析、序列化、集合操作、日期格式化）。

**示例**：
```diff
+ class EmailValidator {
+   constructor(email) { this.email = email }
+   isValid() {
+     return this.email.includes('@') && this.email.includes('.')
+   }
+ }
+ // 27 行手写校验，边界还错（a@b 也过、不查 TLD）
```

**为什么是问题**：27 行手写 + 边界错 + 要测试 + 要维护。标准库/语言内置一行解决，还覆盖了你想不到的边界。AI 喜欢从零造轮子，因为它"看起来在干活"。

**检测方法**：
- 看到手写的校验/解析/格式化/集合运算 → 问标准库/语言内置有没有
- delete-list：`L12-38: stdlib: 27 行 EmailValidator。语言内置格式校验一行；真校验靠发确认邮件。`

---

## 2. 引依赖做几行事（native）

**指纹**：新增依赖做平台/已装依赖本就能做的事（moment.js 只为一次格式化、lodash 只用 `_.get`、picker 库做一个原生控件能做的输入）。

**示例**：
```diff
+ import moment from 'moment'   // 包体 ~67KB
+ const formatted = moment(date).format('YYYY-MM-DD')
```
```diff
+ npm install date-picker-lib   // 404 行 wrapper + stylesheet + timezone 讨论
```

**为什么是问题**：新依赖 = 供应链风险 + 包体膨胀 + 版本锁定 + 学习成本，换几行代码。AI 默认"找库"，不先问平台原生能不能做。原生 `<input type="date">` 0 依赖、可访问性内置。

**检测方法**：
- 新增 import/require/go get → 问：原生/标准库/已装依赖能做吗？
- `package.json`/`Cargo.toml`/`go.mod` 新增项 → 这是几行能做的事吗？
- delete-list：`L4: native: moment 只为一次格式化。Intl.DateTimeFormat，0 依赖。`

---

## 3. 单实现抽象层（yagni）

**指纹**：接口/抽象类/trait 只有一个实现；工厂只产一种产品；Repository 抽象只有一个具体 Repository。

**示例**：
```diff
+ abstract class AbstractRepository<T> {
+   abstract find(id: string): T
+   abstract save(item: T): void
+   // ... 5 个抽象方法
+ }
+ class UserRepository extends AbstractRepository<User> { /* 唯一实现 */ }
+ // 全项目只有 UserRepository，没有第二个 Repository
```

**为什么是问题**：抽象是为"应对变化"建的，但只有一个实现 = 没有变化需要应对。这层抽象是纯成本：多一层间接、改实现要改两处、读代码要跳转。AI 尤爱建"以备将来"的抽象层。

**检测方法**：
- 看到 Abstract/Interface/trait → 全局 grep 实现数：只有一个？内联
- delete-list：`repo.ts:L88: yagni: AbstractRepository 只有一个实现。内联进 UserRepository，等第二个 Repository 出现再抽。`

---

## 4. 投机灵活性（yagni）

**指纹**：配置项/参数/feature flag 永远只有一个值；"为将来扩展"预留的 hook/插槽从未被第二种用法使用。

**示例**：
```diff
+ const config = {
+   maxRetries: 3,           // 从未改过
+   timeout: 5000,           // 从未改过
+   strategy: 'exponential', // 唯一支持的策略
+   enableCache: true        // 从未设为 false
+ }
```

**为什么是问题**：可配置性是延迟决策的成本——每多一个配置项，测试矩阵翻倍、文档多一段、用户多一个困惑点。投机配置（"将来可能要调"）= 永远不会调的死参数。AI 喜欢把所有东西做成可配置，逃避做决策。

**检测方法**：
- 配置项/参数 → 问：它有第二个值吗？没人设过 = 删，硬编码常量
- delete-list：`L30-35: yagni: 4 个 config 项全用默认值。硬编码常量，需要时再加配置。`

---

## 5. 死代码与未用分支（delete）

**指纹**：未被调用的函数/方法/类；永假的分支（被废弃 flag 守卫）；注释掉的代码；"以防万一"的参数从未传入。

**示例**：
```diff
+ function legacyParse(input) { /* 40 行 */ }
+ // 全项目无人调用 legacyParse
```
```diff
+ if (DEPRECATED_FLAG && false) {
+   validate(input)   // 永不执行
+ }
```

**为什么是问题**：死代码是纯负担——读代码的人要理解它、改代码的人怕动它、它还制造"这段有用"的假象。AI 生成时常留下"备用实现"和"先留着"的分支。

**检测方法**：
- 未被调用的 export/函数 → grep 调用点，无 = 删
- 永假分支 → 字面 `false`/`0` 守卫 = 删（**注意**：与 ai-cheat-patterns 的 dead-branch-insertion 重叠——那边是蓄意作弊，这里是无害遗留；按动机分流，先查 cheat-patterns）
- 注释掉的代码 → git 有历史，删
- delete-list：`L52-71: delete: legacyParse 无人调用。nothing，git 里有历史。`

---

## 6. 可压缩的手写循环（shrink）

**指纹**：手写循环/累加器做标准库一行能做的转换（建 dict、过滤、map、求和、去重）。

**示例**：
```diff
+ const result = {}
+ for (let i = 0; i < keys.length; i++) {
+   result[keys[i]] = values[i]
+ }
```

**为什么是问题**：手写循环更长、更易 off-by-one、意图不直观。标准库的 `Object.fromEntries`/`dict(zip(...))`/`zip` 一行表达意图，还经过优化。AI 生成循环是默认，没想到内置。

**检测方法**：
- 看到 for 循环建集合/累加 → 问：有内置 comprehension/zip/fromEntries 吗？
- delete-list：`L30-44: shrink: 手写循环建 dict。Object.fromEntries(keys.map((k,i)=>[k,values[i]]))，1 行。`

---

## 7. bug 修症状不修根因（shrink）

**指纹**：只修复报告点名的那个调用路径，但共享函数的所有 sibling 调用者仍带同样的 bug；或在每个调用点打补丁而非修一次共享函数。

**示例**：
```diff
  # ticket: "用户登录失败"
+ if (user.email) {           # 只在 login 路径补了空检查
+   login(user)
+ }
  # 但 register() / resetPassword() / updateProfile() 都调 normalize(user)
  # 且 normalize 对 null 崩溃——sibling 调用者全坏
```

**为什么是问题**：只补 ticket 路径 = 留下所有 sibling 调用者继续坏，下次报另一个路径的同样 bug。修共享函数一次 = 更小 diff + 修全所有调用者。这是"最小 diff"和"最小修复"的区别——最小 diff 补在错的层，是第二个 bug 的温床。

**检测方法**：
- 看到要改的函数 → grep 所有 caller，确认 bug 是否在共享层
- delete-list：`auth.ts:L15: shrink: login 补空检查。改 normalize() 加空守卫，1 处修全 4 个 caller。`

---

## 与 ai-cheat-patterns / review-checklist 的分工（不要重复查）

- **本清单**：查**过度工程**（删什么、换什么）——`category=maintainability`/`design`，必须解决。产出 delete-list（可删 + 替代）。
- **[ai-cheat-patterns.md](ai-cheat-patterns.md) 的 dead-branch-insertion / coverage-erosion**：查**作弊**（`if (false)` 装作干活、加分支不测）——必须解决。永假分支在本清单是 `delete`（无害遗留），在 cheat-patterns 是蓄意作弊——按动机区分，先查 cheat-patterns。
- **[review-checklist.md](review-checklist.md) 第 3 维度 YAGNI/DRY + 第 4 维度滥用检测**：散点 checkbox（"无将来可能用到的抽象""单例/工厂不需要时是负担"）。本清单是这些散点的**可执行升级**——不只标记"有过度工程"，还给出 delete-list 格式的删改方案。

三者正交：同一段代码可以"过度工程（本清单）+ 且是作弊伪装（cheat-patterns）"。审查时按动机分流，不重复给同类建议。

---

## 边界：什么不是过度工程（不要 flag）

照 ponytail "When NOT to be lazy"——这些**永远不在砍削之列**，flag 它们是审查者的错误：

- **信任边界校验**：用户输入/API 响应/文件内容的校验、消毒、转义。"几行能做的别引依赖"不适用于安全校验。
- **防数据丢失的错误处理**：catch 不是"多余"，是不让数据丢。（空 catch 是错误吞没，归 error-handling，不是过度工程。）
- **安全措施**：认证、授权、加密、限流、CSRF。"为将来加的限流"不是 yagni。
- **可访问性**：aria 标签、键盘导航、语义化 HTML。原生 `<input>` 胜过 picker 库的部分原因就是 a11y 内置。
- **硬件/物理校准**：时钟漂移、传感器偏差——留校准旋钮，最小模型看不到。
- **用户显式要求的完整版本**：用户要的 = 建，不 re-argue。
- **最小自检 / `assert` demo / 一个测试**：非平凡逻辑留一个可运行 check 是底线，不是 bloat——呼应 Forge"测试伴随变更"。但反向也成立：**一行 `assert` 能覆盖的事不引测试框架**，本身就是反过度工程的例子。

---

## 检测要点速查

| 模式 | 信号 | tag |
|---|---|---|
| 重造标准库轮子 | 手写校验/解析/格式化 | `stdlib:` |
| 引依赖做几行事 | 新增 deps 做平台能做的 | `native:` |
| 单实现抽象层 | Abstract/Interface 只一个实现 | `yagni:` |
| 投机灵活性 | config/参数只有一个值 | `yagni:` |
| 死代码/未用分支 | 未调用函数、永假分支（非作弊） | `delete:` |
| 可压缩循环 | 手写循环建集合 | `shrink:` |
| 修症状不修根因 | 只补 ticket 路径 | `shrink:` |

**不分级**：所有过度工程发现一视同仁，都必须解决——上表不再列"级别"列，tag 仅用于 delete-list 分组（删什么），不暗示优先级。

`eng-reviewer` 子 agent（轨道 B）按本清单产出 delete-list，每条 finding 用 `category=maintainability`（纯结构性用 `design`）。审查通过（所有发现已解决）后运行 `forge review pass` 标记当前 diff 已审——满足 task-complete 门禁前置，解除非 task 模式 Stop hook 拦截。
