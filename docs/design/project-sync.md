# project-sync：项目数据跨机器导出/同步设计

> 特性分支：`feat/project-sync`。本文记录身份层 + 传输层的完整设计决策与依据，代码注释只保留局部 rationale，全局权衡在这里。

## 1. 问题

- 项目身份 key = 仓库 `.git` common dir **绝对路径**的 FNV-64a hash12（`internal/forgedata/key.go`）——换机器/换路径即换 key，拷贝过去的数据目录成孤儿（dashboard/task list 全不可见）。
- 数据本身在用户级 `~/.forge/projects/<key>/` 自包含、task JSON 无绝对路径——可移植性唯一障碍就是身份。
- 需求：**双机持续双向同步**（不是一次性搬家），同步后 dashboard 与 agent 都能看到完整历史。

## 2. 身份层：repo-born project ID

### 决策

- 根级 `.forge-project-id`（`fpid_<32hex>`，crypto/rand）。**刻意不放 `.forge/` 下**——项目级 `.forge/` 目录的存在会把 ConfigDir 翻进 team/legacy 模式（`forgedata/paths.go`）。
- `Key()` 推导优先级：主 worktree 根（`Dir(resolvedGitDir)`）存在**合法** ID 文件 → `IDKey(id)` = `hash12(fnv("fpid:"+id))`（"fpid:" 域前缀与路径 hash 输入不相交）；否则路径 hash。**缺失/非法静默回落**（fail-open：存量项目身份不变，坏文件不 brick hook 热路径）。
- 从**主 worktree 根**读：linked worktree 的 `.git` file 解析到主 repo `.git`，`Dir` 后即主根——所有 worktree 看到同一 ID 文件，维持「一 repo 一 key」契约；主根未 commit 的 ID 对 worktree 也已生效。
- ID hash 输入是文件内容 → 同一 repo 跨 macOS/Linux/Windows 推导相同 key（路径 hash 做不到的跨 OS 红利）。
- 非 git 项目 v1 不支持（PathKey 不动）——无 commit 传输通道，export/import 的 key 重映射已覆盖。

### 为什么当初不直接去路径化（历史决策重申）

git 仓库没有免费且稳定的机器无关身份：remote URL 不唯一可变（fork/镜像/多 remote/零 remote）；内容 hash 每次 commit 漂移；往 repo 写 UUID 违反「项目树零写入」。设计取舍 = 路径身份（零侵入）+ 显式工具兜底搬迁。本特性把「显式工具」补齐，并把「写入项目树」收敛为**一次性、git-tracked、类似 `.gitignore` 性质**的 ID 文件——由用户显式 `forge project adopt` 触发。

### adopt 的顺序（正确性关键）

**先迁数据 → 再写 ID 文件 → 最后同步注册表。** 反过来写 ID 会立刻翻转所有并发 hook 的 `DataDirFor` 到新（空）key 目录，迁移窗口内数据写进旧目录被丢视。残余窗口（ID 已写 ↔ 注册表未同步）由注册表路径回退匹配兜底。

### fork / 复制粘贴语义

fork 与 repo 副本天然共享 ID（committed）。同一开发者的两台机器 = 期望行为；两个**不同**项目因复制粘贴共享 ID = 事故，由 `forge registry audit` 的 `id-collision` 检出，`adopt --regenerate` 逃生。

### 信任边界

ID 文件是 clone 即得的**攻击者可控输入**。风险上限是本地数据目录混流（12 hex = 48-bit key，定向碰撞需 2^48 且需预知目标 key），格式严格校验（`^fpid_[0-9a-f]{32}$`）挡畸形注入；无签名，不作为信任边界——信任判定见 §4。

## 3. 传输层：export / import

### bundle 格式（tar.gz）

```
manifest.json    # format_version=1（0 与 >当前 双拒）、bundle_id、origin{hostname,user,root,key,key_mode,path|id,project_id}、files[]{path,sha256,size}、includes
data/<rel>       # DataDir 相对路径载荷
```

- sha256 防损坏**不防恶意**（无签名）；执行安全由 verify-acceptance `--trust-foreign` 门与 import 的 lineage 判定独立把守。
- 解包安全（镜像 `cli/update.go` extractBinary 模式）：仅普通文件（拒 symlink/hardlink）、拒绝对路径与 `..` 穿越、tar 条目必须在 manifest 列表内且列表条目必须齐全、流式校验 sha256+size。staging 在系统 temp（FORGE_DATA_HOME 之外）——半成品绝不被 DataDir 扫描器发现。

### allowlist（默认拒绝）

单一真相源 `internal/projectsync/allowlist.go`。默认只带：`tasks/*.json`（不含 `*.lock`）、`checklog*.jsonl`、`toollog*.jsonl`、`sessions.jsonl` + `sessions/*.json`（timeline 分组需要）、`act/conclusions.jsonl`、`stamps/*`（除 `stamps/hook-deploy` 机器本地部署戳）、`protocol.yml`。`quarantine/`（被隔离源码全文）与 `hazards/`（完整命令行，可能含 token）须 `--include` 显式选入。**默认拒绝而非枚举排除**：未来新增的机器本地文件（新 sentinel/锚）按构造不外泄。

刻意排除的机器本地件与会话锚：`freeze/state.json`（绝对路径列表，迁移后 fail-open 静默失效——不能让用户以为冻结仍生效）、`active-task-ref-*`/`session.json`/`.task-*-grace-*`/`.resume-stale-*`/`.cold-start-injected-*`（raw copy 后 mtime TTL 造成「他 session 活跃」假象 → review-stop auto-PASS 弱化）、`hooks/`（嵌入重生参考副本）、`.sync-version`、`.migration-meta.json`、`.rekey-backup-*/`、`imports.jsonl`（账本机器本地——随 bundle 旅行会泄露各机 hostname/user 且自身需去重）。

### v1 砍掉 --redact

脱敏行与原行字节不同 → 精确行去重失效 → 回灌产生重复行 + 假数据。敏感 store 默认已排除；对外分享场景已有 `forge task export --redact`。

## 4. 信任模型：lineage 条件判定

| 场景 | 判定 | 行为 |
|---|---|---|
| bundle.key == 本地派生 key（双机 adopt 后的常态） | 受信 lineage | **保留结果字段**（Score/CompletedAt/门禁历史），session 链接恒幽灵化（事实正确：源机 session 本机不存在），验收 Run 不打外来标记 |
| key 不匹配（外来 bundle / 未对齐旧 bundle） | 不可信 | 完整 `StripForeignGateSignals`（与 task import 同哲学：外来门禁信号绝不满足本机门禁） |
| `--untrusted` | 强制不可信 | 同上，即使同 key |
| `--trust-foreign` | 显式放行 | key 不匹配仍按受信合并 |

**为何同 key 默认保留**（用户定案）：双机同步若默认剥离，A 机完成的任务到 B 机变回未完成，反向同步还会把 A 的好状态降级——同步永不收敛，特性失去存在理由。key 相等 = 同一身份 lineage（同人另一台机器）是身份层已证明的事实，比「无条件保留」安全（外来默认仍剥）且比「无条件剥离」可用。伪造 bundle 要骗过 lineage 判定需预知目标 key（路径 hash 或 ID）——本地攻击面，由用户自己搬运 bundle 的信任链承担。

## 5. 合并语义：收敛性设计

合并核心抽至 `internal/datamerge`（`forge registry rekey` 与 `project import` 共享；rekey 语义零变化，其测试原样保绿）。import 侧 options：

- **DedupExactLines**：jsonl 按时间戳稳定有序合并后剔除**字节级重复行**。依据：checklog/toollog/sessions 是 append-only 单写者 `json.Marshal` 确定性序列化——同一事件两次导出字节完全一致，raw line 即稳定身份。解决「两次全量导出重叠导入不重复」。
- **任务合并**（import 在命令层逐任务 `LockTask` 锁内合并，防丢更新）：
  - 受信 → `MergeTaskStateSync` = 并集 + 两条**单调规则**：
    1. **gate prefer-Passed**：同 Gate 两侧一 Passed 一 Failed 取 Passed——executor 前置链只认 Passed，本地权威并集会把「对端修复的 gate」永久卡死；
    2. **完成块单调采纳**：incoming 已完成 + local 未完成 → 整块采纳（CompletedAt/ReviewPassed/Score/Assignment/验收结果）；本地已完成绝不被未完成快照降级。双已完成保本地（幂等收敛）。
  - 不可信 → `MergeTaskState`（本地权威）+ 剥离后的传入状态（无 Passed/完成可并入，规则自动退化）。
- **幂等账本** `imports.jsonl`（机器本地）：同 bundle_id 重复导入跳过（`--force` 重做）；账本**最后**记——中途崩溃无记录，重跑收敛（合并不依赖账本也幂等）。

### 双机同步标准流程

```
A: forge project adopt          # 生成 ID，数据迁移
A: git add .forge-project-id && git commit && push
B: git pull && forge project adopt   # 拿同 ID，对齐（此后同 key）
A: forge project export --out shared.tar.gz
B: forge project import shared.tar.gz   # 受信合并，双向收敛
```

反向同步对称。未 adopt 的机器间仍可 export/import（跨 key 默认剥离，或 `--trust-foreign`）。

## 6. 相关命令

| 命令 | 职责 |
|---|---|
| `forge project adopt` | 生成/采纳 ID + 数据迁移 + 注册表同步（先迁后翻） |
| `forge project export` | allowlist 打包 + manifest（来源身份） |
| `forge project import` | 安全校验 + lineage 信任 + 单调合并 + 账本幂等；`--adopt-id` 引导 |
| `forge registry audit` | key-drift / orphan-datadir / id-collision / invalid-id 只读检出（adopt 忘跑的兜底暴露面） |

## 7. 已评估的边界

| 边界 | 处置 |
|---|---|
| `.forge-project-id` 被 .gitignore | adopt 用 `git check-ignore` 检出并提示 `-f`；import 侧 `--adopt-id` 兜底 |
| import 时活会话并发写 | 预检新鲜锚（<10min）warn；同 key 路径逐任务锁；DataDir 级写锁记 follow-up |
| adopt TOCTOU | 顺序修正后窗口压至 ID 写入↔注册表同步，残余窗口并发写落新目录被 union 收敛 |
| Key() 热路径成本 | +1 stat+read（µs 级），不缓存（缓存会在 adopt 写/删文件瞬间失真） |
| staging 与 autoSync/migrate | staging 在系统 temp，全部扫描器只碰 `.forge/` 与 `FORGE_DATA_HOME`，无交集 |

## 8. follow-up（v1 范围外）

`forge project sync --via <dir>` 双向包装；DataDir 级写锁；非 git 项目 ID；bundle 签名；`--redact`（待与去重兼容的脱敏方案）。
