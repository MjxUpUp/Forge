---
name: integration-test-architecture
description: "集成测试架构模式。Use when: 设计集成测试套件时、测试环境需要配置中间件时、测试涉及数据库或 HTTP 连接时、编写 testcontainers 相关测试时、遇到测试间互相影响时。SKIP: 纯单元测试（用 tdd-cycle）、前端组件测试。"
metadata:
  pattern: tool-wrapper
  domain: testing-architecture
---

# 集成测试架构模式

从多个实际项目中提炼的集成测试架构经验。

## 核心规则

### 1. 全局中间件必须可配置

影响所有请求的中间件（限流、CORS、认证超时）必须通过 Config 结构体配置。

**配置模式：**
```go
type ServerConfig struct {
    RateLimitPerMin int    // 0 = 禁用
    CORSOrigins     []string // 空 = 允许所有
}
```

**测试环境设置：**
```go
cfg := ServerConfig{
    RateLimitPerMin: 10000,  // 测试中调大或禁用
}
```

**应用中间件时条件判断：**
```go
if cfg.RateLimitPerMin > 0 {
    router.Use(RateLimitMiddleware(cfg.RateLimitPerMin))
}
```

### 2. 限流器 key 必须只用 IP

`r.RemoteAddr` 包含源端口（如 `127.0.0.1:54321`），每个 TCP 连接的源端口不同，导致每个请求看起来都是不同客户端。

**正确做法：**
```go
ip, _, err := net.SplitHostPort(r.RemoteAddr)
if err != nil {
    ip = r.RemoteAddr // fallback for Unix sockets
}
```

### 3. 用真实连接测试网络行为

`httptest.NewRequest` 使用手工构造的固定 `RemoteAddr`，无法暴露 TCP 层面的 bug。

**需要真实连接的场景：**
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

### 模式：testcontainers + 子进程

```
测试主进程
├── testcontainers-go 启动 PostgreSQL 容器
├── 编译 Rust DLL（或使用预编译版本）
├── 启动 Go server 子进程
├── 等待 server 就绪（健康检查）
├── 运行测试用例
└── 清理：停止 server + 停止容器
```

### 模式：TestMain 生命周期

```go
func TestMain(m *testing.M) {
    // 1. 启动依赖（数据库、缓存）
    // 2. 启动被测服务
    // 3. 运行测试
    code := m.Run()
    // 4. 清理
    os.Exit(code)
}
```

## Gotchas

- **32 个测试共享限流**: 限流修好后立刻把所有测试限流了——因为都走 `127.0.0.1`。解决方案：测试环境设置 `RateLimitPerMin: 10000`。
- **子进程日志丢失**: Go server 作为子进程运行时，stderr 可能被缓冲。确保 `cmd.Stderr = os.Stderr` 或用 `cmd.CombinedOutput()`。
- **DLL 路径**: Rust FFI DLL 的路径在不同 OS 上不同（`.dll` vs `.so` vs `.dylib`）。用 `runtime.GOOS` 选择后缀。
- **测试顺序依赖**: 共享服务器 + 共享数据库 = 测试执行顺序影响结果。每个测试应该独立可运行。
- **e2e/竞态测试改真实仓库文件会跨包污染**: 测试若直接改真实仓库文件（如 `.github/workflows/release.yml`）来验证"破坏后被拦"，`go test ./... -race` 并发下其他包会读到破坏态，产生间歇性失败。用临时 fixture 隔离——`t.TempDir()` + 自建最小项目结构（如 `setupGuardProject`），不碰真实仓库文件。同理：用 `Write` 重写一个已 `git add` 的文件后，index 残留旧版，必须重新 `git add` 否则提交的是竞态旧版。

## 参考

- 测试架构实例详解：[references/test-architecture-example.md](references/test-architecture-example.md)
