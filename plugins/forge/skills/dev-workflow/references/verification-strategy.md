# 验证策略

基于 Thariq 文章中 "Product Verification" 类型技能的理念：
> "验证技能对确保输出正确性极其有用。值得花一周专门打磨验证技能。"
> "考虑在每一步强制执行程序化断言。"

## 核心原则

1. **每个阶段转换都有验证门控** — 不是最后才验证，而是全程验证
2. **验证 = 运行命令 + 看到输出** — "应该没问题"不算验证
3. **测试失败 = 停下来修** — 不绕过、不删除测试、不跳到下一步

## 验证层次

### 第 1 层：单元测试（每个任务后）

最基础的验证。每完成一个任务，运行项目的测试命令。

**通过标准**：所有测试通过，无新增失败。

### 第 2 层：静态分析（每个任务后）

Linter、type checker、formatter 检查。

**通过标准**：无 error 级发现。Warning 可以记录但不阻塞。

### 第 3 层：集成测试（每个阶段后）

跨模块、跨层的测试。可能需要启动服务或使用测试环境。

**通过标准**：端到端路径正常工作。

### 第 4 层：构建验证（交付前）

完整构建整个项目。

**通过标准**：构建成功，产物可运行。

## 多语言项目的验证命令

### 自动检测逻辑

```
if Cargo.toml 存在:
  test_cmd = "cargo test --workspace"
  check_cmd = "cargo clippy --workspace -- -D warnings"
  build_cmd = "cargo build --workspace"

if package.json 存在:
  pkg_mgr = pnpm-lock.yaml 存在 ? "pnpm" : "npm"
  test_cmd = "{pkg_mgr} test" (如果 scripts.test 存在)
  check_cmd = "{pkg_mgr} run lint" (如果 scripts.lint 存在)
  build_cmd = "{pkg_mgr} run build" (如果 scripts.build 存在)

if go.mod 存在:
  test_cmd = "go test ./..."
  check_cmd = "golangci-lint run" (如果已安装)
  build_cmd = "go build ./..."

if pyproject.toml 或 requirements.txt 存在:
  test_cmd = "pytest" (如果已安装)
  check_cmd = "ruff check ." 或 "flake8"
```

### 混合项目

同时存在多种检测文件时，按层分别验证：

```
验证顺序：
1. 后端（Rust/Go/Python/Java）
2. 前端（Node/Vue/React）
3. 跨层（集成测试、E2E 测试）
```

## 测试失败处理流程

```
测试失败
  → 阅读失败输出
    → 定位失败原因
      → 是新代码引入的？
        → 是：修复代码，重新测试
        → 否：是已有 flaky test？
          → 是：记录，但不阻塞（需告知用户）
          → 否：调查根因
```

## 验证反模式（不要这样做）

| 反模式 | 为什么有害 |
|-------|----------|
| "测试太慢了，先跳过" | 部署后才发现问题代价更高 |
| "这个改动太小了，不会出错" | 一行改动导致生产故障的例子数不胜数 |
| "测试失败了但跟我的改动无关" | 可能就是你的改动在远处引发了问题 |
| "我手动验证过了" | 手动验证不可重复，下次改动时无法回归 |
| "编译通过了就行" | 编译通过 ≠ 逻辑正确 |
