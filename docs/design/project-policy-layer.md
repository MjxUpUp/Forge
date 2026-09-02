# Project Policy Layer —— 按项目接管策略（P1 落地设计）

- 状态：P1 已交付（feat/project-policy-p1）；P2-P4 已落地（2026-09-02，feat/project-policy-p234）
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

## P2-P4 落地状态（原"P1 明确不做"清单——已全部完成，feat/project-policy-p234）

- **P2 默认值翻转 + takeover 三档偏好** ✅：`internal/userconfig`（`~/.forge/config.json`）+ `forge config get/set takeover`；env 链 `FORGE_TAKEOVER > FORGE_AUTO_INIT=1(≡auto) > 配置 > ask`。init-suggest 按三档分流（off 静默 / auto 静默 init / ask 询问一次——含接管内容摘要与 forge config 指引）。
- **key 读侧统一** ✅：init-suggest declined 门补 `forge policy state` 注册表权威读侧（common-dir 键），闭合 linked-worktree 下 marker（工作树根键）漏判；写侧双写垫片保留。
- **P3 全局通道感知** ✅：skill-trigger 在生产/调试入口按 root 判 managed（runSkillTriggerHook/runSkillTriggerCmd）；task-resume hook 恒输出 `[forge-session]` managed 会话横幅（用户级指令段与 forge-quality skill 的激活判据锚定于此，取代不可判定的"已 init"条件）；用户级指令段全类指针化（skillgen CLAUDE.md/AGENTS.md + windsurf global_rules.md；spec-kit"一行指针"共识）；**update 后重刷新由既有 autoSync 版本戳机制保证**（版本变更后下一条命令自动重写用户级段，无需新代码）。
- **P4 外来 harness 检测让位** ✅：`internal/harnessdetect` 信号表（`.specify/`、`.claude/commands` 非空、`.claude/settings.json` 含 hooks/permissions、`.cursor/rules` 非空——宁可漏判不误判）；`forge policy yield` 命中即让位（declined, by=foreign-harness）+ bash 分支（压过 auto/AUTO_INIT）；手动 init 打警告继续（显式覆盖是知情选择）；`forge off --commit` 写 `.forge-decline` 团队声明（committed，deny-wins 压过注册表 managed），`forge on` 移除。
- **注册表写锁** ✅：`internal/registry/lock.go`（O_EXCL + 10s 过期破锁 + 2s 有限重试，放弃时退化到无锁行为不阻断命令）；Add/SetStatus 整体入锁，Rekey/List 惰性精简的写回段入锁；并发守卫测试（24 并发 Add + SetStatus 竞态；`-count=3` 复跑验证）。
- dashboard / task-assignment 聚合过滤：P1 已随 `ListManaged()` 落地（workspace doctor 保留全量）。

### P2-P4 行为契约补充（测试钉住的终态）

| 场景 | 行为 |
|---|---|
| 出厂默认（无配置无 env） | ask：未接管项目首次会话询问一次，不登记 |
| `FORGE_TAKEOVER=off` / 配置 off | 非成员静默；成员不受影响 |
| `FORGE_TAKEOVER=auto` / 配置 auto / `FORGE_AUTO_INIT=1` | 静默 init（declined/yield 仍拦） |
| 外来 harness 信号（非成员首会话） | 让位：declined(by=foreign-harness) + 一行说明；`forge on` 覆盖 |
| 外来 harness 信号 + 手动 `forge init` | 警告但继续（warnForeignHarness，advisory） |
| `.forge-decline` 存在（git 根） | State=declined、IsMember=false（deny-wins 压过 managed）；`forge on` 移除文件 |
| skill-trigger（非 managed / declined） | 静默（生产与调试入口同门） |
| managed 项目 SessionStart | 恒输出 `[forge-session]` 横幅（有/无活跃任务都出，单 PASS 协议） |
| 用户级 CLAUDE.md/AGENTS.md/global_rules.md | 指针段（横幅锚定 + 项目协议优先 + 自保护）；版本变更后 autoSync 重刷新 |
| 并发写注册表 | 写锁保不丢条目；SetStatus 后写胜出 |
| declined 项目 `forge status --json` | stdout 输出 JSON 错误包裹（`{"error":...,"takeover":"declined"}`）|

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
