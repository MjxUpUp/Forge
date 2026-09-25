# 各 host 转录格式速查（transcript-forensics 参考）

按 host 列出转录文件位置与事件结构。嗅探优先：host 版本迭代快，本表过时以 `head -1` 实测为准。

## Claude Code

- 位置：`~/.claude/projects/<项目路径转义>/*.jsonl`（项目路径中 `:\`/`\` 转 `-`，如 `E--Forge`）
- 结构：每行一条事件；用户消息 `type:"user"` + `message.content`（字符串或 content blocks 数组）
- 过滤要点：
  - user 槽位混有 tool_result（`content[].type == "tool_result"`）与 hook 注入（文本以 `<` 开头的系统标签 /含 `tool_use_id`）——计数前必须剔除
  - 续传摘要（compact 后的第一条 user）是机器生成，长度异常大
- agent 侧：`type:"assistant"` + `message.content[].type in {text, tool_use}`

## kimi-code

- 位置：`~/.kimi-code/sessions/wd_<工作区>_<hash>/session_<id>/agents/main/wire.jsonl`
- 事件类型（`type` 字段）：
  - `turn.prompt`：**用户真实输入**（`input[].text`）——唯一可靠的用户消息来源
  - `context.append_message`：上下文注入（hook_result / system-reminder 混在 `message.role=="user"` 里）——**不是**用户说的
  - `usage.record`：token 计量（token 燃烧症状的直接数据源）
  - `llm.request`：请求快照；`turn.ended`：轮次结束
- 注意：`message.role=="user"` 的消息多数是注入，按 role 统计必虚高

## pi agent

- 位置：`~/.pi/agent/sessions/<项目路径转义-->/<ISO时间>_<uuid>.jsonl`
- 结构：`type:"message"` + `message.role` / `message.content[].text`；会话头 `type:"session"` 含 cwd
- 用户消息提取：`role=="user"` 且剔除以系统标签开头/含注入标记的条目

## reasonix

- 位置：`%APPDATA%\reasonix\projects\<项目>/sessions/<branch_id>.{jsonl,events.jsonl}`（Git Bash：`/c/Users/<user>/AppData/Roaming/reasonix/...`）；归档在 `%APPDATA%\reasonix\archive\`
- 结构：events.jsonl 为 `{"schema_version","type":"replace","revision","messages":[{role,content}]}` 快照式（每行是全量替换而非增量）
- 过滤要点：
  - `*.conflicts.jsonl` / `*recovery*` 是冲突与恢复副本——**同会话事件会计双份**，按 branch_id 去重
  - 会话很短且大量 `执行`/`已授权` 类指令是正常形态（worker 用法）

## 通用技巧

- Windows 控制台中文乱码：`PYTHONIOENCODING=utf-8 python ... > out.txt` 后再读文件
- Python 打开路径用 `C:/...` 正斜杠形式；Git Bash 的 `/c/...` 传给 Windows Python 会 FileNotFoundError
- 大文件流式逐行 `json.loads`，不要 `json.load` 整读
- 时间线：优先用事件自带 timestamp；缺失时退化为行序
