---
name: tdd-cycle
description: >
  测试驱动开发强制循环（RED-GREEN-REFACTOR）。Use when: 实现任何功能或修 bug 前、在写实现代码之前、重构行为变更时、想"这次跳过 TDD 吧"时。
  先写失败的测试→看着它失败→写最小代码通过→重构。SKIP: 测试质量守卫/断言防注水（用 test-discipline）、一次性原型/配置文件/生成代码（与人确认后可跳过）。
metadata:
  pattern: pipeline + gate
  domain: testing
---

# TDD Cycle — 测试驱动开发

先写测试。看它失败。写最小代码通过。

**核心原则：没看过测试失败，你不知道它测对了东西。**

**违反规则的字面就是违反规则的精神。**

## The Iron Law

```
NO PRODUCTION CODE WITHOUT A FAILING TEST FIRST
```

先写了代码再补测试？删掉，重来。

**无例外：**
- 不留作"参考"
- 不在写测试时"改用"它
- 不看它
- 删就是删

从测试重新实现，到此为止。

## When to Use

**总是：** 新功能、修 bug、重构、行为变更

**例外（与人确认后）：** 一次性原型、生成代码、配置文件

想"就这次跳过 TDD"？STOP，那是 rationalization。

## Red-Green-Refactor 循环

### RED — 写失败的测试

写一个最小测试展示**应该发生什么**。

**要求：**
- 测一个行为（名字里有 "and"？拆开）
- 名字清楚描述行为
- 用真实代码（除非万不得已不 mock）

```typescript
// ✅ 好：清楚名字，测真实行为，一件事
test('retries failed operations 3 times', async () => {
  let attempts = 0;
  const operation = () => {
    attempts++;
    if (attempts < 3) throw new Error('fail');
    return 'success';
  };
  const result = await retryOperation(operation);
  expect(result).toBe('success');
  expect(attempts).toBe(3);
});

// ❌ 坏：含糊名字，测的是 mock 不是代码
test('retry works', async () => {
  const mock = jest.fn().mockRejectedValueOnce(new Error())
    .mockRejectedValueOnce(new Error()).mockResolvedValueOnce('success');
  await retryOperation(mock);
  expect(mock).toHaveBeenCalledTimes(3);
});
```

### Verify RED — 看着它失败（强制，不准跳）

```bash
npm test path/to/test.test.ts   # 或 cargo test / go test / pytest
```

确认：
- 测试**失败**（不是 error）
- 失败信息符合预期
- 因为功能缺失而失败（不是笔误）

**测试通过了？** 你在测已存在的行为，改测试。
**测试 error 了？** 修 error，重跑直到正确失败。

### GREEN — 最小代码

写**最简单**能通过测试的代码。不加功能、不重构别的代码、不"顺手改进"。

```typescript
// ✅ 好：刚够通过
async function retryOperation<T>(fn: () => Promise<T>): Promise<T> {
  for (let i = 0; i < 3; i++) {
    try { return await fn(); }
    catch (e) { if (i === 2) throw e; }
  }
  throw new Error('unreachable');
}

// ❌ 坏：过度设计（YAGNI）
async function retryOperation<T>(fn, options?: {
  maxRetries?: number; backoff?: 'linear'|'exponential'; onRetry?: ...
}) { /* 没人要这些 */ }
```

### Verify GREEN — 看着它通过（强制）

```bash
npm test path/to/test.test.ts
```

确认：测试通过；其他测试还过；输出干净（无 error/warning）。

**测试失败？** 修代码，不修测试。**其他测试失败？** 立刻修。

### REFACTOR — 清理（仅在 green 后）

去重复、改名字、提辅助函数。**保持测试 green，不加行为。**

## 为什么顺序重要

**"我写完代码后补测试验证"** → 写完后的测试立刻通过，立刻通过证明不了任何事：可能测错了东西、可能测了实现而非行为、可能漏了你忘的边界情况、你从没见它抓到 bug。先写测试逼你看它失败，证明它真测了点东西。

**"我已经手动测过所有边界"** → 手动测试是 ad-hoc：没记录、代码变了不能重跑、压力下容易漏、"我试过能用" ≠ 全面。自动化测试是系统化的，每次同样跑。

**"删掉 X 小时工作是浪费"** → 沉没成本谬误。时间是沉没的。选择：删了用 TDD 重写（高信心）vs 留着补测试（低信心，多半有 bug）。留不可信的代码才是技术债。

## Common Rationalizations

| 借口 | 现实 |
|---|---|
| "太简单不用测" | 简单代码也会坏，测试只要 30 秒 |
| "我之后测" | 立刻通过的测试证明不了任何事 |
| "之后补测试目的一样" | 后补测 = "它做了啥"；先测 = "它该做啥" |
| "已手动测过" | ad-hoc ≠ 系统化，无记录不能重跑 |
| "删 X 小时太浪费" | 沉没成本，留不可信代码才是技术债 |
| "留着当参考，先写测试" | 你会改它，那就是后补测，删就是删 |
| "得先探索" | 可以探索，但探索代码扔掉，用 TDD 重来 |
| "测试难 = 设计不清" | 听测试的，难测 = 难用 |
| "TDD 会拖慢我" | TDD 比事后调试快，务实 = 先测 |
| "现有代码没测试" | 你在改进它，给现有代码补测试 |

## Red Flags — STOP 删代码重来

- 先写代码后补测试
- 测试立刻通过
- 解释不出测试为什么失败
- "之后补"测试
- "就这次" rationalize
- "我已手动测过"
- "后补测目的一样"
- "是精神不是仪式"
- "留作参考"/"改用现有代码"
- "已花 X 小时，删了浪费"
- "TDD 是教条，我务实"
- "这次不一样因为..."

**以上都意味着：删代码，用 TDD 重来。**

## Bug 修复也用 TDD

发现 bug？写一个复现该 bug 的失败测试，走 TDD 循环。测试证明修复生效且防回归。

**永远不写测试就修 bug。**

## Verification Checklist（标完成前）

- [ ] 每个新函数/方法都有测试
- [ ] 实现前看过每个测试失败
- [ ] 每个测试因预期原因失败（功能缺失，非笔误）
- [ ] 写了最小代码让每个测试通过
- [ ] 所有测试通过
- [ ] 输出干净（无 error/warning）
- [ ] 测试用真实代码（万不得已才 mock）
- [ ] 覆盖边界和错误场景

勾不全？你跳了 TDD，重来。

## 与其他 skill 的分工

- **test-discipline**：测试**质量**守卫（防弱断言、防假阳性、区分单测/端到端）——本 skill 管"先写测试的循环"，test-discipline 管"测试本身写对了没"。互补，都用
- **systematic-debugging**：Phase 4 写失败测试时用本 skill
- **compile-fix-loop**：编译错误不走 TDD（编译错误是类型系统已告知根因）

## Final Rule

```
产品代码 → 必有先失败的测试
否则 → 不是 TDD
```

无人许可，无例外。
