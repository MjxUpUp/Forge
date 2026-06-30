---
name: on-demand-guards
description: "按需激活的临时安全护栏（session 级），补充 always-on 的 hazard-guard 自动挡。Use when: 用户说\"小心点\"\"/careful\"\"别误删\"\"我要动生产环境\"\"锁住这个目录\"\"/freeze\"\"只改这里别动其他\"\"高危操作\"时、即将执行 chmod -R 777 / curl|sh / 写裸设备等 hazard-guard 未覆盖的危险操作时。激活后持续到 session 结束或用户说\"解除\"。SKIP: 日常低风险开发（不需要护栏）、已经在 git protected 分支（git 本身会拦）。注：rm -rf / DROP TABLE / force-push / kubectl delete / TRUNCATE 等已由 hazard-guard hook 自动拦截（HITL via forge hazard confirm），无需本 skill。"
metadata:
  pattern: gate
  domain: safety
---

# On-Demand Guards — 按需临时安全护栏

本 skill 分两层，与 hazard-guard 自动挡互补：

| 层 | 覆盖 | 形态 |
|---|---|---|
| **always-on（自动挡）** | `rm -rf` / `git push --force` / `git reset --hard` / `DROP DATABASE\|TABLE\|SCHEMA` / `TRUNCATE` / `GRANT ALL` / `kubectl delete` / `docker system prune` / `shred` / 无 WHERE 的 `DELETE\|UPDATE` | hazard-guard hook（PreToolUse Bash），自动 block + HITL（`forge hazard confirm` 登记 5min 标记后放行）|
| **session 级（本 skill）** | hazard-guard 未覆盖的（`chmod -R 777` / `curl … \| sh` / `> /dev/sda` 等危险模式）+ 目录锁定（/freeze） | 激活后 agent 自我约束，每次匹配操作前 STOP 确认 |

**核心原则：激活后，每次匹配危险模式的操作前必须 STOP 确认，直到用户说"解除"。**

pi 没有运行时动态注册 hook 的能力，session 级部分用 **skill 正文纪律**模拟——激活后 agent 自我约束，每次危险操作前自检。

## When to Use

用户明示要"小心"，或即将执行 hazard-guard 未覆盖的危险操作时激活：

| Guard | 触发信号 | 激活后效果 |
|---|---|---|
| **/careful** | "小心点""别误删""动生产环境""/careful" | 阻止 hazard-guard 之外的危险模式（chmod -R 777 / curl\|sh / 写裸设备等），每次 STOP 确认 |
| **/freeze** | "锁住这个目录""只改这里""/freeze <dir>" | 阻止对指定目录外的 Edit/Write，每次 STOP 确认 |

> `rm -rf` / `DROP TABLE` / `git push --force` / `kubectl delete` / `truncate` 等已由 **hazard-guard hook 自动拦截**（不需要本 skill 激活）——见下文「always-on：hazard-guard」。

## always-on：hazard-guard 自动挡

下列高危命令由 hazard-guard hook（PreToolUse Bash，所有 agent 自动接线）**始终拦截**，无需激活本 skill：

- 递归强删 / 不可逆破坏：`rm -rf` / `shred` / `mkfs`
- git 危险操作：`git push --force` / `git push --delete` / `git reset --hard`
- SQL 破坏性 DDL / 权限：`DROP DATABASE|TABLE|SCHEMA` / `TRUNCATE` / `GRANT ALL` / `GRANT … TO PUBLIC` / 无 WHERE 的 `DELETE|UPDATE`
- 基础设施破坏：`kubectl delete` / `docker system prune` / `docker volume rm` / `docker rm -f`

**拦截后（HITL 闭环，不是硬 block）**：hook 给出指纹和指引 → agent 用所在 AI 工具的提问确认工具（Claude Code→AskUserQuestion；codex/cursor/windsurf→各自机制）向用户说明风险获明确确认 → 运行 `forge hazard confirm "<命令>"` 登记 5min 限时标记 → 重试原命令自动放行。测试/CI 可设 `FORGE_ALLOW_HAZARD=1` 跳过。

## /careful — hazard-guard 之外的补充护栏

**激活条件**：用户说"小心""/careful""别误删""动生产环境"等。

**激活后，每次执行以下模式命令前 STOP 确认**（hazard-guard 已拦的不重复，这里只列未覆盖的）：

```bash
chmod -R 777    # 危险权限
curl ... | sh   # 执行远程脚本
> /dev/sda      # 写裸设备
```

**STOP 确认格式**：
```
⚠️ /careful 已激活，检测到高危命令：
  [命令]
确认执行？说"确认"继续，或其他取消。
```

**解除**：用户说"解除 careful""不用小心了""恢复正常"。

## /freeze — 目录锁定

**激活条件**：用户说"锁住 X 目录""只改这里""/freeze src/"。

**激活后，每次 Edit/Write 到锁定目录外的文件前 STOP 确认**：

```
⚠️ /freeze 已激活（锁定目录：src/），检测到目录外修改：
  [文件路径]
确认修改？说"确认"继续，或其他取消。
```

**典型场景**：调试时"我只加日志，别让我不小心改了无关代码"——freeze 后只允许改指定目录。

**解除**：用户说"解除 freeze""解锁""可以改其他了"。

## 激活状态记忆

激活后，agent 在**每个回合开始**自检当前激活状态（/careful？/freeze 哪个目录？），不需要用户重复声明。状态持续到：
- 用户明示"解除"
- session 结束

**状态记录**：激活时把状态记到 session 上下文（如"当前激活：/careful + /freeze src/"），后续回合读这个状态。

## Gotchas（高信号）

- **激活后不能"忘记"**：用户说了 /careful，后续整个 session 都生效，不是只管下一次。每个回合开始自检状态。
- **不与 hazard-guard 重复**：`rm -rf`/`DROP`/`force-push` 等已被 hazard-guard 自动拦，本 skill 不重复 STOP——只覆盖 hazard-guard 模式之外的（chmod -R 777 / curl|sh / 写裸设备）。
- **STOP 不是拒绝**：STOP 是让用户确认，不是拒绝执行。用户说"确认"就继续——护栏的目的是防误操作不是禁止操作。
- **不要过度拦截**：只拦高危模式，普通 ls/cat/grep 不拦。过度拦截会让用户烦。
- **pi 无动态 hook 注册**：session 级部分靠 agent 自我约束模拟，不是真正的 PreToolUse hook（hazard-guard 则是真 hook，全 agent 生效）。

## Red Flags — STOP

- 激活了 /careful 却直接执行 chmod -R 777 不确认（忘记激活状态）
- /freeze 后改了锁定目录外文件不确认
- 用户说"解除"后还在拦截（状态没更新）
- 拦截低风险命令（ls/grep/cat 等只读操作不该拦）

## 与其他 skill 的分工

- **hazard-guard hook（always-on 自动挡）**：高危命令（rm -rf / DROP / force-push / kubectl delete 等）的硬拦截 + HITL。本 skill 是 hazard-guard 之外的补充。
- **delivery-gate**（pi extension）：全局的资产交付门控（写 skill/hook 后验证）。本 skill 是 session 级按需安全护栏。
- **code-review-gate**：代码质量审查。本 skill 是操作安全拦截（防误删/误改）。
- **systematic-debugging**：调试方法论。调试时配合 /freeze 防止"顺手改了无关代码"。
