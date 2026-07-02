# 集成测试架构实例

32 个集成测试，使用 testcontainers-go + Rust FFI DLL + Go server 子进程。

## 目录结构

```
tests/
├── integration_test.go    # TestMain + 所有集成测试
├── testhelpers.go         # 辅助函数（启动 server、健康检查等）
└── fixtures/              # 测试数据
```

## TestMain 生命周期

```go
func TestMain(m *testing.M) {
    // 1. 启动 PostgreSQL 容器
    ctx := context.Background()
    postgresContainer, _ := testcontainers.GenericContainer(ctx,
        testcontainers.GenericContainerRequest{
            ContainerRequest: testcontainers.ContainerRequest{
                Image:        "postgres:16",
                ExposedPorts: []string{"5432/tcp"},
                Env:          map[string]string{"POSTGRES_DB": "test"},
            },
            Started: true,
        })
    defer postgresContainer.Terminate(ctx)

    // 2. 编译 Rust DLL（或使用预编译）
    // buildRustDLL()

    // 3. 启动 Go server 子进程
    dsn := postgresContainer.DSN()
    cmd := exec.Command("./server", "--db", dsn, "--rate-limit", "10000")
    cmd.Start()
    defer cmd.Process.Kill()

    // 4. 等待就绪
    waitForReady("http://localhost:8080/health")

    // 5. 运行测试
    code := m.Run()
    os.Exit(code)
}
```

## 中间件配置模式

```go
type ServerConfig struct {
    ListenAddr     string
    DatabaseDSN    string
    RateLimitPerMin int
    JWTSecret      string
}

func NewServer(cfg ServerConfig) *Server {
    router := chi.NewRouter()

    // 条件中间件
    if cfg.RateLimitPerMin > 0 {
        router.Use(RateLimitMiddleware(cfg.RateLimitPerMin))
    }

    // 生产默认值 vs 测试值
    // 生产: cfg.RateLimitPerMin = 100
    // 测试: cfg.RateLimitPerMin = 10000
}
```

## 遇过的坑

### 坑 1: 限流器 key 包含端口
- 症状: 110 个请求从不触发限流
- 原因: `r.RemoteAddr` = "127.0.0.1:54321"，端口每次不同
- 修复: `net.SplitHostPort(r.RemoteAddr)` 取 IP

### 坑 2: 测试共享限流预算
- 症状: 前 20 个测试通过，后面的全部 429
- 原因: 修好 key bug 后，32 个测试共享 100/min 限额
- 修复: 测试环境 `RateLimitPerMin: 10000`

### 坑 3: JWT 类型跨层不一致
- 症状: 登录成功但 GET /users/me 返回 404
- 原因: DB 存 UUID string → Go Claims.UserID 用 int64 → strconv.ParseInt(UUID) 返回 0 → 查询 "0" 无结果
- 修复: Claims.UserID 从 int64 改 string，8 个文件

### 坑 4: DLL 路径在 CI 中找不到
- 症状: 本地通过 CI 失败
- 原因: 本地 DLL 在项目根目录，CI 在子目录
- 修复: 用 `runtime.GOOS` + `os.Executable()` 计算相对路径
