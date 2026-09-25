# node-identity：节点身份与信任模型设计

> 特性分支：`feat/node-identity`。多机器方向的 Phase 0 地基之一（姊妹篇：`docs/design/sync-convergence.md`）。本文记录节点身份 + 信任 profile 的完整设计决策与依据，代码注释只保留局部 rationale，全局权衡在这里。

## 1. 问题

- Forge 走向「多机器多任务」（同一开发者的多台机器 + 多人团队两种场景并存），但系统里**没有机器身份概念**：bundle origin 只有 hostname/user 散文（`internal/projectsync/manifest.go`），事件行（checklog/toollog/sessions/conclusions）没有任何字段能回答"这条事件来自哪台机器"。
- 没有节点身份，就没有：事件去重键、LWW 决胜键、租约持有者、信任判定对象、Pulse 的机器维度归因。
- 类比：hostcap 解决的是「宿主归因」（事件来自哪个 agent），node-identity 解决的是「机器归因」（事件来自哪台机器）。

## 2. 身份层：公钥即身份

### 决策

- 每台机器首次运行时生成 **ed25519 密钥对**，落在用户级 `~/.forge/node.json`（私钥 0600，永不随 bundle 旅行——进 allowlist 默认拒绝名单）。
- **node_id = 公钥指纹**：`fnode_<32hex>` = 公钥 SHA-256 的前 16 字节。"fpid:"/"fnode_" 域前缀保证与项目 fpid、路径 hash 的输入空间不相交（沿用 `internal/forgedata/key.go` 的域分隔惯例）。
- 选 ed25519：std/crypto 原生（`crypto/ed25519`），签名短（64B），零新依赖——符合 stdlib-only 原则。

### 为什么是公钥指纹而不是随机 ID + 独立公钥

- **身份与证明能力绑定**：随机 ID 需要额外机制证明"我是这个 ID 的合法持有者"（签名挑战），公钥指纹把这个证明折叠进身份本身——签名可验即身份成立。
- **先例充分**：Syncthing Device ID = TLS 证书 SHA-256；Tailscale node key 就是 WireGuard 公钥；Radicle 节点身份 = ed25519 DID。三个不同领域的成熟系统收敛到同一决策，说明这不是风格偏好而是结构必然。
- **事后补密码学是最痛的返工**：先上随机 ID、团队场景再加签名 = 全量换 ID 体系（所有已落盘的 node_id 作废）。先公钥指纹、验签后置 = 团队场景只是打开开关。

### 密钥轮换：证书链预留（v1 不实现轮换命令）

- 教训来自先例的痛点：Syncthing 换证书 = 换身份，所有对端重配；Tailscale 则专门做了 node key 过期/轮换机制。
- **预留格式**：`node.json` 含 `rotation_chain: []`——每个环节是「新公钥 + 旧私钥签名」，trust store 验链后接受身份延续。v1 字段恒为空，但格式从第一天就在，避免"换密钥 = 换机器"的身份断裂。
- 不实现 `forge node rotate` 命令（YAGNI：第二个使用者出现前不建抽象），但任何写 node.json 的代码必须保留该字段。

## 3. 信任层：两套 profile，一套引擎

> 阶段边界：本节 trust store / profile 属 Phase 1-2（团队档开启时实现）；Phase 0 只落地 §2 身份层与 §4 字段格式预留。

### 决策

- **同步引擎单一，信任是可插拔 profile**。个人档与团队档共享 100% 的收敛/传输机制，差异只在验签与权限策略。
- profile 落在用户级 `~/.forge/trust.json`（trust store）：已知节点公钥表 + 每节点 profile（`personal` | `team`）+ 身份声明（user/hostname 散文，仅显示用，不作判定依据）。

| | 个人档 personal | 团队档 team |
|---|---|---|
| 验签 | 签名无条件生成；**SigInvalid（篡改/公钥不符）恒拒**；未签/未知签名者放行+提示 | `require-signed on`：未签或签名者未登记一律硬拒 |
| 信任建立 | 隐式：lineage 判定（同 fpid = 同开发者，沿用 `internal/projectsync` 现有语义） | 显式：`forge trust add <node-pubkey>`（TOFU，SSH known_hosts / Syncthing introduction 先例） |
| 任务租约 | advisory：提示不阻断 | enforced：认领才可变更 + fencing 序号（见 sync-convergence.md §4） |

> **实现校正（feat/trust-profile）**：① 团队档总开关是 trust.json 的全局 `require_signed`（peer 上的 `profile` 字段 v1 为展示语义）。② 「验签关闭」收窄为「放行未签名」——签名无效任何档位恒拒。③ 开启 require-signed 前须让所有节点升级并重新 push（存量无签 bundle 会立即被拒）。④ enforced 租约的命令面（claim 门禁拦截）留待团队档实操时落地，advisory 面已全量实现。

### 为什么 TOFU 而不是 PKI / 中心化签发

目标场景是「同一开发者几台机器」和「小团队」，没有 CA 运维者。TOFU 把信任建立变成一次显式的人肉动作（`forge trust add` 时展示指纹让人核对），与 SSH/Syncthing 同源。中心化 server 签发是 N>5 且 git 通道实测痛了之后的后话（YAGNI）。

### 信任边界

- 私钥不出本机；bundle 签名（v1 预留 `sig` 字段位，恒空）防的是**自动化传输落地后**的篡改——手动 export/import 时代 sha256 已够（防损坏），签名是给 Phase 1 自动同步的预埋。
- node.json / trust.json 都在用户级，不进项目树（延续「项目树零写入」原则），不进 bundle（allowlist 默认拒绝，新增机器本地文件按构造不外泄）。
- **事件里的 node_id 是攻击者可控输入**（同 fpid 文件的信任模型）：个人档信任上限 = 本地数据混流；团队档由验签把关。任何依赖 node_id 的判定必须先过验签关再信其内容。

## 4. 事件签名格式（预留，v1 恒空）

> 阶段边界：本节事件字段（node_id/seq/ts_hlc/sig）属 Phase 0 后续任务（事件打戳），本阶段只落地身份 store 本身。

- 事件行（checklog/toollog/sessions/conclusions）新增字段：`node_id`（string）、`seq`（int64，节点本地单调）、`ts_hlc`（HLC 时间戳，见 sync-convergence.md §3）、`sig`（base64 ed25519 签名，v1 恒空字符串）。
- **老版本兼容**：未知字段按 JSON 惯例忽略；新字段对老版本不可见 → 单向兼容（新读旧全兼容，旧读新静默忽略 sig/node_id）。manifest 的 format_version 守卫不变（bundle 级版本与事件级版本正交）。
- `seq` 由节点本地计数器持久化（`~/.forge/node-seq`），崩溃恢复取 max(持久值, 扫描本地事件最大 seq)+1——单调即可，不追求无洞。
  > **实现校正（fix/dsh-review-followup，2026-08-21）**：扫描恢复未实现——nodestamp 位于项目 DataDir 之下，无从枚举各项目事件，逐项目扫描也不该发生在 hook 热路径。实际防线分两层：① **身份诞生即播种计数器**（nodeid.LoadOrCreate 独占初建路径 no-clobber 写 node-seq=0，契约注在 seedNodeSeqCounter）——身份有五个非打戳创建方（task start 租约认领 / node show / bundle 签名 / sync），不播种则「身份存在而计数器缺失」是新机器常态而非事故信号；② 计数器缺失（播种前的存量机器或真丢失）**告示后从 1 继续**（fail-open 首要原则：打戳绝不阻塞事件），一次性 stderr 告知 (node_id, seq) 复用风险；损坏仍恒禁用。A 类 G-Set 以 (node_id, seq) 为键启用前，须把「缺失」升级为硬禁用或补跨项目 seq 观测点。

## 5. 机器归因的展示面（Phase 3 预埋）

- Pulse（`internal/dashboard`）渲染 feed 时加 node 维度标签；trust store 的 user/hostname 散文只做显示名。
- **数据同步过来、dashboard 保持本地只读**的路线不变——不做远程 API。

## 6. 先例对照（决策依据留存）

| 决策 | 先例 |
|---|---|
| 公钥即身份 | Syncthing device ID、Tailscale node key、Radicle ed25519 DID |
| TOFU trust store | SSH known_hosts、Syncthing introduction、git SSH `allowed_signers` |
| 轮换证书链 | Tailscale node key 轮换（反面：Syncthing 不可轮换） |
| 验签默认关、引擎单一 | 双 profile 分层参考 atuin（record 层加密、传输层哑） |

## 7. 明确不做（v1）

- 不做中心化身份 server / CA。
- 不做远程 dashboard API。
- 不实现轮换命令（只留格式）。
- 不做硬件密钥/TPM（开发者机器场景，软件密钥够用）。
