# Project Policy Layer —— 按项目接管策略（P1 落地设计）

- 状态：P1 实施中（2026-09-02）· 分支 `feat/project-policy-p1`
- 背景：全局 forge 安装（plugin/npm）后 init-suggest 对所有 git 项目自动接管，暴露三个弊端——项目自带 harness 冲突、项目试验新 harness、用户想按项目控制。完整调研（业界学界证据 + 代码考古 + 对抗复查）见 `~/.forge/research/global-forge-optout-20260902-0958/report.md`；本文只记录 P1 落地的设计决策。
- 总原则（调研结论）：**机制全局，行使本地**——装 plugin = 机制存在；接管某项目 = 策略授权；每次 hook 触发 = 仲裁检查。P1 是策略面的地基：状态模型 + 对称命令 + 旁路收编。

## P1 范围（in）

1. **状态模型**：注册表条目 `Entry` 增加 `Status`（空 = managed，向后兼容存量 JSON；`declined` = 已退出）与决策审计字段（`DecisionBy`/`DecisionAt`）。单一真相源 = `~/.forge/projects.json`。
2. **对称命令**：`forge off [--all]` / `forge on`——一条命令、立即生效（下一条 hook 触发即不跑）、升级不重置、幂等。
3. **仲裁收编**（六处）：
   - `IsMember` 只认 managed（refactor 出共享 `lookup`，`State()` 三态 managed/declined/unknown）；
   - `projectroot.Find` 的 legacyFind 自愈分支先查 declined——declined 不自愈登记，返回 `registry.ErrDeclinedProject`（hook 侧沿用"Find 失败即静默放行"的既有分支，免改）；
   - `registry.Add` upsert 保留既有 Status/决策字段（dashboard 自登记等调用方不得复活 declined）；
   - `registry.Rekey` 改 key 时整条目迁移（原实现重建 `Entry{Path,Key}` 丢 status——对抗复查 M7）；
   - init-suggest bash：declined 标记检查前置到成员检查与 FORGE_AUTO_INIT 之前（修复"git 分支 AUTO_INIT 旁路 declined"——G-1；非 git 分支本就先检标记，不动）；
   - `forge init` Go 侧硬门禁：State=declined 时拒绝执行，提示 `forge on`（declined→managed 的唯一路径是显式 `forge on`）。
4. **命令面**：`forge off` 同时写 legacy `.init-suggested/<tag>` declined 标记（init-suggest bash 仍读标记——迁移垫片，P2 把 init-suggest 改为 registry 驱动后移除）；`forge on` 清标记 + 若从未 init（DataDir 无 protocol.yml）则提示运行 `forge init` 补全（不自动跑）；`forge suggest decline/reset` 委托同一核心，语义不破。
5. **可见性**：`forge status` 头部增加接管状态行；declined 项目 `forge status` 以 `ErrDeclinedProject` 的可读文案退出非零（退出码 = "是否 managed 成员"的既有契约保持不变，init-suggest 脚本依赖它）。
6. **审计**：on/off 落 checklog 行（`takeover-policy`）+ Entry 决策字段（by/at）。

## P1 明确不做（out，留后续）

- **P2 默认值翻转**（auto-takeover → ask-once）与 takeover 三档偏好——独立发布。
- **P3 全局通道感知**（skill-trigger per-project 化、用户级指令文件指针化、managed 会话横幅）。
- **P4 外来 harness 检测让位**。
- **注册表写锁**：维持现状立场（原子写 + 接受本地工具低概率并发丢失，writeEntries 注释自认）；on/off 是人触发的低频命令，风险增量小。留后续任务。
- **key 统一迁移**（SuggestTagFor worktree-root tag → common-dir）：P1 双写垫片下两者并存一致；P2 统一。
- dashboard / task-assignment 聚合的 declined 过滤：随 P1 顺带（`ListManaged()`），workspace doctor 保留全量（declined 项目仍有 DataDir，漂移检测需要看到它们）。

## 行为契约（测试钉住的终态）

| 场景 | 行为 |
|---|---|
| `forge off`（managed 项目） | Entry→declined；`forge status` 退出非零（ErrDeclinedProject 文案）；所有 project-scoped hook 静默放行（Find 失败分支）；legacy 标记写入；checklog 记录 |
| `forge off`（从未 init 的项目） | 登记 declined 条目 + 写标记（首次接触前退出，P1 语义下 plugin-takeover 不再吃它） |
| `forge off --all` | 全部存活条目→declined（含逐条写标记） |
| `forge on`（declined 且已 init） | Entry→managed；清标记；hook 恢复 |
| `forge on`（declined 且从未 init） | 只翻状态 + 提示运行 `forge init` 补全（**不自动跑 init**——init 会写用户级 agent 配置，是应显式发生的动作；此时 init 不再被门禁拒绝） |
| `forge on`（unknown，从未登记） | 拒绝并指向 `forge init`（on 只负责 declined→managed，不是第二个 init） |
| declined 项目 `forge init` / FORGE_AUTO_INIT / plugin auto-takeover | init 拒绝（错误文案指向 forge on）；bash 前置标记检查先行静默退出 |
| declined + 遗留 `.forge/` + plugin 项目 | declined 前置检查先于成员分支 exit 0——**跳过原成员分支的 `forge plugin dedupe` 残留清理**（有意取舍：declined = forge 零动作零输出，清理属管理动作；残留 hooks 等重装/重开接管时再收敛） |
| 非 git 目录 | 键 = cwd（路径条目，粒度为目录而非仓库）；git 项目键 = 仓库身份（decline 任一子目录/worktree 命中同一条目） |
| `registry.Add`（declined 条目存在） | upsert 保留 declined（不复活） |
| 存量 projects.json（无 status 字段） | 全部视为 managed（零值兼容），Add upsert 不无谓注入 status 键（不重写既有条目形态） |
| Rekey / List 惰性精简 / Prune | Status/决策字段随条目保留；Prune 只清死路径（declined 活条目保留） |

## 红线对照（调研报告 4.5）

对称 ✅（on/off 各一条命令）· 即时 ✅（hook 热路径查 State）· 不重置 ✅（状态在用户级 store，升级链路不触碰；AUTO_INIT 旁路本次堵死）· 无残留 ✅（零项目写入架构，off 不产生任何项目内文件）· 可验证 ✅（status 状态行 + checklog + doctor 后续节）· 无惩罚 ✅（update/doctor/全局命令与状态正交；skill-scan/mcp-scan 不动——它们本就全局）。
