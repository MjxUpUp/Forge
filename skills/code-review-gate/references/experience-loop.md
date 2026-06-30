# 经验闭环（experience → knowledge → review）

`code-review-gate` 步骤 2 前置依赖的 experience → knowledge → review 反馈闭环。这是 loop engineering 的**学习层**：让审查越用越准——每次低分 task 的教训沉淀成知识，下次审查自动消费。

## 闭环数据流

```
低分 task (score < 70)
    │  forge task complete 自动触发（评分时建 review + 生成 proposal）
    ▼
经验提案  .forge/experience/proposed/{id}.json   [status: proposed]
    │  forge experience accept <id>
    ▼
跨项目知识库  ~/.forge/knowledge/index.json + gotchas/{id}.md   [Entry]
    │  code-review-gate 步骤 2 前置消费
    ▼
forge knowledge check    机器扫 patterns  →  命中即 block
forge knowledge list     人审语义核对     →  补充检查表
    │
    ▼
下次审查更准（积累的 gotchas 自动注入）
```

## 各环节命令

| 环节 | 命令 | 存储 |
|---|---|---|
| 提案（低分自动生成） | `forge task complete` 评分<70 自动生成 / `forge experience generate <task-ref>` 回填历史 review | `.forge/experience/proposed/` |
| 审阅提案 | `forge experience list` / `forge experience show <id>` | — |
| 接纳（→ knowledge） | `forge experience accept <id>` | 写入 `~/.forge/knowledge/` |
| 消费（审查时） | `forge knowledge check` + `forge knowledge list --category gotchas` | 读 `~/.forge/knowledge/` |

## 为什么分两层消费（机器扫 + 人审）

经验条目分两种，消费方式不同，互补覆盖：

- **有 patterns 的 gotcha**（如 `@ts-ignore` / 空 catch / `t.Fatal→t.Log`）→ regex 可判，`forge knowledge check` 机器扫全项目，命中直接 `block`。**高精度、低召回**（只抓有明确指纹的）。
- **无 patterns 的 gotcha**（如"跨层数据类型必须一致""错误路径必须有测试""依赖必须真实存在"）→ 无法 regex，靠 `forge knowledge list` 输出后由 agent 语义核对 diff。**低精度、高召回**（覆盖语义级的）。

两层叠加：机器扫兜住有指纹的硬错，人审兜住语义级的软错。只靠一层都会有漏。

## MCP 接口（agent 可编程调用）

装了 `forge mcp serve` 的 agent（Claude Code / Codex / Copilot）可用 MCP 工具结构化消费，不必 parse CLI 文本：

- `forge_experience_propose` —— 提案新经验（写端）
- `forge_experience_search` —— 搜索提案
- `forge_knowledge_lookup` —— 查询知识库（消费端，等价 `forge knowledge list` + 关键词过滤）

机器扫描（`forge knowledge check`）暂无 MCP 工具——它需要项目根 + 全项目文件扫描，更适合 CLI/hook 触发，不在 MCP 单次调用模型内。

## 反模式

- ❌ **只写不消费**：proposed 堆积从不 accept → knowledge 空 → 审查永远只用静态 11 类，闭环断在写端。低分 task 后及时 `forge experience accept`。
- ❌ **只机器扫不人审**：跳过 `forge knowledge list` → 漏掉所有无 patterns 的语义级 gotcha（这些往往是危害最大的设计类问题）。
- ❌ **accept 不审就接纳**：把噪音写进 knowledge 污染后续所有审查。accept 前确认 title/description/patterns 准确。
