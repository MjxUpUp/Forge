---
name: test-discipline
description: "测试质量守卫。Use when: 测试失败时、声称验证通过时、审查提交 diff 时、编写测试时、修改测试让测试通过时。专注检测断言弱化、验证假阳性、区分单元测试与端到端验证。SKIP: 需要 TDD 循环指引时（用 tdd-cycle）、纯文档变更。"
metadata:
  pattern: reviewer
  domain: testing
---

# 测试质量守卫

测试质量守卫。检测断言弱化、验证假阳性、区分单元测试与端到端验证。

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

## Gotchas

- **Vue Test Utils**: `wrapper.text()` 不包含 `<input>` 元素的值。测输入框用 `wrapper.findAll("input")[0].element.value`。
- **单例 composable**: `useGraph` 等模块级 ref 的 composable，测试必须在 `beforeEach()` 调 `clearAll()`。
- **共享测试服务器**: 多个测试共享同一 server 实例时，注意中间件（限流等）和数据库状态隔离。
- **httptest.NewRequest vs 真实连接**: `httptest.NewRequest` 使用固定 `RemoteAddr`，无法暴露 TCP 层面的 bug（如限流 key 包含端口）。

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
