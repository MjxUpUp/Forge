# 周报模板

## 变量列表

**必填**：
- `period`：汇报周期（如 2026-W25 / 本周 / 6.10-6.16）
- `done`：本周完成（来自 git log / 任务系统 / 用户口述）
- `next`：下周计划

**选填**：
- `blockers`：阻塞/风险
- `metrics`：关键数据
- `learnings`：经验/踩坑

## 章节结构

```
# 周报 | {period}

## 本周完成
{done，按项目/模块分组，每条含产出物 + 验证状态}

## 下周计划
{next，按优先级}

## 阻塞与风险
{blockers 或 "无"}

## 关键数据
{metrics 或 省略}

## 经验沉淀
{learnings 或 省略}
```

## 风格示例

> # 周报 | 2026-W25
>
> ## 本周完成
> **MyApp 治理**
> - 完成 lark skill 隔离（27 个移到 .lark-skills）✅ 已验证 junction
> - 新建 systematic-debugging/tdd-cycle skill ✅ registry 通过
>
> ## 下周计划
> - [P0] git init MyApp 版本管理
> - 补 doc-generator 模板库

简洁要点、产出物带验证状态、计划可执行。
