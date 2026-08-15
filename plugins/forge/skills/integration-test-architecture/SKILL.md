---
name: integration-test-architecture
description: "集成测试架构模式。Use when: 设计集成测试套件时、测试环境需要配置中间件时、测试涉及数据库或 HTTP 连接时、编写 testcontainers 相关测试时、遇到测试间互相影响时。SKIP: 纯单元测试（用 tdd-cycle）、前端组件测试。"
metadata:
  pattern: reference
  domain: testing
---

# 集成测试架构模式

从单一真实项目（Go 后端 + testcontainers + Rust FFI）实践中提炼的集成测试踩坑记录——通用性有限，当经验参考用，不当教科书。语言无关规则在正文，Go/testcontainers 专属细节在 references。

## 核心规则（语言无关）

### 1. 全局中间件必须可配置

影响所有请求的中间件（限流、CORS、认证超时）必须通过配置注入，测试环境能调大阈值或整体禁用（0 = 禁用）。硬编码的全局限流 = 测试环境关不掉 = 集成测试互相干扰。Go 配置模式见 references「中间件配置模式」。

### 2. 限流器 key 只用 IP，不含端口

socket 的 RemoteAddr 含源端口（`127.0.0.1:54321`），每个 TCP 连接源端口不同 → 每个请求被当成不同客户端 → 限流永不命中。取 key 前剥离端口。完整案例：references「坑 1: 限流器 key 包含端口」。

### 3. 网络行为用真实连接测试

手工构造的假请求（如 Go 的 `httptest.NewRequest`）用固定 RemoteAddr，暴露不了 TCP 层 bug。需要真实连接的场景：

- 限流器（需要真实端口行为）
- 连接超时（需要真实网络延迟）
- 请求体大小限制（需要真实流式传输）

### 4. 共享服务器的状态隔离

测试套件共享一个 server 实例时：

| 隔离维度 | 策略 |
|---------|------|
| 数据库 | 每个测试用独立 schema 或事务回滚 |
| 认证 | 每个测试创建独立的测试用户 |
| 限流 | 测试环境调大限流阈值或禁用 |
| 缓存 | 每个测试前清空缓存或用独立 key 前缀 |

## 架构模式

**容器化依赖 + 子进程被测服务**：测试进程启动容器化依赖（数据库等）→ 启动被测服务子进程 → 健康检查等待就绪 → 跑测试 → 清理（停服务 + 停容器）。Go 完整实现（testcontainers-go + TestMain 生命周期）见 references。

## Gotchas

- **测试顺序依赖**: 共享服务器 + 共享数据库 = 测试执行顺序影响结果。每个测试应独立可运行。
- **32 个测试共享限流预算**: 限流 key 修好后所有测试都走同一 IP，立刻集体 429——测试环境限流阈值调大或禁用（规则 1）。
- **e2e/竞态测试改真实仓库文件会跨包污染**: 测试若直接改真实仓库文件（如 `.github/workflows/release.yml`）验证"破坏后被拦"，`go test ./... -race` 并发下其他包读到破坏态，间歇性失败。用 `t.TempDir()` + 自建最小 fixture 隔离，不碰真实仓库文件；用脚本重写已 `git add` 的文件后 index 残留旧版，必须重新 `git add`。

## 参考

- Go/testcontainers 专属细节与完整实例：[references/test-architecture-example.md](references/test-architecture-example.md)（TestMain 生命周期、中间件配置模式、子进程日志、DLL 路径、坑 1-5）
