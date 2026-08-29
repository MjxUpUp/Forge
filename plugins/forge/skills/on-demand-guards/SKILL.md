---
name: on-demand-guards
description: "按需激活的临时安全护栏（session 级），补充宿主高危命令 hook 的 always-on 自动挡。Use when: 用户说\"小心点\"\"/careful\"\"别误删\"\"我要动生产环境\"\"锁住这个目录\"\"/freeze\"\"只改这里别动其他\"\"高危操作\"时、即将执行 chmod -R 777 / curl|sh / git clean -fd / npm publish 等自动挡未覆盖的危险操作时。激活后持续到 session 结束或用户说\"解除\"。SKIP: 日常低风险开发（不需要护栏）、已经在 git protected 分支（git 本身会拦）。注：rm -rf / DROP TABLE / force-push / kubectl delete / TRUNCATE 等已由宿主高危命令 hook 自动拦截（HITL 确认放行），无需本 skill。"
metadata:
  pattern: gate
  domain: security
---

# On-Demand Guards — 按需临时安全护栏

本 skill 分两层，与宿主的 always-on 自动挡互补：

| 层 | 覆盖 | 形态 |
|---|---|---|
| **always-on（自动挡）** | `rm -rf` / `git push --force` / `git reset --hard` / `DROP DATABASE\|TABLE\|SCHEMA` / `TRUNCATE` / `GRANT ALL` / `kubectl delete` / `docker system prune` / `shred` / 无 WHERE 的 `DELETE\|UPDATE` | 宿主高危命令 hook（PreToolUse Bash，如已接线）自动 block + HITL 确认登记后放行 |
| **session 级（本 skill）** | /freeze 目录锁定 + 自动挡未覆盖的危险模式（`chmod -R 777` / `curl … \| sh` / `git clean -fd` / `npm publish` 等） | 正文纪律——每次匹配操作前 STOP 确认；宿主有 freeze 类 hook 时升级为硬阻断 |

**核心原则：激活后，每次匹配危险模式的操作前必须 STOP 确认，直到用户说"解除"。**

## When to Use

用户明示要"小心"，或即将执行自动挡未覆盖的危险操作时激活：

| Guard | 触发信号 | 激活后效果 |
|---|---|---|
| **/careful** | "小心点""别误删""动生产环境""/careful" | 阻止自动挡之外的危险模式（chmod -R 777 / curl\|sh / git clean -fd 等），每次 STOP 确认 |
| **/freeze** | "锁住这个目录""只改这里""/freeze <dir>" | 阻止对指定目录外的 Edit/Write——宿主有 freeze 类 hook 时硬阻断，否则 STOP 确认 |

> `rm -rf` / `DROP TABLE` / `git push --force` / `kubectl delete` / `truncate` 等在宿主接线了高危命令 hook 时**自动拦截**（不需要本 skill 激活）——见下文「always-on 自动挡」。

## always-on 自动挡（宿主 hook 层，如已接线）

下列高危命令由宿主的高危命令 hook（PreToolUse Bash）**始终拦截**，无需激活本 skill：

- 递归强删 / 不可逆破坏：`rm -rf` / `shred` / `mkfs`
- git 危险操作：`git push --force` / `git push --delete` / `git reset --hard`
- SQL 破坏性 DDL / 权限：`DROP DATABASE|TABLE|SCHEMA` / `TRUNCATE` / `GRANT ALL` / `GRANT … TO PUBLIC` / 无 WHERE 的 `DELETE|UPDATE`
- 基础设施破坏：`kubectl delete` / `docker system prune` / `docker volume rm` / `docker rm -f`

**拦截后（HITL 闭环，不是硬 block）**：hook 给出指纹和指引 → 授权判定：**用户本回合已明确指令/确认过该操作时**（如用户直接要求执行，或 agent 前置已问过），无需二次确认，直接登记放行；否则先用所在 AI 工具的提问确认机制向用户说明风险、获明确确认再放行 → 重试原命令自动通过。confirm 链是唯一放行路径，测试/CI 同样走确认登记。

宿主未接线自动挡时，这些命令同样危险——按 /careful 的 STOP 确认纪律处理。

## /careful — 自动挡之外的补充护栏

**激活条件**：用户说"小心""/careful""别误删""动生产环境"等。

**激活后，每次执行以下模式命令前 STOP 确认**（自动挡已拦的不重复，这里只列未覆盖的）：

```bash
chmod -R 777      # 危险权限
curl ... | sh     # 执行远程脚本
> /dev/sda        # 写裸设备
git clean -fd     # 不可逆清未跟踪文件（含未提交的新文件，git 无法找回）
npm publish       # 不可逆公开发布（旧版不可撤回，撤包有窗口期限制）
ssh prod-host     # 直接操作生产机（命令在远程执行，本地 hook 管不到）
> existing-file   # 重定向覆盖已有文件（静默截断，区别于 >> 追加）
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

**STOP 纪律模拟**（宿主无 freeze 类 hook 时的主路径）：激活后每次 Edit/Write 到锁定目录外的文件前 STOP 确认：

```
⚠️ /freeze 已激活（锁定目录：src/），检测到目录外修改：
  [文件路径]
确认修改？说"确认"继续，或其他取消。
```

**宿主有 freeze 类 hook 时优先用 hook**（硬阻断，不依赖 agent 每回合自检）。

**可靠性上限（诚实声明）**：prompt 型护栏的可靠性 = agent 每回合记得自检。长会话/上下文压缩后激活状态必然漂移——恰是它要防的场景。宿主有 hook 就不要依赖纯纪律；用纪律路径就把冻结范围告诉用户，请用户在发现越界时直接纠正。

**典型场景**：调试时"我只加日志，别让我不小心改了无关代码"——freeze 后只允许改指定目录。

**解除**：用户说"解除 freeze""解锁""可以改其他了"（宿主 hook 路径用其解除命令）。

## 激活状态记忆

- **hook 路径 /freeze**（宿主支持时）：状态由 hook 持有，随时可查，不靠 agent 记忆。
- **/careful 与纪律 /freeze**：agent 在**每个回合开始**自检当前激活状态，不需要用户重复声明。激活时把状态记到 session 上下文（如"当前激活：/careful + /freeze src/"），后续回合读这个状态。

状态持续到：用户明示"解除"，或 session 结束。

## Gotchas（高信号）

- **激活后不能"忘记"**：用户说了 /careful，后续整个 session 都生效，不是只管下一次。每个回合开始自检状态。
- **不与自动挡重复**：`rm -rf`/`DROP`/`force-push` 等已被宿主高危命令 hook 拦，本 skill 不重复 STOP——只覆盖自动挡模式之外的（chmod -R 777 / curl|sh / git clean -fd / npm publish / ssh 生产机 / `>` 覆盖已有文件）。
- **STOP 不是拒绝**：STOP 是让用户确认，不是拒绝执行。用户说"确认"就继续——护栏的目的是防误操作不是禁止操作。
- **不要过度拦截**：只拦高危模式，普通 ls/cat/grep 不拦。过度拦截会让用户烦。
- **纪律护栏不是真 hook**：无 hook 支持时 session 级护栏靠 agent 自我约束模拟，长会话/压缩后必漂移（见 /freeze 节的可靠性声明）；hook 型护栏全 agent 生效。

## Red Flags — STOP

- 激活了 /careful 却直接执行 chmod -R 777 / git clean -fd 不确认（忘记激活状态）
- /freeze 后改了锁定目录外文件不确认
- 宿主有 freeze hook 却用 prompt 纪律模拟（该走 hook）
- 用户说"解除"后还在拦截（状态没更新）
- 拦截低风险命令（ls/grep/cat 等只读操作不该拦）

## 与其他 skill 的分工

- **高危命令 hook 层（always-on 自动挡）**：高危命令（rm -rf / DROP / force-push / kubectl delete 等）的硬拦截 + HITL。本 skill 是自动挡之外的补充。
- **delivery-gate**（非本仓库 canonical skill，仅部分 agent 以扩展形式提供）：全局的资产交付门控（写 skill/hook 后验证）。本 skill 是 session 级按需安全护栏。
- **code-review-gate**：代码质量审查。本 skill 是操作安全拦截（防误删/误改）。
- **systematic-debugging**：调试方法论。调试时配合 /freeze 防止"顺手改了无关代码"。
