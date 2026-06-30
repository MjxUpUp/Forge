---
name: claude-session-diagnostics
description: "Claude / Claude Code 会话劣化与失败的事后取证归因。Use when: 用户给会话 id 问'为什么变笨/越来越笨/变傻/变差/越来越慢'、'分析下这个会话'、'这个会话为什么失败了/卡住了/跑偏了/废了'、'Claude 越用越蠢'、会话越用越差需要找根因时。SKIP: 检索历史会话做了什么（用 session-search）、跨会话恢复上下文继续干活（用 session-continuity）、单个运行时代码 bug 排查（用 systematic-debugging）。"
metadata:
  pattern: tool-wrapper
  domain: session-forensics
---

# Claude 会话劣化诊断

会话"变笨"不是玄学，是**可量化的工程问题**：自动压缩次数 × 噪声注入速率 × 任务主线漂移，三者叠加决定上下文还能不能撑住。本 skill 给出从 jsonl transcript 取证归因的标准方法 + 可复用脚本。

## When to Use / When NOT to Use

**Use**：
- 用户给会话 id + "为什么变笨/失败/卡住/跑偏/废了"
- "Claude 越用越蠢""会话越来越慢"
- 排查 agent 后期开始忘事、反复试探、被打脸后还重复旧假设
- 评估某个长会话是否还有救 / 该不该开新的

**SKIP**：
- 想知道历史会话"做过什么" → **session-search**
- 要从上次中断处继续干活 → **session-continuity**
- 单个代码/运行时 bug → **systematic-debugging**
- 还没产生 jsonl 的实时调试（本 skill 是事后取证）

## 诊断流程

### 第 1 步：跑脚本拿量化数据（必做，不要凭印象）

```bash
python <skill_dir>/scripts/session-diagnostics.py <session-id> --out report.md
```

脚本自动从 `~/.claude/projects/*/<id>.jsonl` 定位会话，输出 5 个维度的量化报告 + 红线汇总。`<skill_dir>` = 本 SKILL.md 所在目录。

脚本只解决**可量化的 4 个维度**（压缩、噪声、工具循环、规模）。第 5 维度（主题漂移）脚本会列出全部人类输入，由你判断主线数。

### 第 2 步：读红线，定根因

对照下方"劣化信号解读"，把命中的红线翻译成具体根因。**根因永远比红线本身重要**——红线是症状，根因是机制。

### 第 3 步：读压缩 summary 看信息损失程度

挑首/尾两条 `This session is being continued…` 消息（脚本时间序列的第 1 条和最后 1 条）对比：
- 早期 summary 是否含精确技术细节（行号、枚举变体、verbatim 约束）
- 末段 summary 是否只剩缩略英文 bullet、丢了 verbatim 原文

差距越大，有损重写累积越严重。这是"变笨"最直接的机制证据。

### 第 4 步：定性补刀（看末段行为退化）

即使量化红线没全中，读末段（最后 300-500 行）的工具调用序列，找：
- 连续 ≥4 次同工具的密集小步探查（Read/Read/Read/Read）= 丢主线后凑上下文
- 用户当场打脸后，agent 又把已被否定的假设当新结论 = 上下文里事实与已纠正内容混在一起

## 劣化信号解读（根因翻译表）

| 红线 | 根本机制 | 不是什么 |
|---|---|---|
| 压缩次数 ≥10 | 有损重写累积：每次都是"把之前全对话再总结一遍"，技术细节（精确行号、枚举名、verbatim 约束）逐次丢失 | 不是"会话长"，是"重写次数多" |
| 最短压缩间隔 <15min | 噪声撑爆上下文，膨胀失控：一两个回合就再次压缩 | 不是用户话多，是注入多 |
| 零信息回执占比 ≥50% | hook_success / task_reminder / goal 每轮全量重发，挤占推理预算，加速填满→触发下一次压缩 | 不是正常工具流 |
| 工具循环 run ≥20 | 丢主线后靠密集小步探查凑上下文（探查退化） | 不是勤奋，是迷茫 |
| 人类输入跨 ≥4 主线 | 多个不相关任务挤一个 session，压缩时被迫决定保留谁丢谁，加剧损失 | 不是"任务多"，是"该开新会话没开" |

## 避免重演（从根因反推改进）

诊断完不是终点，要把根因翻译成可执行改进给用户：

- **压缩次数高** → 一个主线一个会话；完成即 `/clear` 或新开
- **压缩到 3-5 次就该人工收尾**：把结论写进 memory/文档，新开会话用 session-continuity 接力。25 次压缩 = 早就不可靠
- **最短间隔短** → 给 hook 减噪：PostToolUse hook 改成只在**非 approve**时注入，截断 stdout
- **task_reminder/goal 高** → 目标只在偏离时才提醒，不要每轮全量重发
- **主题漂移** → "检查这个 session id 为啥失败""打个安装包"这类吃上下文的任务另起会话，别混进主线

## Common Rationalizations（纪律：别凭感觉诊断）

| 借口 | 现实 |
|---|---|
| "模型就是变笨了，没法分析" | 劣化可量化。跑脚本拿压缩×噪声×漂移数据，根因是工程问题不是玄学 |
| "凭印象说会话太长就行" | 不够。必须指出命中的具体红线 + 根因机制，否则改进无从落地 |
| "看最后几条消息就够了" | 末段是劣化的**结果**，根因在压缩分布和噪声注入，在中段 |
| "直接说'开新会话吧'" | 没根因的结论等于没诊断。先取证，再说怎么改 |
| "不用脚本，我手动翻 jsonl" | 21829 行人工翻不动。脚本 1 秒出量化报告，必跑 |

## Gotchas（jsonl 解析陷阱——最高信号）

脚本已处理这些，但理解原理避免自己手写时踩坑：

- **Python 不认 MSYS 路径**：`/c/Users/...` 在 Windows 原生 Python 下 FileNotFoundError。用 `C:/Users/...` 或 `os.path.expanduser`。脚本内部全用 expanduser + os.path.join，安全。
- **Windows 控制台 GBK 乱码**：中文输出到 stdout 会乱码。脚本用 `sys.stdout.reconfigure(encoding="utf-8")` 强制 UTF-8，且推荐 `--out` 写文件。
- **attachment 字段是字符串化的 Python dict，不是 JSON**：形如 `"{'type': 'hook_success', ...}"`（单引号）。不能 `json.loads`，要用正则 `'type':\s*'([^']+)'` 提取。少数情况下它真的是 dict（已处理 isinstance 分支）。
- **user message.content 可能是 str 或 list**：list 里混 text block 和 tool_result block。诊断人类输入时必须**跳过 tool_result**（那是工具回执不是人话）。
- **isSidechain 经常是字符串 "False"** 不是布尔。脚本用 `is_true()` 兼容两者。
- **isMeta 标记**：Meta 消息（如 command 回显）要过滤，否则污染人类输入统计。
- **jsonl 路径编码规则**：`~/.claude/projects/<encoded-cwd>/<session-id>.jsonl`，cwd 的 `\` `:` 被替换成 `-`（如 `E:\DevWorkbench` → `E--DevWorkbench`）。脚本用 glob 通配 `*` 绕过，不用手动拼。
- **compaction 识别**：用 `text.startswith("This session is being continued")`，不要找其他特征。

## 与其他 skill 的分工

- **session-search**：检索历史会话"**做了什么**"（语义查找）
- **session-continuity**：恢复上下文"**继续干活**"（预防侧：恢复）
- **systematic-debugging**：单个代码/运行时 bug 的根因排查
- **本 skill**：会话本身"**为什么劣化/失败**"的事后取证归因（诊断侧）

典型组合：本 skill 诊断出"会话过载"→ 用 session-continuity 在新会话恢复→ 避免重蹈覆辙。

## 实战锚点（校准红线）

红线阈值来自一次真实劣化会话标定（39 小时、25 次压缩、89% 噪声回执、122 次工具循环 run、6 个不相关主线）——那是极端值。日常会话命中 1-2 条红线就该警惕，命中 3 条以上基本不可逆。
