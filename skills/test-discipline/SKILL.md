---
name: test-discipline
description: "测试质量守卫（提交前 diff 审查 + 断言防注水）。Use when: 审查提交 diff 时、准备 git commit/push 前、编写或修改测试时、准备为让测试通过而改断言时、声称验证通过前自查断言强度时。专注检测断言弱化、验证假阳性、区分单元测试与端到端验证。SKIP: 需要 TDD 循环指引时（用 tdd-cycle）、测试运行失败找根因（用 systematic-debugging）、纯文档变更。"
metadata:
  pattern: reviewer
  domain: testing
  triggers: [{"event":"PreToolUse","match":"Bash","keywords":["git commit","git push"],"cooldown":120}]
---

# 测试质量守卫

测试质量守卫。提交前 diff 审查清单：检测断言弱化、验证假阳性、区分单元测试与端到端验证。

## 铁律 1：禁止断言弱化

测试失败时，问"代码哪里写错了"，而不是"测试怎么才能过"。

**禁止的懒惰修改：**
- `t.Fatal` → `t.Log`（降低严重性让 CI 绿）
- 严格状态码检查 → 接受任意状态码
- 跳过校验逻辑
- `// TODO: fix this later` + 删掉断言
- `assert_eq!` → `assert!` 或省略（Rust）
- `.toBe(expectedValue)` → `.toBeTruthy()` 或 `.toBeDefined()`（JS）
- 添加 `t.Skip`（Go）或 `#[ignore]`（Rust）跳过失败测试

如果 tempted to weaken an assertion，写 TODO 注释说明假设的根因，然后去修代码。

## 铁律 2：单元测试通过 ≠ 验证完成

`cargo test` / `npm test` / `go test` 通过只说明各模块内部逻辑正确，不说明集成链路通了。

**必须端到端验证的场景：**
- 涉及 IPC 的变更（如 Tauri）→ 必须跑真实 IPC 通信
- 涉及 HTTP API 的变更 → 必须用真实 HTTP 连接测试（不是 httptest.NewRequest）
- 涉及前端渲染的变更 → 必须在浏览器中实际看到
- 涉及数据库交互的变更 → 必须跑集成测试（不是 mock）

**"37 tests passed" 不等于 "应用可以用了"。**

## 铁律 3：审查 diff 时检查断言变化

每次提交前审查自己的 diff，显式检查：

- `t.Fatal` / `t.Fatalf` 是否被降级为 `t.Log` / `t.Errorf`
- `assert!` / `assert_eq!` 是否被删除或替换为更弱的断言
- `.toBe(` 是否被替换为 `.toBeTruthy()` / `.toBeDefined()`
- 测试是否被 `t.Skip` / `#[ignore]` / `it.skip` 跳过
- 测试用例是否被整体删除

发现上述变化时，先回答"代码哪里写错了"，不是"怎么让测试通过"。

## 铁律 4：测试要表达规格，不是镜像实现（规格档）

AI 写的测试天然是实现的镜像——实现错了，照着实现写的期望值让测试照样绿（vacuous pass：看起来验证了，实际什么都没拦住）。打破自证环的进攻性手段：让测试来自**规格**而非实现。

**属性测试（property-based testing）——何时升级到这一档：**
- 纯函数 / 解析器 / 序列化-反序列化 / 状态机迁移类逻辑：示例用例只能踩点，属性覆盖整个输入空间
- 从**需求**提炼不变量，不从实现抄期望值。好属性的启发式：
  - 往返律：`decode(encode(x)) == x`
  - 幂等律：`f(f(x)) == f(x)`
  - 结构不变量：排序后元素多重集不变、任何操作后余额非负
  - 新旧等价：重写后的实现与旧实现/参考实现对同一输入输出一致
- 工具：Go `testing/quick`/rapid、Rust proptest/quickcheck、JS fast-check、Python hypothesis
- **属性频繁改 = 属性在镜像实现**——实现重构不该动属性；动了就回到规格重新提炼

**变异测试（mutation testing）——测"测试"的有效性（CI 可选档）：**
- 工具偷偷改源码（翻转边界、删语句、换运算符），有 mutant 存活 = 该路径断言不足——比覆盖率诚实：覆盖率只证明代码被执行过，不断言验证了什么
- 何时值得：核心算法、安全/防作弊相关断言、断言强度被质疑的存量测试；成本高于单测，按项目启用
- 工具：Stryker（JS/TS/C#）、mutmut/cosmic-ray（Python）、cargo-mutants（Rust）、go-mutesting（Go）

## Gotchas

- **Vue Test Utils**: `wrapper.text()` 不包含 `<input>` 元素的值。测输入框用 `wrapper.findAll("input")[0].element.value`。
- **单例 composable**: `useGraph` 等模块级 ref 的 composable，测试必须在 `beforeEach()` 调 `clearAll()`。
- **共享测试服务器**: 多个测试共享同一 server 实例时，注意中间件（限流等）和数据库状态隔离。
- **httptest.NewRequest vs 真实连接**: 案例唯一真相源：integration-test-architecture `references/test-architecture-example.md`「坑 1/坑 2」，此处不复制——结论：`httptest.NewRequest` 固定 `RemoteAddr`，暴露不了 TCP 层 bug（如限流 key 含端口），网络行为用真实连接测。

## Common Rationalizations

| 借口 | 现实 |
|---|---|
| "测试差不多过了" | 差不多 ≠ 过。CI 绿了不代表断言检查了正确的东西 |
| "t.Fatal 降级成 t.Log 只是提醒" | 降级 = 失败不再阻断 = 测试名存实亡。这是断言弱化，回去修代码 |
| "这次跳过测试回头补" | 跳过的测试从不会回头补。要么修代码让测试过，要么明确标 xfail 并跟踪 |
| "用 mock 表演一下证明逻辑通" | mock 表演 ≠ 真实测试。真实依赖没验证，集成时必坏 |
| "单元测试过了应该没问题" | 单元测试只证明模块内部逻辑。IPC/HTTP/渲染/DB 必须端到端验证 |
| "httptest.NewRequest 够了" | httptest 固定 RemoteAddr，TCP 层 bug（限流 key 含端口）测不出来，用真实连接 |
| "删掉这个失败测试让 CI 绿" | 删测试 = 掩盖 bug。测试失败说明代码错了，修代码不删测试 |
| "严格检查改成范围接受更灵活" | 灵活性是 bug 的温床。严格断言是测试的价值，放宽 = 废测试 |
| "属性测试太重，示例够了" | 示例只踩点不覆盖输入空间；纯函数/解析器类一个属性顶一堆用例，且不会被实现带偏 |
| "覆盖率 90% 还不够吗" | 覆盖率只证明代码被执行过，不断言验证了什么——vacuous pass 的测试覆盖率照样 100% |

## Red Flags — STOP 检查断言变化

如果你正要做这些，STOP：
- 把 `t.Fatal`/`assert_eq!`/`.toBe(x)` 改成更弱的形式（`t.Log`/`.toBeTruthy()`）
- 添加 `t.Skip`/`#[ignore]`/`it.skip` 跳过失败测试
- 删除整个失败测试用例
- 把严格状态码检查改成 `>= 200` 范围接受
- 加 `// TODO fix later` 然后删断言

**以上都意味着：测试在告诉你代码错了，你却让测试闭嘴。修代码，别修测试。**

## 参考

- 反模式案例集：[references/anti-patterns.md](references/anti-patterns.md)
- 互补：`tdd-cycle`（管先写测试的循环）、`systematic-debugging`（测试失败先找根因）
