# Codex 适配层

Codex 的机制限制（实测确认）：
- **不消费 SKILL.md**（不读 `~/.codex/skills/`，与 pi/claude/cursor 不同）
- **无 input/hook 拦截机制**（不能像 pi extension 那样 transform 输入）
- **唯一指令入口是 AGENTS.md**（`~/.codex/AGENTS.md`，与 pi/家目录硬链接统一）

因此 Codex 的 skill 路由**只能靠 AGENTS.md 文字**——这是所有 agent 里最弱的强制（纯 prompt 纪律，无运行时拦截）。

## 部署方式

把下面"AGENTS.md 注入片段"的内容追加到 `~/.codex/AGENTS.md`（实际是硬链接的 canonical `~/.agents/AGENTS.md`）。

> ⚠️ 由于 AGENTS.md 是 4 处硬链接统一的（pi/codex/家目录/`.agents`），追加这段会让 pi 和家目录的 AGENTS.md 也获得该路由表。这对 pi 无害（pi 有更强的 extension 路由，AGENTS.md 是文字层），对 Codex 是唯一生效路径。

## AGENTS.md 注入片段

```markdown
# Skill 强制路由规则

收到用户消息时，先扫描意图是否命中下表关键词。**命中即 read 对应 SKILL.md 全文，严格按其流程处理**，不要自己瞎搞。

路由表单一真相源：`E:\Forge\skills\skill-routing\routes.json`（修改后重启 Codex 生效）。

| 关键词 | skill | SKILL.md 路径 |
|---|---|---|
| 日程/会议室/忙闲 | lark-calendar | E:\Forge\skills\lark-calendar\SKILL.md |
| 历史会议/纪要/逐字稿 | lark-vc | E:\Forge\skills\lark-vc\SKILL.md |
| 入会/离会/会中实时 | lark-vc-agent | E:\Forge\skills\lark-vc-agent\SKILL.md |
| 发消息/群聊/搜索聊天 | lark-im | E:\Forge\skills\lark-im\SKILL.md |
| 读/创建/编辑文档 | lark-doc | E:\Forge\skills\lark-doc\SKILL.md |
| 上传/下载文件/云盘 | lark-drive | E:\Forge\skills\lark-drive\SKILL.md |
| 多维表格/bitable | lark-base | E:\Forge\skills\lark-base\SKILL.md |
| 电子表格/sheets | lark-sheets | E:\Forge\skills\lark-sheets\SKILL.md |
| 邮件/收件箱 | lark-mail | E:\Forge\skills\lark-mail\SKILL.md |
| 通讯录/找人 | lark-contact | E:\Forge\skills\lark-contact\SKILL.md |
| 妙记/转写 | lark-minutes | E:\Forge\skills\lark-minutes\SKILL.md |
| 考勤/打卡 | lark-attendance | E:\Forge\skills\lark-attendance\SKILL.md |
| 审批 | lark-approval | E:\Forge\skills\lark-approval\SKILL.md |
| OKR/关键结果 | lark-okr | E:\Forge\skills\lark-okr\SKILL.md |
| 飞书任务/待办 | lark-task | E:\Forge\skills\lark-task\SKILL.md |
| 画板/白板 | lark-whiteboard | E:\Forge\skills\lark-whiteboard\SKILL.md |
| 幻灯片/PPT | lark-slides | E:\Forge\skills\lark-slides\SKILL.md |
| 会话变笨/分析会话 | claude-session-diagnostics | E:\Forge\skills\claude-session-diagnostics\SKILL.md |
| 提交代码/code review | implementation-discipline | E:\Forge\skills\implementation-discipline\SKILL.md |

命中路由表 > skill description 语义匹配。先查此表，未命中再按通用方式响应。
```

## 为什么 Codex 是最弱的

| Agent | 强制机制 | 强度 |
|---|---|---|
| pi | extension input transform（改写输入） | 硬强制 |
| Claude | UserPromptSubmit additionalContext 注入 | 中（针对性命中注入） |
| Cursor | alwaysApply mdc 规则 | 软（纯 prompt） |
| **Codex** | **AGENTS.md 文字** | **软（纯 prompt，无命中针对性）** |

Codex 用户若发现仍瞎搞，只能靠 AGENTS.md 文字强度 + 模型自觉，无运行时兜底。这是机制上限。
