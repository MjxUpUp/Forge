---
name: session-search
description: "检索 pi 历史会话（之前怎么解决过/上次调研过什么/记得处理过类似问题）。Use when: 用户说\"之前怎么做的\"\"上次那个问题\"\"我记得处理过\"\"之前调研过XX\"\"看看历史会话/对话记录\"时、想复用过往解决方案避免重复劳动时、想找之前的项目上下文时。SKIP: 实时网络搜索（fact-research）、代码库内搜索（直接 rg）、API/文档查询（dev-lookup）。"
metadata:
  pattern: tool-wrapper
  domain: session-history
---

# Session Search — 历史会话检索

检索 pi 的历史会话（`~/.pi/agent/sessions/*.jsonl`），找回"之前怎么解决的""上次调研过什么""记得处理过类似问题"。

**痛点**：pi 会话是结构化 jsonl（type/role/content 嵌套），裸 grep 噪音大——单行常 >10KB，system prompt 混入，跨项目结果混杂，无时间排序。本 skill 封装结构化检索脚本，解决这些痛点。

## When to Use

- 用户说"之前怎么做的""上次那个问题""记得处理过""之前调研过"
- 想复用过往的解决方案、调试经验、调研结果
- 想找某个项目的历史上下文
- 想知道"这个坑以前踩过吗"

## SKIP（转其他 skill）

- 实时网络搜索（数据/对比/进展）→ **fact-research**
- 代码库内搜索（函数/类/定义在哪）→ 直接 `rg`，不用 skill
- API 签名/报错/库用法 → **dev-lookup**
- 当前会话内已有上下文 → 不用本 skill

## 会话结构（脚本封装的细节，理解检索逻辑用）

pi 会话是 jsonl，每行一条记录：

```jsonl
{"type":"session","cwd":"E:\\DevWorkbench","timestamp":"...","id":"..."}
{"type":"message","role":"user","content":[{"type":"text","text":"..."}]}
{"type":"message","role":"assistant","content":[{"type":"text","text":"..."},{"type":"thinking","thinking":"..."},{"type":"toolCall",...}]}
{"type":"message","role":"toolResult","content":[{"type":"text","text":"工具返回内容"}]}
```

- **cwd 双重存储**：session 行的 `cwd` 字段 + 文件所在目录名（`--E--DevWorkbench--`）
- **role 三种**：`user`（用户输入）、`assistant`（助手回复+思考+工具调用）、`toolResult`（工具返回）
- **content 多类**：`text`（文本）、`thinking`（推理）、`toolCall`（工具调用）

**关键**：工具返回（toolResult）常含真实价值（命令输出、文件内容、调研素材），别只搜 user/assistant。

## 核心脚本

`scripts/session-search.sh` —— 封装 jq 结构化提取 + rg 高速预筛。

**脚本路径**：本 skill 目录下的 `scripts/session-search.sh`（如读 SKILL.md 在 `X/session-search/SKILL.md`，脚本在 `X/session-search/scripts/session-search.sh`）。

### 用法

```bash
# 基础搜索（跨所有项目，返回命中的消息片段）
bash scripts/session-search.sh "<关键词>"

# 按项目过滤（支持模糊匹配）
bash scripts/session-search.sh "<关键词>" -c DevWorkbench

# 按角色过滤
bash scripts/session-search.sh "<关键词>" -r user        # 只看用户问过什么
bash scripts/session-search.sh "<关键词>" -r assistant    # 只看助手回答
bash scripts/session-search.sh "<关键词>" -r toolResult   # 只看工具返回（常含命令输出/文件内容）

# 列出所有项目分布（先用这个发现有哪些项目可检索）
bash scripts/session-search.sh --list-cwd

# 只看命中的会话清单（不输出消息内容，快速定位）
bash scripts/session-search.sh "<关键词>" --sessions-only

# 输出完整消息内容（默认截断到 300 字符）
bash scripts/session-search.sh "<关键词>" --full

# 限制返回数量
bash scripts/session-search.sh "<关键词>" -l 5
```

### 检索策略（按意图选参数）

| 意图 | 推荐命令 |
|---|---|
| "之前怎么解决 X 问题" | `-r assistant` 找助手回答，`-r toolResult` 找命令/代码 |
| "上次调研过 X" | `-r assistant` + `-r toolResult`（调研素材在工具返回里） |
| "X 项目的历史" | `-c <项目名>` 过滤项目 |
| "我记得提过 X" | `-r user` 找用户原话 |
| "踩过 X 坑吗" | 不加 `-r`，全角色搜 |
| 发现有哪些项目 | `--list-cwd` |

## 检索降级链（找不到时换策略）

```
精确关键词（"报错 E0432"）
  ├─ 命中 → 用结果
  ├─ 无命中 → 换 -r 角色（assistant 找不到换 toolResult，反之亦然）
  ├─ 仍无 → 换近义词/技术术语（"Tauri 窗口白屏" → "webview blank"）
  ├─ 仍无 → 去掉 -c 项目过滤（可能记错了项目）
  └─ 仍无 → --list-cwd 确认项目名，或诚实说"历史会话未找到相关记录"
```

## 输出解读

脚本输出每条命中的：会话文件名、消息时间、所属项目（cwd）、角色、文本片段。定位到有价值的会话后，可用完整文件名 `read` 整个 jsonl 看完整上下文。

## 上限纪律

- **这是轻量检索，不是深度分析**：≤5 次调用定位到会话即可，拿到就停
- **不重写历史**：检索是为复用上下文，不是修改历史会话
- **引用真实片段**：复用历史结论时标注来自哪次会话、什么时间
- **诚实于"未找到"**：历史会话没记录就说没有，不脑补

## Gotchas（高信号）

- **工具返回（toolResult）是最有价值的源**：命令输出、文件内容、调研素材都在这里，不只搜 user/assistant。脚本默认搜全角色正是为此
- **cwd 模糊匹配**：`-c DevWorkbench` 会匹配 `E:\DevWorkbench` 和 `E:\DevWorkbench\src-tauri`，传子目录名或父目录名都行
- **thinking 不默认搜**：脚本只搜 `text` 类型 content。`thinking`（推理过程）和 `toolCall`（工具调用参数）需读完整 jsonl 查看
- **单条消息可能很长**：`--full` 输出整条，默认截断 300 字符。定位到关键会话后用 `read` 看全貌
- **关键词用业务/技术词**：搜"报错 E0432""Tauri 窗口白屏""调研 code-review"比搜"那个错误""之前的问题"有效
- **跨项目问题去掉 -c**：通用问题（rust 语法、git 操作）可能记错项目归属，去掉 `-c` 全局搜
- **正则支持**：query 支持 rg 正则，如 `"错误|报错|失败"` 多词匹配
- **会话可能被压缩**：长会话 jsonl 的早期消息可能已被摘要，搜索不到早期细节属正常

## 与其他 skill 的分工

- **fact-research**：实时网络搜索外部事实。本 skill 搜本地历史
- **dev-lookup**：查 API/库的官方文档。本 skill 查"之前我怎么用过这个 API"
- **session-retrospective**：从历史会话提取教训写入记忆文件。本 skill 提供检索能力，session-retrospective 提供提炼与载体路由能力

## 参考

- 检索脚本：[scripts/session-search.sh](scripts/session-search.sh)
