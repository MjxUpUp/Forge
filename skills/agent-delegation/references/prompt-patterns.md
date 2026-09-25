# 子代理 Prompt 模式

## 好的 Prompt 示例

### 修复类任务

> 修复 la-pipeline/src/engine.rs 第 142 行的编译错误。
>
> 错误信息：`mismatched types: expected PipelineResult<()>, found Result<(), CoreError>`
>
> 当前代码：

```rust
fn execute_stage(&self, stage: &Stage) -> Result<(), CoreError> {
    self.compensator.retry(stage, || stage.run())
}
```

> PipelineResult 定义在 la-core/src/types.rs 中，是 `Result<T, PipelineError>`。
> PipelineError 有一个 `from_core(err: CoreError)` 转换方法。
>
> 修复方式：用 `.map_err(PipelineError::from_core)` 包装返回值。
> 验证：`cargo check -p la-pipeline` 通过。

### 实现类任务

> 在 crates/la-tools/src/registry.rs 中实现 ToolRegistry::get_by_name 方法。
>
> 当前结构体定义（第 23-28 行）：

```rust
pub struct ToolRegistry {
    tools: HashMap<String, Box<dyn Tool>>,
}
```

> 需要实现：
> - `pub fn get_by_name(&self, name: &str) -> Option<&dyn Tool>` — 只读查找
> - 如果 tool 不存在返回 None
>
> 约束：
> - Tool trait 定义在 la-core/src/tool.rs
> - 不要修改 Tool trait
> - 保持现有的 register() 和 list() 方法不变
>
> 验证：`cargo test -p la-tools` 通过。

## 差的 Prompt 示例

### 偷懒委托

> 看看 la-tools 的代码，工具注册好像有问题，帮我修一下。

问题：没有文件路径、没有行号、没有具体描述什么问题、没有说明期望行为。

### 过于宽泛

> 重构整个 la-pipeline 模块，让代码更清晰。

问题：没有具体目标、没有约束、没有验证标准。

### 缺少验证标准

> 给项目的 rate limiter 加一个 IP 白名单功能。

问题：没有说明白名单格式、从哪读取配置、怎么验证功能正确。
