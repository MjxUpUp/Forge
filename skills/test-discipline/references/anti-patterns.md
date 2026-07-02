# 测试反模式案例集

从真实项目中收集的测试反模式。每一条都是实际犯过的错误和付出的代价。

## 反模式 1：弱化断言隐藏真 bug

**做了什么**: TestRateLimiter 把 `t.Fatal` 改成 `t.Log`；TestLoginAndTokenRefresh 删掉了 `/users/me` 断言；TestAPITokenAuth 把严格 404 改成"只要不是 401 就行"。
**隐藏的真 bug**: JWT Claims 用 int64 而 DB 用 UUID string；限流器 key 包含端口导致永远不触发；CI 端点收到无效 UUID 格式。
**修复代价**: 后来做了一次完整的 auth 系统重构，从 int64 改到 string，涉及 8 个文件。

## 反模式 2：cargo test 通过就声称完成

**做了什么**: 修复完 P0/P1 后说"验证通过"，实际只跑了 `cargo check` 和 `cargo test`。
**暴露方式**: 用户跑 `cargo tauri dev`，立刻遇到 `beforeDevCommand` 路径报错。
**根因**: 单元测试验证各 crate 内部逻辑，不验证前端能否启动、Tauri 命令能否调用、IPC 是否通了。

## 反模式 3：用 mock 测试代替真实连接

**做了什么**: 限流器单元测试用 `httptest.NewRequest` + 手工构造的固定 `RemoteAddr`。
**隐藏的真 bug**: 真实 HTTP 连接的 `RemoteAddr` 包含源端口（`127.0.0.1:54321`），每个请求看起来都是不同的客户端。110 个请求从未触发限流。
**教训**: 涉及网络层行为的测试必须用真实连接。

## 反模式 4：策略文档当成交付物

**做了什么**: 被问"端到端测试完成了吗"，回答"只写了策略文档，一行测试代码都没写"。
**教训**: 策略、计划、设计文档不是完成。代码运行并通过验证才是完成。

## 反模式 5：共享状态污染测试

**做了什么**: Vue composable 测试没有在 `beforeEach()` 中重置状态。
**暴露方式**: 测试互相污染 node/edge 状态，导致间歇性失败。
**修复**: `beforeEach()` 调 `clearAll()`；组件测试用 `vi.mock()` 隔离。
