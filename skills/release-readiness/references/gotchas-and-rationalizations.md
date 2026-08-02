# Gotchas / Rationalizations / Red Flags

发布门禁的经验库：Gotchas 管真实事故、Rationalizations 管堵借口、Red Flags 管 STOP 信号。

## Gotchas（从实际事故积累——最高信号）

- **复用已 push 的 tag**：tag 推到远程后被强制删除重打 → 用户/CI 缓存了旧 commit，部分人拉到旧版部分拉到新版，定位极慢。**铁律：tag 一旦 push，永不复用**，要修就发新版本号。Forge npm 旧版不可撤回的事故就是这个坑。
- **迁移只测了前向没测回滚**：上线后出 bug 想回滚，发现回滚迁移半年没人跑、语法错了 / 数据格式不兼容前版本代码 → 卡在中间态。**M4 强制 staging 双向跑**。
- **破坏性 SQL 被审过但没人当回事**：审阅者"看起来 OK"签字，实际是 `DELETE FROM users WHERE id = ANY($1)` 漏了 `AND deleted_at IS NULL`——code-review-gate 的 sql-safety-checklist 同源。破坏性 SQL 必须双签。
- **secrets 硬编码进镜像层**：源码里 `apiKey := os.Getenv("KEY")` 但 Dockerfile `ENV KEY=sk-xxx` 把生产 key 烤进镜像层，推到公共 registry 泄露。M5 不只扫源码，要扫 Dockerfile/CI yaml。
- **CHANGELOG 漏 Breaking Change**：commit message 写了 BREAKING 但 CHANGELOG 只列在 Improvements，用户没看到 → 升级后生产崩。M2 强制 grep `breaking|破坏|不兼容`。
- **多包/多 README 漂移**：monorepo 根 README 对了，发包的 `npm/README.md` 滞后——发出去用户看到的还是旧 hook 表/旧版本号。M6 必须覆盖每份派生副本。Forge 反复踩过（见 memory skillgen-asset-sync-discipline）。
- **回滚预案写了没演练**：RUNBOOK.md 写得很漂亮，真出事时发现"上一版本镜像 7 天清理策略已删了 N-1" / "数据库 schema 不兼容 N-1 代码"。R1 强制演练。
- **体积暴涨没人在意**：新依赖引入 50MB，CI 都绿，发布后用户安装时间翻倍 / 镜像 pull 超时。M3 强制对比上次发布的体积。
- **发布窗口与并发变更冲突**：发版进行中有人 merge 了新 commit，构建产物混了半新半旧。发布窗口期间 freeze main，或从特定 commit 切出 release branch 构建。
- **"灰度 1% 五分钟没事就全放"**：1% 流量可能根本没触达关键路径（夜间、低峰）。灰度要确认**关键路径流量真的流过新版本**，不是看总错误率，看新版本标签的错误率。

## Rationalizations（堵借口）

| 借口 | 现实 |
|---|---|
| "代码都测过了能上线" | 代码测过 ≠ 发布风险覆盖。迁移/版本号/回滚是发布特有的坑，单测管不到 |
| "回滚以后再说" | "以后"= 永不。出事时慌乱回滚比预先演练贵 100 倍 |
| "Breaking change 用户应该会看 commit" | 用户不看 commit，看 CHANGELOG。Breaking 必须在 CHANGELOG 显式段 |
| "CHANGELOG 等发完再补" | 发完就忘。Tag 推上去 CHANGELOG 还没更新 = 用户拉到无文档版本 |
| "体积涨一点没事" | 涨 50% 用户安装时间翻倍，CI 镜像 pull 超时。>30% 要解释 |
| "staging 跑过就行不用回滚演练" | staging 前向跑过不代表回滚能跑。回滚迁移必须实测 |
| "secret 先硬编码下个版本改" | 一次都不能发。硬编码 secret 进镜像 = 公开泄露 |
| "Recommended 项可以跳" | Recommended 不等于可忽略。未过项必须进"已知风险"段，取得干系人 explicit ack |
| "tag 推错了 force push 修一下" | tag 永不复用。force push 后部分缓存仍指向旧 commit，定位极慢 |
| "灰度五分钟没报错就全放" | 1% 流量可能没触达关键路径。看新版本标签的错误率，不是全局 |

## Red Flags（看到这些想法 = STOP，你在 rationalize 发布）

- "M4 回滚先跳过，前向测过应该没问题" → NO-GO，回滚迁移必须实测
- "M5 secret grep 出一个，先发了再改" → 立即阻断，撤下用 env 替换
- "CHANGELOG 等发完一起写" → 必须先写，tag 一上就不可逆
- "体积涨了 50% 但应该没事" → 必须解释，否则 NO-GO
- "回滚 RUNBOOK 写了，演练下次吧" → 至少 staging 走一次
- "破坏性 SQL 我看了应该 OK" → "看起来 OK"不是评审，要有书面记录或双签
- "Recommended 项先跳过，发完再补风险说明" → 风险说明在发版前，不是发版后
- "差不多能发" → 门控不是建议，NO-GO 就是 NO-GO
- "上个版本发的时候也没出事" → 上次没出事 ≠ 这次没风险，每项都要查
- "灰度跑了五分钟没报错" → 看新版本标签的错误率，确认流量真的过
