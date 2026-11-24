# Changelog

## [4.0.0](https://github.com/MjxUpUp/Forge/compare/v3.0.1...v4.0.0) (2026-11-24)

### ⚠ 破坏性变化（core-only，2026-11-24）

* **命令面从 200 条收窄到 86 条**：旧时代命令组整组删除——`act`、`bridge`、`config`、`conventions`、`dashboard`、`enforcement`、`eval`、`gate`、`harness`、`health`、`migrate`、`node`、`policy`、`project`、`registry`、`skill`、`skills`、`trace`、`trust`、`update`、`verify`、`workspace`、`worktree` 均已删除（运行得到 `unknown command`）。保留 `init / on / off / version / next / status / change / task / review / docs / impact / test / compat`。
* **技能脚手架整包移除**：`internal/cliskills`（约 3.2k 行）与 `skillintegrate`/`skillmetrics`/`skillscanonical`/`skillsdecisions`/`skillsdist`/`skillseval`/`skillsfm`/`skillsqa`/`skilltrigger` 等包及其测试删除（合计约 14k 行）；`skills/` 与 `skills-forge/` 的 SKILL.md 内容目录保留，由 `TestSkillsContentDirs` 守卫结构。
* **评测与导出体系删除**：`internal/aatout`、`internal/otelout`、`internal/evalsuite`、`internal/artifactchain`、`internal/datamerge`、`internal/dashboard`、`internal/bridgeverify`、`internal/projectsync`、`internal/skillcap`、`internal/userconfig` 删除；checklog 保留为审计事实源。
* **18 个宿主时代检查名移除**（bash-guard / file-sentinel / scope-drift / artifact-chain / hook-infra 等），`TestNoHostEraCheckNames` 防止它们被加回。
* **文档随之收窄**：`docs/plans`、`docs/surveys` 与 22 份历史设计删除；`docs/design/compat-commitments.md` 只承诺 core-only 后仍在的三面（数据面/命令面/分发面），历史裁决移到 `docs/archive/compat-rulings-2026.md`；`forge docs closure` 的守卫面扩到 README/npm/homepage/READMEs/design/compliance/migration，历史设计用「状态：历史」标记豁免。

### Bug Fixes（core-only 补充）

* **判据选择器空匹配不再假绿**：`change verify` 与 `task verify-acceptance` 对 `-run` 指向不存在测试、`no test files`、`-v` 下无 `=== RUN` 的 go test 输出判负（`internal/criteria`）。
* **测试账本不再把条件跳过当噪声**：`forge test ledger` 只把无条件的 `t.Skip` 算作 skipped；`if`/`switch`/循环分支里的平台闸门单列为「条件跳过」。



### ⚠ BREAKING CHANGES

* **cli:** 未知子命令 exit 1；清空 18 个历史检查名
* **cli:** 删除 review llm 判官臂与 internal/evalkit
* **core:** 只留核心命令面——删旧时代命令组、死检查名、死接线与历史计划文档

### Features

* **agents:** 托管块由命令树生成，带块头指纹与漂移检查 ([6888e48](https://github.com/MjxUpUp/Forge/commit/6888e48ec9d1381ff7c0f5cc4c2787578374e936))
* **capability:** 能力面闭合表——命令树与活文档的漂移进 verify 摘要 ([20ef5a6](https://github.com/MjxUpUp/Forge/commit/20ef5a6d1d8b4e45300cb9020f1c60deac446b3c))
* **test:** 测试账本——执行记录为真相源、Skip 需理由与审批、impact 报测试状态、增量 mutation 探针 ([61788e0](https://github.com/MjxUpUp/Forge/commit/61788e028fcfc48b1fdaa250f5abaeeef26a8fd5))


### Bug Fixes

* **agentproto:** 命令引用在标点处截断，并写明覆盖边界 ([0258201](https://github.com/MjxUpUp/Forge/commit/0258201594d178cdf74275ee9077eb47f115d4f0))
* **agentproto:** 版本串归一成单 token，--check 只比正文指纹 ([bd03e03](https://github.com/MjxUpUp/Forge/commit/bd03e03b073162b807649aa6463596387dfdd761))
* **ci:** 发布链断言跟上 4.0 命令面，新增 workflow 死命令守卫 ([46d03e6](https://github.com/MjxUpUp/Forge/commit/46d03e6bb2e9bb58c5c8bc3507a62537fe82c03f))
* **ci:** 守卫脚本对齐 core-only 后的仓库现状 ([862deff](https://github.com/MjxUpUp/Forge/commit/862deffcbe50d077a65ab0afa5fe1bd9c98f6010))
* **ci:** 清掉 canary 报出的不可达符号，发布工作流断言改用现存命令 ([384ea23](https://github.com/MjxUpUp/Forge/commit/384ea23dbcded393f052530c72143c6f9b20004b))
* **ci:** 清掉 workflow 里的已删命令，gofmt 五个文件 ([a625507](https://github.com/MjxUpUp/Forge/commit/a625507d10a01b43771f10439f810cc48ccaf8ef))
* **ci:** 迁移段按版本号识别——1.85–2.0 升级节的 forge hook 讲解不再误判 ([f32a335](https://github.com/MjxUpUp/Forge/commit/f32a3357a6d0238d969609b3f872bebd12431962))
* **cli:** 未知子命令 exit 1；清空 18 个历史检查名 ([501ac36](https://github.com/MjxUpUp/Forge/commit/501ac36a6aefc4e4b38cf3c657e6d777200d6953))
* **core:** 假绿判负、死代码/死提示清空、技能脚手架移除 ([0263a76](https://github.com/MjxUpUp/Forge/commit/0263a764b33274ef149d9d592e17845ce4dac189))
* **core:** 收口核心清理的五条残留风险 ([69d08d2](https://github.com/MjxUpUp/Forge/commit/69d08d2de1ae716a51dd4ca324f51e4855145ea8))
* **docsconsistency:** 「已删除」语境行豁免，迁移表不再被报成漂移 ([5ef62d0](https://github.com/MjxUpUp/Forge/commit/5ef62d0a8f48f30c328944ed924b67aca9c19b78))
* **gate:** 默认值硬化——-run 空匹配判负、先失败证明必填、review 必须声明核对判据数 ([3e213de](https://github.com/MjxUpUp/Forge/commit/3e213ded8b97f4d3e5790000e939494e850d551f))
* 评审修复——选择器多包判定、探针未跟踪文件、账本子测试、门禁严格化 ([a3a496c](https://github.com/MjxUpUp/Forge/commit/a3a496c1bcf518e256ff2250cb079546c6125fc6))


### Code Refactoring

* **cli:** 删除 review llm 判官臂与 internal/evalkit ([47b3b55](https://github.com/MjxUpUp/Forge/commit/47b3b55ed6ffc9a10f5f7a29902a525ea1c27320))
* **core:** 只留核心命令面——删旧时代命令组、死检查名、死接线与历史计划文档 ([cbcfb42](https://github.com/MjxUpUp/Forge/commit/cbcfb424e5dcfe285bb4896d956beb55db0c6680))

## [3.0.1](https://github.com/MjxUpUp/Forge/compare/v3.0.0...v3.0.1) (2026-11-20)


### Bug Fixes

* **next:** 收尾提示改为采集完成事实，不再提已删的评分 ([83dcd16](https://github.com/MjxUpUp/Forge/commit/83dcd165cc5d7a66aac137e40f6846586bbb8b91))

## [3.0.0](https://github.com/MjxUpUp/Forge/compare/v2.3.1...v3.0.0) (2026-11-19)

### ⚠ 升级注意

* **复审改为「评审者复用」**：同一个任务的 code-review 必须由首轮绑定的同一个子 agent 继续（`--reviewer <id>`），源码变更后再用**新** reviewer 会被拒；确实需要换人时用 `--rotate-reviewer --reason "<原因>"` 显式留痕。原先「源码一变就必须换新 reviewer」的规则已反转——每轮换人会把上一轮的问题丢掉，评审收敛不了。上一轮的未闭合项用 `--findings <清单>` 承接，关闭或 `(waived: 原因)` 才放行。
* **带 `--change` 启动必须登记验收判据**：`task start --change <id>` 至少一条 `--accept`；确实没有可执行判据时用 `--allow-empty-criteria` 留痕。
* **先失败证明（可选但会被披露）**：`task start --accept-red "<命令>"` 登记开工前必须失败的判据；`task complete` 要求同一命令已经转绿（`go test` 判据两侧按 `-v` 归一化）。没登记不拦，但交付单会写「未登记」。

### ⚠ 行为变化（entry-honest，2026-11-19）

* **评分合成分整体删除（drop-score 第二批）**：`TaskState.score` 及其子键从序列化面移除，任务完成只保留事实清单 `TaskState.evidence`（判据通过数、红先绿、评审轮次、逃生数）；`forge act health`（dashboard pulse）不再输出 `overall`/`grade`，`forge task golden`、`internal/scoring`、`internal/scoringtypes` 一并删除；merge 收敛改用事实清单比较。旧 state 的 `score` 键按未知字段忽略，`evidence` 缺失时按 checklog 与状态现算事实。`forge verify --collect-golden` 与 `FORGE_RECURRENT_HARDEN` 同步移除；`protocol.yml` 的 `scoring:` 段与 act 结论/dashboard/skills/task audit 的合成分字段也不再读取（旧文件与旧结论按未知键忽略）——破坏性面与预告见 `docs/design/compat-commitments.md`「2026-11-20 drop-score 第二批」。
* **`forge next` 不再推荐陈旧 change**：ready 但超过 72 小时没动过的 change 只进 `stale_changes` / `stale_change_ids`，不会被当成下一步；有脏树时给 `forge change new`，干净树仍给 `task start`。陈旧判定：工作区干净的 change 看它最近一次提交时间；有未提交改动才取「文件时间与提交时间的较大者」；浅克隆无法判定历史时一律按陈旧处理，换机器/新 clone 不会把旧活刷成新鲜。
* **删掉「考卷必须含测试类命令」启发式**：`task complete` 不再按命令名猜判据质量（`go test`/`npm test` 之类前缀）。判据可信度由实跑回扣、新鲜度、红先绿、评审认领共同决定；`node --test`、`test -f x` 等合法判据不再被误拒。
* **协议不再默认先跑 `forge next`**：用户提出新需求时直接 `forge change new` / `forge task start`；next 只用于继续或接手已有任务。

### Features

* **测试先行三事实**：红先绿证据（`--accept-red` + `task complete` 校验）、判据先于实现（带 `--change` 必须登记判据）、判据溯源（`review pass --criteria-reviewed <n>`，交付单显示 `n/N`）。
* **评审连续性**：`review pass` 记录 reviewer 绑定与轮次（未闭合/已关闭/换人次数），`forge next` 的复审提示直接带上次的 reviewer id；`task report` 新增「先失败证明」「评审连续性」两行。

### ⚠ BREAKING CHANGES

* **drop-score:** TaskState.score 及其 9 个子键移除，完成面只保留事实清单 TaskState.evidence（判据通过数、红先绿、评审轮次、逃生数）；forge task golden 与 forge verify --collect-golden 移除；FORGE_RECURRENT_HARDEN 逃生舱移除；protocol.yml 的 scoring: 段不再读取；act 结论 score/grade/dim_scores/low_dimensions、dashboard pulse stats avgScore/medianScore/trend/gradeDist/lowDims、skills 聚合 avgScore、task audit score_overall 一并移除。internal/scoring 与 internal/scoringtypes 整包删除；merge 收敛改用事实清单比较。旧 state/配置/结论按未知键忽略。破坏面记录见 docs/design/compat-commitments.md「2026-11-20 drop-score 第二批」。

### Features

* **change-drift:** forge change drift 对照声明与实际改动（advisory） ([cca4752](https://github.com/MjxUpUp/Forge/commit/cca47521bba5837eabef9b3bf8d92638b080b803))
* **drop-score:** 删除评分合成分（第二批） ([7766fe8](https://github.com/MjxUpUp/Forge/commit/7766fe8a87cb1f12143efec23c84db9784312a2e))
* **skill:** forge skill new 把 retro 教训落成 SKILL.md ([cb7a147](https://github.com/MjxUpUp/Forge/commit/cb7a14700688a33827160d33f43b2218c07357d0))
* **trajectory:** checklog 记 duration_ms/steps/attempt，report 显示轨迹 ([3cb612f](https://github.com/MjxUpUp/Forge/commit/3cb612f67b656c85ab615309dc325e78d7b852b1))


### Bug Fixes

* **act:** rebuild 用已落盘事实清单复原完成结论 ([e3ed7dd](https://github.com/MjxUpUp/Forge/commit/e3ed7dd4519d3d7089801789244a2ff6fbc6e487))
* **checkpoint:** 从 git stash create 输出里解析对象名，容忍警告行 ([90e1971](https://github.com/MjxUpUp/Forge/commit/90e1971572275e7477e7082770bd0b3724298fa0))
* **checkpoint:** 修评审四条 P1——无任务判空、ID 唯一、拒绝 untracked、refs 保活 ([9d9a361](https://github.com/MjxUpUp/Forge/commit/9d9a361577389293af0b0051cdf33cb03d7a79c7))

## [2.3.1](https://github.com/MjxUpUp/Forge/compare/v2.3.0...v2.3.1) (2026-11-19)


### Bug Fixes

* **eval:** 与 verify 同语义的 set -e、超时收进程树、--json 也落盘、失败原因取尾部 ([9ace0eb](https://github.com/MjxUpUp/Forge/commit/9ace0ebd9d876e04c764953e6af32f2c3826a06d))
* **eval:** 围栏整段交给 shell、超时与纯 JSON、失败原因可见 ([ab2ba6b](https://github.com/MjxUpUp/Forge/commit/ab2ba6bc3b3c7bb40cc469eb6c95397f184413ff))

## [2.3.0](https://github.com/MjxUpUp/Forge/compare/v2.2.0...v2.3.0) (2026-11-19)


### Features

* **drop-score:** complete/report/list 只报事实清单，不再产出合成分 ([22ea220](https://github.com/MjxUpUp/Forge/commit/22ea2209a261984189a665771718b421b2c81e0f))


### Bug Fixes

* **drop-score:** 事实清单不再洗白——未测面现算、红先绿归一化、逃生读 checklog ([6a892c8](https://github.com/MjxUpUp/Forge/commit/6a892c8e9b43d80bd9969bdfa0e627f33ab0ddd0))

## [2.2.0](https://github.com/MjxUpUp/Forge/compare/v2.1.3...v2.2.0) (2026-11-19)


### Features

* **entry-honest:** 删命令名启发式考卷门禁，next 不再推荐陈旧 change ([071a353](https://github.com/MjxUpUp/Forge/commit/071a353d2c3ccacdbcc4b7b9e4a5b027a9a5a3e1))


### Bug Fixes

* **entry-honest:** 浅克隆按陈旧处理，不推荐无法判定的旧 change ([e09f40f](https://github.com/MjxUpUp/Forge/commit/e09f40f761971e50f608a0d682fd8855ae6ac6f7))
* **entry-honest:** 浅克隆按陈旧处理并补测试，rename 不再污染 change id ([28b53cf](https://github.com/MjxUpUp/Forge/commit/28b53cfbaa4637e94f7a84664cec602587d9648e))
* **entry-honest:** 陈旧判定改为干净树看提交、脏树取 max，并补批量与浅克隆说明 ([74853e0](https://github.com/MjxUpUp/Forge/commit/74853e0b21c5614de62e2d255426fed3d6bfc7a7))
* **entry-honest:** 陈旧判定改用提交时间兜底、半成品优先、陈旧 id 进 state ([f97009d](https://github.com/MjxUpUp/Forge/commit/f97009da2ccee459ae66baf5db0013f30d6c24e8))

## [2.1.3](https://github.com/MjxUpUp/Forge/compare/v2.1.2...v2.1.3) (2026-11-18)


### Bug Fixes

* **ci:** npm 幂等判据改为 tarball 可下载，自检窗口扩到 20 分钟 ([f6008eb](https://github.com/MjxUpUp/Forge/commit/f6008eb41fe72c5e955895340a5cbff0489ebfa6))

## [2.1.2](https://github.com/MjxUpUp/Forge/compare/v2.1.1...v2.1.2) (2026-11-18)


### Bug Fixes

* **backlog-p3:** 幻影判定再收一层——HEAD 里仍跟踪的保留名文件不算幻影 ([098fed9](https://github.com/MjxUpUp/Forge/commit/098fed966198794ccb1e0d3b1ba4f26cc7391189))
* **backlog-p3:** 幻影判定改看磁盘存在性，补顺序/接线测试与证据入库 ([3e00cce](https://github.com/MjxUpUp/Forge/commit/3e00cce4e4304474548ceb4275ba0d0ca100f864))
* **backlog-p3:** 幻影路径判定收敛到 ghostpath，gate/verify preflight 顺序对齐 ([2863fd6](https://github.com/MjxUpUp/Forge/commit/2863fd6bec5556905eb08d596283a4d00aaa15a6))
* **ghostpath:** 保留名当目录永远是幻影，单文件才看磁盘/HEAD ([938f4dd](https://github.com/MjxUpUp/Forge/commit/938f4dd2e3482e7cf52af8c3acc690c79e1df921))


### Performance Improvements

* **ghostpath:** 非保留名路径短路，不再为每行脏状态 spawn git ls-tree ([62c1f2b](https://github.com/MjxUpUp/Forge/commit/62c1f2bfc2ad451c0c3544609d19ff6eb063972a))

## [2.1.1](https://github.com/MjxUpUp/Forge/compare/v2.1.0...v2.1.1) (2026-11-18)


### Bug Fixes

* **backlog:** 文档写清 first-parent 盲区与 5 秒窗口；封印 070910Z 入库 ([737b842](https://github.com/MjxUpUp/Forge/commit/737b8428d99198ddaf17cddc85105a002990c930))
* **backlog:** 红快照绑开工时刻、实现者独立性、幻影路径收口 ([59659ee](https://github.com/MjxUpUp/Forge/commit/59659ee77b026ca28fbd77913415b62f2671bdb9))
* **backlog:** 评审闭环——实现者必须指定评审者、红快照容差收到 5 秒、合并按 first-parent ([eb929d5](https://github.com/MjxUpUp/Forge/commit/eb929d571e57633acac6b30686e31ee3e875c54f))

## [2.1.0](https://github.com/MjxUpUp/Forge/compare/v2.0.4...v2.1.0) (2026-11-18)


### Features

* **red-first:** 红先绿证据、判据溯源与评审者复用连续性 ([1774965](https://github.com/MjxUpUp/Forge/commit/177496526a347d8116565189197c5ad6aae8e89d))


### Bug Fixes

* **e2e:** 临时目录清理失败不得把 red-first e2e 判成失败（Windows 句柄） ([9605405](https://github.com/MjxUpUp/Forge/commit/960540588f56c6958de3408a5bf7801849d41b49))
* **guards:** docs-artifact-counts 豁免迁移对照表（行内含三份/迁移/升级/对照） ([3bc6e05](https://github.com/MjxUpUp/Forge/commit/3bc6e054d0961c487bb65d1b6eedd5a572084afe))
* **red-first:** go test 判据按 -v 归一化、豁免原因去空白、零判据拒认领 ([16c617f](https://github.com/MjxUpUp/Forge/commit/16c617f99ce80ffeff03b7192604b4fc3f5818dd))
* **red-first:** 接线 complete 的红先绿检查、豁免须写原因、换人披露 ([0322d99](https://github.com/MjxUpUp/Forge/commit/0322d9963a23eedf1388e208cc5ec08b2ccbc91a))
* **review:** 门禁指引带 --reviewer、豁免原因只认可打印字符 ([d32c5de](https://github.com/MjxUpUp/Forge/commit/d32c5de0f9e6282b3ad810e74bb37cff4d17d8a8))

## [2.0.4](https://github.com/MjxUpUp/Forge/compare/v2.0.3...v2.0.4) (2026-11-18)


### ⚠ 升级注意

* **`clarify.md` 成为 `change verify` / `change gate` 的硬前置**：三份布局（`goal.md` + `change.md` + `acceptance.md`）的 change 若还没记录 `人确认：<UTC>`，升级后 `verify`/`gate` 会直接拒绝（报错会给出 `forge change clarify <id> --by-human` 的修法）。老布局（`proposal.md`/`design.md`/`tasks.md`）不受影响。


### Bug Fixes

* 按 review-leftovers-204 收紧——保留名过滤仅 Windows、证据侧同修、gate 同口径要 clarify ([8c6b1c4](https://github.com/MjxUpUp/Forge/commit/8c6b1c464385f51d8b8f00c0477263e75b56e218))
* 闭合 2.0.3 遗留——保留名幻影树、clarify 硬前置、守卫与冒烟补路径 ([12af1a1](https://github.com/MjxUpUp/Forge/commit/12af1a15c1036bc20fefc8831a7ab9034d18dbc2))
* review 的文件枚举跳过 Windows 保留设备名幻影路径（`nul/` 等），且该过滤只在 Windows 生效——Linux/macOS 上 `aux.go` 这类正常源码不会被误吞
* change 证据的 `files` 指纹同样过滤保留设备名幻影路径，不再出现「什么都没改、`change check` 却报证据失效」
* 发布冒烟改为真行为：临时仓库里建 change → 填产物 → 提交后重验（封印只绑 head）→ check；守卫脚本不再被空模式假绿


## [2.0.3](https://github.com/MjxUpUp/Forge/compare/v2.0.2...v2.0.3) (2026-11-17)


### Bug Fixes

* **ci:** 发布冒烟接受 PATH 上的裸命令名 ([552241a](https://github.com/MjxUpUp/Forge/commit/552241a283d2e4063222cd10ab99560a5892fefc))

## [2.0.2](https://github.com/MjxUpUp/Forge/compare/v2.0.1...v2.0.2) (2026-11-17)


### Bug Fixes

* **npm:** 包装器兼容全局安装的嵌套落点，spawn 同步抛错也响亮失败 ([fd0541c](https://github.com/MjxUpUp/Forge/commit/fd0541ce28e7ef87aa6d93290ead6b022b454ad0))

## [2.0.1](https://github.com/MjxUpUp/Forge/compare/v2.0.0...v2.0.1) (2026-11-17)


### Bug Fixes

* **npm:** 启动器按同级平台子包解析，禁止祖先目录遮蔽与静默 approve ([d4f62f7](https://github.com/MjxUpUp/Forge/commit/d4f62f7f039e473889051dd659887a1a4ee1491c))

## [2.0.0](https://github.com/MjxUpUp/Forge/compare/v1.88.1...v2.0.0) (2026-11-17)


### ⚠ BREAKING CHANGES

* task-verify/doc-review/mutation/artifact/chain-init/selfcheck/eval artifact-drill 已删除；change 产物由四份/五份改为三份（goal/change/acceptance，tasks 可选）；review pass 只接受新的 --reviewer。旧布局与旧 change 仍可读。

### Features

* 门禁降为两道、产物改三份、删除十项检查 ([c048d7a](https://github.com/MjxUpUp/Forge/commit/c048d7a8e169d96bf678358d690aaa8e5ce433d6))


### Bug Fixes

* **2.0:** 清理静态检查未用符号、旧模板与已删逃生舱并复钉 compat ([da92fc1](https://github.com/MjxUpUp/Forge/commit/da92fc1799f343a895f0bad984c2ebf9ffe3df9a))
* **ruthless:** 评审闭环——next/selfcheck/文案/预跑/墓碑/三份口径 ([8e10124](https://github.com/MjxUpUp/Forge/commit/8e10124ab18a013f71a75b7a594bd60a2de631cb))
* **ruthless:** 闭合 review-ruthless-3——complete 复核 review 快照、reviewer 归一化、删命令报错、gate 同口径验收前置 ([4d08487](https://github.com/MjxUpUp/Forge/commit/4d084871e447c64a85ddd50f7a22524fa092b8ed))
* **ruthless:** 闭合 review-ruthless-4——工作流口径、审查基线不可达改拒、活提示与守卫接入 CI ([09330db](https://github.com/MjxUpUp/Forge/commit/09330dbf9d3c336dfd012e247984121e893d2fc1))
* **ruthless:** 闭合 review-ruthless-5——基线不可达拒绝重盖章、发布冒烟改行为级、复审口径统一、守卫覆盖 ADR ([e7b9b2a](https://github.com/MjxUpUp/Forge/commit/e7b9b2a2be3c0c9435cad810d347b301dab004eb))

## [1.88.1](https://github.com/MjxUpUp/Forge/compare/v1.88.0...v1.88.1) (2026-11-16)


### Bug Fixes

* **report:** 交付单披露已钉 change；Windows 提示 MSYS 风格 FORGE_DATA_HOME ([6b01d25](https://github.com/MjxUpUp/Forge/commit/6b01d25baf1b85b26c7ecd862badc168b9d21eed))

## [1.88.0](https://github.com/MjxUpUp/Forge/compare/v1.87.3...v1.88.0) (2026-11-16)


### Features

* 加强隐私边界——证据封存与 bundle 外发默认脱敏 ([98c56b7](https://github.com/MjxUpUp/Forge/commit/98c56b795d359cb89625c3e8b70d27264ba26ba0))


### Bug Fixes

* **ci:** 历史重写后的两处 Linux 失败——raw string 字面 \n 与 hostname 脱敏顺序 ([c34f0c7](https://github.com/MjxUpUp/Forge/commit/c34f0c72cf8eee4013a685086f88ba1a57d914b4))
* **ci:** 同步推送确认在测试里显式跳过；重钉 compat 快照 ([1afc3c6](https://github.com/MjxUpUp/Forge/commit/1afc3c6bffc2684f69082aea53422258686bf998))
* **ci:** 清掉 staticcheck U1000 的四处死声明（历史重写清理残留） ([15c3df5](https://github.com/MjxUpUp/Forge/commit/15c3df510b3f2cd0b0187215dbfd0a904599e038))

## [1.87.3](https://github.com/MjxUpUp/Forge/compare/v1.87.2...v1.87.3) (2026-11-16)


### Bug Fixes

* **cli:** forge version 子命令；start 产物链提示走 change 口径；report 未验证面诚实披露 (`03424e7`)
* 评审回件——version 归组 + compat 重钉；report 三态（Go 测试文件）；判据覆盖整包 cli (`abf8eda`)

## [1.87.2](https://github.com/MjxUpUp/Forge/compare/v1.87.1...v1.87.2) (2026-11-16)


### Bug Fixes

* **gate:** task-complete 与 complete 同口径；交付单披露文档回检 (`1082b84`)
* **gate:** 按代码评审 P1/P2/P3 修夹具回归、提示落点、报告判定与产物链边界 (`d14886c`)
* **gate:** 补 compat 首发声明与快照，报告判定/零值测试与判据覆盖（代码评审二轮） (`7a223ae`)

## [1.87.1](https://github.com/MjxUpUp/Forge/compare/v1.87.0...v1.87.1) (2026-11-16)


### Bug Fixes

* NextRepoContext 排除无 goal 的老 change；cli 委托同一实现；提示只在无任务时读 spec (`02ab6b7`)
* **routing:** next 干净工作区直接开工；契约仓库评分静默回退；AGENTS/cli 文档补齐门禁步骤 (`2077efb`)
* **scoring:** 区分 protocol.yml 缺失与坏文件——契约仓库缺失静默、坏文件仍告警 (`e1174a2`)
* 按双评审修 dogfood-fixes（生产路径接线、NextHint 上下文、文档顺序与判据） (`e609d00`)

## [1.87.0](https://github.com/MjxUpUp/Forge/compare/v1.86.0...v1.87.0) (2026-11-15)


### Features

* **routing:** 任务是一等入口，change 可后补（A1+A2+A3） (`76546d3`)
* **routing:** 入口路由与阶段机——功能改动先立 change（A1+A2+A3） (`e2c6f7b`)


### Bug Fixes

* **routing:** 代码评审 P3——note-file 仓根解析、老 change 提示、判据补覆盖 (`21917f6`)

## [1.86.0](https://github.com/MjxUpUp/Forge/compare/v1.85.2...v1.86.0) (2026-11-14)


### Features

* **change:** goal.md 作为第五份产物，task start 钉哈希；fast 需人批 (`23e6e9f`)
* **change:** 协议可执行化——先跑 forge next、Task 门禁段、1.84 迁移说明 (`b878cc8`)


### Bug Fixes

* **change:** 按双评审修迁移覆盖、门禁顺序与守卫假绿，托管块同步 (`7a7687c`)
* **change:** 按文档复评修迁移破坏面、Cursor/其余宿主与 hooks 字段注释口径 (`eaf8113`)
* **change:** 收 review 回件——change id 边界、新 change 必留 goal.md、门禁接线测试 (`e7e85ef`)
* **change:** 收文档 L2 回件——模板去掉不存在的 decisions 出口、判据换成防回归脚本 (`83167c0`)
* **ci:** no-hook-refs 排除 1.84 迁移节（该节本来要引用旧命令做清理） (`64d5324`)
* **clitask:** --worktree 启动在 worktree 内重解析 goal 契约（P2-A） (`25f8a4f`)
* **docs:** forge-quality 同时清 ~/.agents 副本并把删除命令移出注释 (`77b4a56`)
* **docs:** 迁移备份按宿主分目录、补 cursor/kimi 清理与 forge-quality，守卫钉动作行 (`3e7d7cc`)
* **docs:** 迁移节改 POSIX glob、补 windsurf/codex 标记块与守卫动作锚点 (`5fee11a`)
* **test:** worktree goal 期望值改从检出字节计算，兼容 core.autocrlf (`208c12b`)

## [1.85.2](https://github.com/MjxUpUp/Forge/compare/v1.85.1...v1.85.2) (2026-11-14)


### Bug Fixes

* **skills:** 内置库列举不落盘，真实路径需求改解压到系统临时目录 (`78ad00f`)

## [1.85.1](https://github.com/MjxUpUp/Forge/compare/v1.85.0...v1.85.1) (2026-11-14)


### Bug Fixes

* **docs:** 修正阶段 D 后 skillsdist 的现状描述 (`05e3c38`)

## [1.85.0](https://github.com/MjxUpUp/Forge/compare/v1.84.5...v1.85.0) (2026-11-13)


### Features

* **agents:** AGENTS.md 托管块由 forge 幂等同步（ADR-0002 phase A 余额） (`32bff65`)
* **agents:** AGENTS.md 托管块由 forge 幂等生成 (`7dcaac9`)
* **change:** priorAttempts 列表与单源多指针 (`cfa477b`)
* **change:** 仓库内 change 生命周期与可执行判据 (`c717599`)
* **change:** 证据闸门 `change check`（CI/pre-commit 共用） (`650e979`)
* **hooks:** 仓库契约决定 hook 是否运行 (`b981224`)
* **loop:** change status / gate / 耗尽即停 / fast 通道 (`92e61ab`)
* **loop:** 回环契约 phase A——status/gate/耗尽即停/fast 通道（ADR-0002） (`a7f6755`)
* **spec:** spec/forge.yaml 作为成员资格锚点，clone 即用 (`8b33167`)


### Bug Fixes

* **agents:** 分隔符统一一个空行、CRLF 保持、补边界测试 (`a162f4d`)
* **agents:** 多对标记拒绝猜、新建写标题、无契约拒绝同步 (`68196eb`)
* **agents:** 追加不改原始字节、托管块自证身份、文档口径对齐 (`fdd4d1e`)
* **change:** $ 成对消耗、算术不吃括号、&gt;( ) 也是进程替换 (`d9c1946`)
* **change:** check 按评审修 —— 代码改动不再绕过 pre-commit、JSON 恒合法 (`fbcb5d9`)
* **change:** pre-commit 在旧版 forge / 未用 spec-changes 的仓库放行 (`dc37f3c`)
* **change:** 修阶段 A 复评的 P1（超时语义、cursor frontmatter、变异漏洞） (`b287ce7`)
* **change:** 判据只允许"一条简单命令"，缩进围栏不再被静默丢弃 (`b425c97`)
* **change:** 判据检查改看"去引号之后的词"，引号包不住 internals (`e740308`)
* **change:** 判据检查真正拦住嵌套 shell、eval 与 set +o (`83c92de`)
* **change:** 包装命令与零工作量判据，收回文档里说过头的保证 (`0ec9d23`)
* **change:** 包装命令的参数、分组与占位判据，文档改成精确的覆盖清单 (`8688f80`)
* **change:** 双引号里的命令替换仍会执行，必须拒绝 (`a8cbed8`)
* **change:** 反斜杠续行会拼出 $( )，算术展开不是命令替换 (`86a70a5`)
* **change:** 只提交证据不再判失效，判据禁令改由代码执行 (`e7ed26b`)
* **change:** 围栏各用自己的壳；封印后新出现的文件也判失效 (`aca6a40`)
* **change:** 外层 $'...' 是字面量，算术里的 $'...' 先解码再判 (`c64966b`)
* **change:** 已勾选的人工项不再从报告里消失 (`b4ffe73`)
* **change:** 干净工作区留不下指纹时，HEAD 一动就判证据失效 (`d1e7630`)
* **change:** 干净验证后新写文件也判失效；判据检查按行识别注释 (`cde5b0f`)
* **change:** 引号跨行时不再重置，注释只到行尾 (`ce609ac`)
* **change:** 打断的 verify 不再算一份证据；verify 先报上次证据状态 (`e61d5d1`)
* **change:** 拒绝 env -S，并说明它的绕过面 (`4cb7807`)
* **change:** 指纹改在命令跑完后取，残缺摘要不再冒充封印 (`3d155a4`)
* **change:** 按文档二轮与代码复评修证据路径、cursor frontmatter 与 pointers 解析 (`2a952dd`)
* **change:** 旧版 forge 探针真的能探出来，JSON 形状与文案对齐 (`8143270`)
* **change:** 注释不参与引号匹配，堵掉注释里的落单引号 (`92e3663`)
* **change:** 被转义的空白之后的 # 不是注释 (`f5610ac`)
* **change:** 转义的 $ 不参与配对，算术里的反引号仍要抓 (`118472c`)
* **change:** 退化检查改用 note，skipped 只表示「什么都没查」 (`e9fa805`)
* **change:** 骨架门禁、管道失败与内容级漂移判定 (`8bb6ef8`)
* **cli:** change 命令归组并重钉 compat golden (`e0f1324`)
* **cli:** 契约仓库的仓内清理前移到接线闸门之前（P2-1），并修 --all/list 提示与文档口径 (`410e054`)
* **cli:** 已 disable 的契约仓库不再被 autoSync 当 legacy 接线回写 HOME (`dcee1e3`)
* **cli:** 收口阶段 C 评审——按宿主接线、disable 墓碑与文档诚实性 (`3ff64da`)
* **cli:** 注册表/看板/团队模式提示与注释对齐 Phase C，并补配对测试 (`e5b799f`)
* **hooks:** assertion-check 在 gate 模式静默 abort，task-implement 恒红 (`b19b7d8`)
* **hooks:** 契约不能关掉安全底线，缺省档位与全局对齐 (`2385105`)
* **hooks:** 阶段 B 评审修复——测试环境 opt-in 前提 + 执法宣传残留改 audit-only (`5490d2d`)
* **loop:** 任务正文去掉自己的编号，状态行不再打印「1. 1.」 (`4479103`)
* **loop:** 判据连词不再丢号，围栏与归档目录补齐 (`0a04e92`)
* **loop:** 勾选后的交接不再指向 gate，判据连写与围栏宽度都算清 (`b161f39`)
* **loop:** 归档建议用当天日期，去掉不会走到的分支 (`602b1dc`)
* **loop:** 按双评审修 blocked/handoff、判据解析与文档口径 (`f51eac2`)
* **membership:** symlink 用例夹具、契约恢复与 legacy 早退 (`17e4b9a`)
* **membership:** 契约只在仓库根生效，State 变纯读探针 (`477fb22`)
* **membership:** 成员资格判据统一走 projectroot.State (`89e172d`)
* **projectroot:** walk-up 边界同时包含账户 home，防测试重定向 HOME 后越界 (`a24b480`)
* **registry:** 拒绝/跳过保留根条目，修复 CI 上 temp 被解析成 home (`beb8c41`)
* **spec:** 收敛只读审查发现——主版本兼容、status --json 来源、and-list 收尾全量改写 (`6202750`)
* **taskpipeline:** 删除 hazard 清账遗留的未用常量（staticcheck U1000，CI canary） (`8fb7117`)
* 清掉阶段 D 遗留的宿主层引用（init 下一步、全局视图提示、废弃 freeze 包、--agents flag） (`069dd39`)
* 阶段 D 评审回件——verify 改查契约、advisory 不计证据、CI 去插件引用、--agents 兼容 no-op、文档按无宿主层重写 (`180824f`)

## [1.84.5](https://github.com/MjxUpUp/Forge/compare/v1.84.4...v1.84.5) (2026-10-25)


### Bug Fixes

* **hooks:** 汇总四项 hook 摩擦修复（hazard 误拦、read-before-edit 误拦、升级提示泄漏、hook 静默失败） ([#143](https://github.com/MjxUpUp/Forge/issues/143)) (`7c4d8de`)

## [1.84.4](https://github.com/MjxUpUp/Forge/compare/v1.84.3...v1.84.4) (2026-10-25)


### Bug Fixes

* **review:** 复审路径补「续用原评审者」分档（doc-review / Stop hook / 双 reviewer / 协议 A·B） ([#141](https://github.com/MjxUpUp/Forge/issues/141)) (`cca2a96`)

## [1.84.3](https://github.com/MjxUpUp/Forge/compare/v1.84.2...v1.84.3) (2026-10-24)


### Bug Fixes

* **task:** 错树守护 fail-closed —— 任务锚在另一棵树时禁止在主树写源码/过门禁 + human 档链定义指纹 ([#139](https://github.com/MjxUpUp/Forge/issues/139)) (`ded892c`)

## [1.84.2](https://github.com/MjxUpUp/Forge/compare/v1.84.1...v1.84.2) (2026-10-24)


### Bug Fixes

* **review:** Stop 门禁按会话作用域判定，不再因他会话的未审改动阻断 ([#137](https://github.com/MjxUpUp/Forge/issues/137)) (`ac12459`)

## [1.84.1](https://github.com/MjxUpUp/Forge/compare/v1.84.0...v1.84.1) (2026-10-22)


### Bug Fixes

* **escape:** 逃生舱审计与裁决 + 文档卫生批 ([#131](https://github.com/MjxUpUp/Forge/issues/131)) (`eeba704`)
* **scope:** 合并基线分支后改动集剔除只经合并带入的文件 ([#132](https://github.com/MjxUpUp/Forge/issues/132)) (`3bae866`)

## [1.84.0](https://github.com/MjxUpUp/Forge/compare/v1.83.0...v1.84.0) (2026-10-22)


### Features

* **eval:** 评测有效性前置——Track A preflight/leakage、扰动臂与 MDE、golden 分层与 position-swap ([#127](https://github.com/MjxUpUp/Forge/issues/127)) (`4373f27`)


### Bug Fixes

* **protocol:** 协议文本与代码对齐 + 阻断正当性单一真相表 ([#129](https://github.com/MjxUpUp/Forge/issues/129)) (`9c5cbfe`)

## [1.83.0](https://github.com/MjxUpUp/Forge/compare/v1.82.0...v1.83.0) (2026-10-21)


### Features

* **forge-dsh:** batch 单进程分派、静默回执过滤、降级提示、locale、技能库提议与版本对比 ([#125](https://github.com/MjxUpUp/Forge/issues/125)) (`b2c3a98`)

## [1.82.0](https://github.com/MjxUpUp/Forge/compare/v1.81.0...v1.82.0) (2026-10-21)


### Features

* **forge-dsh:** 首装链路——SessionStart 必达、内置 forge 本体、首轮欢迎语与发布后验证 ([#123](https://github.com/MjxUpUp/Forge/issues/123)) (`bf0cf8f`)

## [1.81.0](https://github.com/MjxUpUp/Forge/compare/v1.80.1...v1.81.0) (2026-10-21)


### ⚠ 行为变更（非 BREAKING，需知悉）

**任务接续改为按需拉取（#121）**

* 会话启动（SessionStart）只输出 `[forge-session]` 横幅一行：不再注入活跃任务的接续视图或未完成任务清单，也不再自动把当前 session 锚定到任务。需要接续时由用户发起：说「继续某某任务」时 agent 走 `forge task resume --match`，说「还有哪些任务没完成」时走 `forge task list --open`。
* 「待认领」推送下线：派给当前 agent 的任务不再在会话启动时推送。`forge task assign` / `claim` / `deliver` / `mine` 不变，`forge task resume` 接手派给自己的 offered 任务时仍会自动认领。任务状态里的 `notified_at` 字段停写，保留以兼容旧数据。
* kimi 首个 prompt 的冷启动接续补发一并下线。
* 新增 `forge task resume --match "<关键词>" [--workspace|--all-projects] [--json]`：只读模糊匹配未完成任务（容忍错拼、乱序词、缩写、中文片段），最多返回 10 个带得分的候选，不锚定、不认领。
* 新增 `forge task list --open [--workspace|--all-projects] [--json]`：列出未完成任务的标题、门禁进度、下一步、阻塞数、派活状态、最后活动时间。不加 `--open` 时 `task list` 输出不变。
* task-guard 放行某 session 的首次源码写入时，会静默把该 session 记入任务的参与记录并认领节点租约，不改门禁、不输出任何提示。
* 压缩后的重注入（PostCompact + resume-reinject，claude-code / codex）不变；没有 PostCompact 事件的宿主（zcode、cursor、windsurf 等）压缩后不再自动恢复接续视图，需要时手动 `forge task resume`。
* 1.80.0 所述「会话启动注入的接续文本多一行循环判词」自本版起不再适用（会话启动不再注入接续文本）；`resume` / `context` 与压缩后注入的接续文本仍有这一行。


### Features

* **resume:** 任务接续改为按需拉取——SessionStart 只留横幅，新增 resume --match / list --open ([#121](https://github.com/MjxUpUp/Forge/issues/121)) (`3e22f1b`)

## [1.80.1](https://github.com/MjxUpUp/Forge/compare/v1.80.0...v1.80.1) (2026-10-21)


### ⚠ 行为变更（非 BREAKING，需知悉）

**hazard-guard heredoc 判定（#116）**

* 解释器 heredoc（如 `python3 - <<'EOF'`）不再因为同一条命令后面 `go test -count=1` 这类参数被误判为 `python -c` 而遭拦截。
* 同时收紧了两种此前会被放行的写法：只剥离第一个 heredoc 的正文（以前会把后面所有行一起吞掉）；第一个 `<<` 必须属于解释器那一段。这类命令现在会被拦截。

（本节于发版后补写，原因同 1.80.0。）


### Bug Fixes

* **hazard-guard:** stop a later -count/-c from vetoing heredoc data release (16:51 false block) ([#116](https://github.com/MjxUpUp/Forge/issues/116)) (`5306f1b`)

## [1.80.0](https://github.com/MjxUpUp/Forge/compare/v1.79.2...v1.80.0) (2026-10-21)


### ⚠ 行为变更（非 BREAKING，需知悉）

**loop 契约 B1（#117）**

* `forge task verify-acceptance` 多两类输出：每检出一次验收同因复发（改了代码、失败指纹没变），打一行 `ADVISORY:`，同时落一条 `acceptance-recurrence` checklog 行，并自动记一条已证伪方案；末尾加一行 `loop: <判词>（<原因>）→ next: <命令>`。退出码不变。
* `forge task status --json` 顶层新增 `loop_verdict`（verdict / reasons / next），原有键及其顺序不变。
* `forge task resume` / `context` 以及会话启动、压缩后注入的接续文本多一行「循环判词」，新增【已证伪方案】节（在【已确认决策】之前），tl;dr 多一行「已证伪 N 条」。
* `forge task finding --reset-loop` 现在会同时清空所有验收的失败历史，同因复发窗口从头起算。
* 新增 `forge task decide --rejected`。`task decide` 的幂等判断加入了类别：先否决、再采纳同一方案，会记成两条记录。
* `protocol.yml` 新增可选的 `loop:` 段（recurrence_threshold / max_failed_verifies / max_changed_files / token_budget / max_verifies_without_progress），只影响 advisory 判词，不阻断任何门禁。

**Windows hook 稳定性（#114）**

* hook 子进程的 `TMPDIR` 未设置时，forge 会把它固定为系统临时目录；显式设置的 `TMPDIR` 不受影响。此前在 Windows Git Bash 下，如果 `/tmp` 解析失败，file-sentinel 会静默放行 Bash 写源码的操作，现在能正常隔离。
* hook 输出中的解释器启动告警（如 `bash.exe: warning: …`）不再计入判定结果、checklog 详情和去重指纹。
* hook 去重标记改为毫秒精度。升级后第一次运行时，旧的秒级标记按过期处理，最多多出一条审计行。

（本节于发版后补写：1.80.0 发版时 release PR 被 release-please 自动合并，未来得及附上。）


### Features

* **loop:** loop 契约 B1——验收同因复发判定、循环判词、预算段、死路记忆、Loop 规程 ([#117](https://github.com/MjxUpUp/Forge/issues/117)) (`dc07157`)


### Bug Fixes

* **hooks:** root-cause windows-latest flakes — MSYS /tmp noise, dedup clock floor, deadlock watchdog ([#114](https://github.com/MjxUpUp/Forge/issues/114)) (`2c7778f`)

## [1.79.2](https://github.com/MjxUpUp/Forge/compare/v1.79.1...v1.79.2) (2026-10-20)


### ⚠ 行为变更（非 BREAKING，需知悉）

* 回环耗尽在**首次检出时就拦** `forge task complete`。此前检出的那一次会被放行（标记只写了盘，判定用的仍是旧的内存状态），要到下一次才拦。（#112）
* `forge task finding --reset-loop --note` 现在会**重开回环预算**：open finding 从下一轮复核起重新计龄，得到一份完整的 `max_rounds` 预算（`max_rounds=1` 时重置后立即再次耗尽）。此前只清标记、不清轮龄，重置后下一次预检就会再次耗尽。TaskState 新增 `loop_reset_round` / `loop_reset_at` 两个键，只在本机有效。（#112）
* `forge task report` 的"自供申辩"计数现在会计入 `--reset-loop` 人工裁决注记。此前从未计入，所以历史任务重新生成的 report 可能多出这一项。（#113）


### Bug Fixes

* **loopedge:** 回环耗尽两处漏放——首次检出当场拦、--reset-loop 发放完整新预算（loop 契约 B0） ([#112](https://github.com/MjxUpUp/Forge/issues/112)) (`bc58648`)
* **report:** task report 统计 --reset-loop 人工裁决注记 ([#113](https://github.com/MjxUpUp/Forge/issues/113)) (`d590c95`)

## [1.79.1](https://github.com/MjxUpUp/Forge/compare/v1.79.0...v1.79.1) (2026-10-19)


### Bug Fixes

* **dsh:** surface a missing forge CLI and refresh market metadata ([#110](https://github.com/MjxUpUp/Forge/issues/110)) (`76786b2`)

## [1.79.0](https://github.com/MjxUpUp/Forge/compare/v1.78.0...v1.79.0) (2026-10-18)


### Features

* **eval:** 沙箱 oracle v0——变更级考卷（cheat 召回 6/7、held-out 逃逸 100%→0%） (`a08a7cf`)

## [1.78.0](https://github.com/MjxUpUp/Forge/compare/v1.77.1...v1.78.0) (2026-10-15)


### Features

* research-workflow 可信度升级 (`997aaaf`)
* 证据链防断——~/ 路径失效与决策证据缺来源告警 (`5ce66c6`)
* 调研文档 L1 lint 与产物按来源定型 (`a2471dd`)


### Bug Fixes

* research-workflow 评审修订（lint 契约与文档一致性） (`b930cd2`)
* run 引用收窄为数字 id，文档写明落行语义 (`e2a4b43`)
* survey lint 评审修订与变异体补断言 (`029a2e1`)
* 增量调研补走独立核验，同步 Phase 编号 (`d59aafe`)
* 清账计入调用会话、信封剥离只认行首 (`431e0f2`)
* 触发剥宿主信封、赋值误拦与清账作用域 (`ec2d335`)
* 证据链告警降噪（只查证据前缀、run 算来源） (`559b454`)

## [1.77.1](https://github.com/MjxUpUp/Forge/compare/v1.77.0...v1.77.1) (2026-10-15)


### Bug Fixes

* **hookdispatch:** hook batch 管道并发泵送，修复 payload/输出超 64KB 挂死 (`168d5cf`)

## [1.77.0](https://github.com/MjxUpUp/Forge/compare/v1.76.0...v1.77.0) (2026-10-13)


### Features

* **skills:** skills decide 新增 --warrant——accept 决策缺可核验锚点或 run 锚点回归时 advisory 告警 (`ccda457`)


### Bug Fixes

* **skills:** 审查跟进——强锚点必须做过有效 baseline 比对，比对做不成降为弱并说明原因 (`c625281`)
* **taskpipeline:** mutation 抽样加阳性对照——未变异包测试先绿，红则 baseline-red 不计分 (`ce8424b`)
* **taskpipeline:** 阳性对照审查跟进——工具链缺失单独报错、披露剔除数、零样本杀灭率 n/a (`da4ca48`)

## [1.76.0](https://github.com/MjxUpUp/Forge/compare/v1.75.0...v1.76.0) (2026-10-13)


### Features

* **skills:** 技术方案三档模板，design-audit 与 design-artifact-standards 收回核心 (`7d42168`)
* **taskpipeline:** 设计阶段硬化——方案 lint、事实核查、需求追溯、分档路由 (`864cc3c`)


### Bug Fixes

* **skills:** 设计阶段硬化后续——重新提取刷新追溯、插件同名遮蔽检测、README design_tiers (`9eac9f7`)
* **taskpipeline:** interface 块开括号后允许行尾注释与 CRLF (`8651077`)
* **taskpipeline:** interface 块识别——一行空 interface 不吞后续函数体，支持泛型 interface (`a719e89`)
* **taskpipeline:** 复审轮 Minor——接口方法算声明、requirements.txt 触发依赖档、审批快照取锁内引用 (`50575e4`)
* **taskpipeline:** 审查轮修复——方案核查限定仓库内、Run 行追溯不丢、手工 scope 优先 (`5979346`)
* **taskpipeline:** 重新提取只替换来自产物的追溯——手工补登的追溯保留 (`36501d3`)

## [1.75.0](https://github.com/MjxUpUp/Forge/compare/v1.74.1...v1.75.0) (2026-10-10)


### Features

* **review:** 分档复审——低风险修复续用首轮 reviewer 增量复核，高风险/终审新派 (`c1788f8`)


### Bug Fixes

* **forge-dsh:** session-start 测试从 spec.json 推导期望名册并等整组落盘，消除 PostCompact 竞态 (`ed2443f`)

## [1.74.1](https://github.com/MjxUpUp/Forge/compare/v1.74.0...v1.74.1) (2026-10-09)


### Bug Fixes

* **forge-dsh:** 消息 source 改用生产者自有 kind，适配 DSH 会话格式 V4 (`84ffb16`)

## [1.74.0](https://github.com/MjxUpUp/Forge/compare/v1.73.2...v1.74.0) (2026-09-30)


### Features

* **cli:** 验收跟进批——status 孤儿指引 + next 落后远端提示 + CHANGELOG 陈旧段清理 + 发版事故恢复 RUNBOOK (`ca6a861`)

## [1.73.2](https://github.com/MjxUpUp/Forge/compare/v1.73.1...v1.73.2) (2026-09-25)


### Bug Fixes

* **ci:** npm-verify 装回钉主源 registry，传播窗 15×60s 加长 (`d071dfb`)
* **ci:** 修复远端 release.yml——API blob 上传误用 -f 致内容为字面路径（-F 才读文件） (`e289aa5`)

## [1.73.1](https://github.com/MjxUpUp/Forge/compare/v1.73.0...v1.73.1) (2026-09-25)


### Bug Fixes

* **taskpipeline:** fuzz 非零退出分型——crasher 只认语料标记，瞬断重试一次 (`a0da419`)

## [1.73.0](https://github.com/MjxUpUp/Forge/compare/v1.72.0...v1.73.0) (2026-09-25)


### Features

* **dashboard:** 面板复发维度分布形态上板 (`4ebd1d7`)

## [1.72.0](https://github.com/MjxUpUp/Forge/compare/v1.71.1...v1.72.0) (2026-09-22)


### Features

* **cli:** health 复发低分维度带分数分布直方图 + 形态判读 ([#91](https://github.com/MjxUpUp/Forge/issues/91)) (`9fdd8ac`)


### Bug Fixes

* **cli:** hazard 清账出口状态感知——halt release 可清未停机悬账，事件被毁死锁解除 ([#90](https://github.com/MjxUpUp/Forge/issues/90)) (`c4eb445`)

## [1.71.1](https://github.com/MjxUpUp/Forge/compare/v1.71.0...v1.71.1) (2026-09-21)


### Bug Fixes

* **ci:** Nightly master-reminder 场景适配 L1 验收登记门——complete 前登记快验收+实跑；场景失败信息带命令输出 (`8bc6c4a`)

## [1.71.0](https://github.com/MjxUpUp/Forge/compare/v1.70.0...v1.71.0) (2026-09-20)


### Features

* **evalkit:** traps 扩容 3→14——cheat-scan 七模式族策展 11 例 + 类型枚举扩容 (`671ab7a`)


### Bug Fixes

* **evalkit:** 审查修复——E2E 断言解析式 14/14、traps Short 同步、枚举钉住七模式 (`4399ac3`)

## [1.70.0](https://github.com/MjxUpUp/Forge/compare/v1.69.0...v1.70.0) (2026-09-20)


### Features

* **evalkit:** L3 校准判官——断言族 golden×10 + golden harvest + review llm 判官臂 + exit 断言接管 (`581cfb4`)


### Bug Fixes

* **evalkit:** 审查修复——判官留档走 projectroot、时间戳纳秒防覆盖、平台口径注记 (`0040d82`)

## [1.69.0](https://github.com/MjxUpUp/Forge/compare/v1.68.0...v1.69.0) (2026-09-20)


### Features

* **skills:** 理解侧闭环——spec 样例对/约束三分类 + 原型确认可编译导出 (`9cd8db8`)


### Bug Fixes

* **skills:** 审查修复——spec 模板四反引号嵌套+accept 围栏形态对齐提取契约、plugin 镜像同步 (`fecff32`)

## [1.68.0](https://github.com/MjxUpUp/Forge/compare/v1.67.0...v1.68.0) (2026-09-20)


### Features

* **oracle-pipeline:** 断言判定分派——五型机械断言 + 逐断言证据行 + --assert/--accept-file CLI (`2d35895`)


### Bug Fixes

* **oracle-pipeline:** 独立审查轮修复——globstar 递归翻译/空 Run v1 边角/exit 域门/YAML 严格模式 (`f3bd534`)

## [1.67.0](https://github.com/MjxUpUp/Forge/compare/v1.66.1...v1.67.0) (2026-09-20)


### Features

* **delivery-hardening:** 墙价硬批次——取证驱动的十项复发面关闭 (`485f713`)
* **oracle-pipeline:** 阶段一——考卷层级制 + complete 登记门 + conventions 兜底 + 交付验收单 + chain-init (`3024c8e`)
* **oracle-pipeline:** 阶段三——heldout 池 + seeded-bug 红队演练 + test-diff 隔离审查 (`3818689`)
* **oracle-pipeline:** 阶段二——机器出题三件套 + 修复自证回归前置 (`85afc13`)


### Bug Fixes

* **redteam:** fixture 字面量的导出名占位符化——unused-gate 行级提取器把字符串里的 func TestXxx 当真声明（自举拦截，名字运行时拼回） (`6e63e91`)

## [1.66.1](https://github.com/MjxUpUp/Forge/compare/v1.66.0...v1.66.1) (2026-09-18)


### Bug Fixes

* **release:** optionalDependencies 平台钉随 release 火车自动 bump (`a05ca1d`)
* **release:** optionalDependencies 平台钉随 release 火车自动 bump (`06fdaac`)

## 1.66.0 (2026-09-18)


### Features

* **act:** 证据式 ack——forge act retro-done 落 dispositions，已回顾 nudge 退出面板告警 (`f3f84df`)
* **artifact-chain:** 产物链工作流化——L6 接线 + schema.yaml 分档门禁 + spec→acceptance 提取 (`43b9467`)
* **artifact-drill:** forge eval artifact-drill——产物链行为级演练沉淀 + 发版链路接线 (`963db17`)
* **bridge-verify:** H2a 静态级 dsh 插件检查器（owner 拍板提前启动 H2） (`ac2281a`)
* **cli,hooks:** W0 hook 瘦身三件套——档位/耗时预算门/死检查报告 (`12a7752`)
* **cli,hooks:** W0 hook 瘦身三件套——档位/耗时预算门/死检查报告 (`fa330b6`)
* **cli:** vNext P1 pull 侧引导——forge next 单命令 + task wild 申报 + 文案接线 (`5005f78`)
* **cli:** vNext P2 审计层——forge enforcement 报告+随机审计+双环/降格信号 (`649a314`)
* **conventions:** 依赖倒置契约化——CONVENTIONS §13 + R18 规则 + 17 skill 正文合规迁移 (`4b936be`)
* **conventions:** 后续三件套——lint 未跑门禁 + learn 纠正写回 + codex 写入时刻补全 (`c1f3e81`)
* **conventions:** 项目规范档案——扫描建档 + 会话/写入时刻注入 (`9fa18be`)
* **discipline-first:** P1 落地——checklog outcome 分类 + test-nudge 文件级跨档升级 (`122fe1c`)
* **discipline-first:** P2-P4 落地 + 守护审计 3P1/3P2 修复 (`81a8e10`)
* **doclint:** 可读性门禁二期 P0——D8 结论位置 + D1/D2 清单二期 + 评委偏差防线 (`0115a76`)
* **escape-hatch:** P0 修复——test-nudge 天花板审计行 + task-drift choke point (`db82842`)
* **escape-hatch:** P1 落地——dispatch 幂等守卫 + ratchet/Stop 有界阻断 + HITL/wild 补强 (`ae7f476`)
* **escape-hatch:** 门禁逃生口加固 P0+P1——审计不静默/choke point/幂等守卫/有界阻断 (`7403b2f`)
* **eval:** dead-checks v2 检查分类口径——blocking/advisory/gate/pipeline 四档判定 (`338ddb0`)
* **eval:** forge eval harness-audit——A-G 修复的可复算度量 + 门禁命令形态分类器（设计 B1/M） (`74d74cf`)
* **eval:** Forge 自评测体系 P0-P4 落地——forge eval 命令族 + internal/evalkit 双轨评测栈 (`bbfa97a`)
* **eval:** 爬坡项落地——两 trap 洞闭环 + 历史反哺 golden + judge-audit 首轮 + docker 首跑 (`9563843`)
* **eval:** 补齐设计量纲——golden 三门禁 12 例 + 接续演练 3 条 + Terminal-Bench 冻结 manifest 适配器 (`5c6db1a`)
* **eval:** 评测可见性与触发点接线——Pulse 看板事件 + status 健康行 + release-readiness R6 (`46fd466`)
* **extensibility:** hostcap 行为注册表守卫 + compat 第七面外部桥契约 + forge-dsh H1 (`b5ee64c`)
* **focus-b1:** git/PR 收口 v1——forge gate push + pre-push 钩子（方向 A P0） (`28fda45`)
* **focus-b1l2:** 设计族拆包 + 教科书瘦身 + 死机制清理（方向 A 内容线） (`96e0182`)
* **focus-b1:** OTel GenAI exporter——checklog→OTLP/JSON 导出器（方向 D1） (`5f65b8c`)
* **focus-b1:** 自报一致性门禁 + 监控分段落地（方向 B P0） (`46262db`)
* **focus-b2cde:** issue-tracker 镜像 + task watchdog + eval 升级；B3 商业化设计（方向 C/E/F） (`b967906`)
* **focus-b2:** held-out gap 门禁 + safe-halt 语义（方向 B） (`8444093`)
* **focus-d2:** 标准卡位补齐——AAT mapper + skills inventory + 合规对照（P1 D2，批次切分漏项） (`9826c58`)
* **harness:** T7 引导层——onboarding 状态机 + 触发点 + 防 nag (`e4c32c5`)
* **harness:** T9 传输换代——git remote push/pull + 首推出境 HITL (`e10d381`)
* **hooks:** C gate-cmd-form advisory hook + F.1 解释器 heredoc 数据上下文 + F.2a confirm 链式分离 + F.2b advisory（设计 B2 批） (`8f8ddbe`)
* **hooks:** hazard-guard 语义分词层——GuardFall 五类绕过补漏（W1 门禁可证化） (`cfe7835`)
* **hooks:** task-guard 执法谱系 v2——无视计数器+zcode 提升+测试锚定 (`565c2c5`)
* **leverage-p0:** 六杠杆点 P0 批次——L5 命令面冻结仪表 + L1 楔子演练 + L2 spec-as-gate v2 数据形状 (`3af1301`)
* **loopedge:** 审查回环回边——轮次预算/复发检测/exhausted 升级人工（G2 落地） (`e13e1b6`)
* **mechanism-p012:** P0 版本纪律 + P1-2/3 逃生舱升级与承诺表 (`e544c4a`)
* **mechanism-p012:** P1-1 compat 工件 + P2 指纹分流与生产者声明 (`f5c05e5`)
* **multi-task-concurrency:** T10 收尾——dispatcher 心跳接线 + 落地记录 + dogfood 门禁通过 (`7d6cb0f`)
* **observability:** T10 度量收尾——status 归属覆盖率行 + 并发矩阵总测 (`2adf07e`)
* **p1:** 可读性门禁二期 P1——双评落档 + Finding.Tag 聚合 + doc-lint 写时 hook (`f96ad46`)
* **p1:** 可读性门禁二期 P1——双评落档 + Finding.Tag 聚合 + doc-lint 写时 hook (`0f03938`)
* **policy:** Project Policy Layer P1——按项目退出/恢复接管（forge off/on） (`6d26e5a`)
* **policy:** Project Policy Layer P2-P4——默认 ask 翻转 + 全局通道感知 + 外来 harness 让位 + 注册表写锁 (`1c6230d`)
* **registry:** gc 回收孤儿项目数据目录+测试夹具隔离 (`1282cef`)
* **release-flow:** 发布流程硬化 P-A/P-B/P-C——update --apply/写入时刻 gofmt/dedup 窗口旋钮 (`fb9d371`)
* skills 依赖倒置 + code-review-gate 边界重划 + 工程原则增强 (`2906f9c`)
* **skills:** 工程原则增强——真·SOLID 五原则 + 契约完整性 + 测试规格档 (`95304ec`)
* **skills:** 拆分 code-review-gate——doc-review 建家 + phase-*.md 归位 design-artifact-standards (`c214cbe`)
* **skills:** 演化纪律三缺口落地——事前成功度量门 + deferred 承诺三要素 + 不混车审查项 (`01a46d9`)
* **skills:** 演化纪律三缺口落地——事前成功度量门 + deferred 承诺三要素 + 不混车审查项 (`461ef57`)
* **skills:** 零反向依赖迁移——R18 硬校验 + 集成知识出库 + forge 原生 skill 迁出 (`966f52b`)
* **skills:** 零反向依赖迁移——R18 硬校验 + 集成知识出库 + forge 原生 skill 迁出 (`6a235d1`)
* **skilltrigger:** A 通道重构——决策点加载/动作点内联 + 输出失败门；B next 推送化 (`c875b8a`)
* **specs:** T8 产物契约层——specs 文件产物 + 哈希引用 + attempts 回灌 (`1577585`)
* **T1/T4/T7:** 锁收口 + 状态完整性签名 + 产品语义修正 (`b15e3ef`)
* **T2:** 检测器黄金用例集 + 配对死条件修复 + untracked 源 + 批量 tracked (`1c292a7`)
* **T3:** checklog 轮转 janitor + stamps 清理——active 无界增长结构性收口 (`3ae37a4`)
* **T8a:** 清理批 A——卸载通道分流/断言结构化/protocol 校验/决策转义/安装盲区 (`8f1afb9`)
* **T8b:** 清理批 B——九项收尾：哨兵退出/死代码/session入口统一/跨仓依赖/白名单反转/git探测守卫/infra出口 (`631f3b7`)
* **T9:** 性能簇——更新负缓存+短超时/funnel 分桶去重复解析/MoveFile 流式/空会话隔离 (`d13228e`)
* **taskpipeline,skillsqa:** G.1 覆盖预告 + G.2 注释清理抑制 + D refs_critical/R19/步骤 0（设计 B2-3+B3 批） (`3811583`)
* **taskpipeline:** req-hygiene 接线 task-verify——补 63a9457 缺失的生产调用 (`d44a65e`)
* **taskpipeline:** unused-gate——internal/ 零引用 Go 导出升格 complete 硬前置 (`e422dc2`)
* **taskpipeline:** 需求卫生检查（req-hygiene advisory）——方向二第一子步 (`63a9457`)
* **task:** vNext P3 三段工件分层 + task-guard 缓冲窗口状态机 (`a5ff2d1`)


### Bug Fixes

* **artifact-chain:** 复核 P2 修正——审批作废双侧删除防 ScoreTask 回写复活 + fail-open 补审计行 + escape 全段跳过语义 (`f0d4bd5`)
* **artifact-chain:** 独立审查修正——P0 flags 未注册 + P1 审批误杀/路径穿越 (`fa50594`)
* **artifact-drill:** CI 守卫修正——release needs 链守卫纳入 drill 位 + raw string 字面 \n 改写 (`6b61df5`)
* **attribution:** code-review 回应——同口径读方补齐 + 探针覆盖全部 hook 写点 + hazard 去重加会话维度 (`c6db533`)
* **attribution:** dogfood 发现 [#5](https://github.com/MjxUpUp/Forge/issues/5)——hook 记账路径归一（绝对→repo 相对） (`e3bc37d`)
* **attribution:** E 完成即冻结归因 + F.3 hazard 事件去重（设计 B0 批） (`35b1cdb`)
* **attribution:** porcelain 测试期望改正斜杠字面量（windows CI 实证） ([#38](https://github.com/MjxUpUp/Forge/issues/38)) (`f325d78`)
* **attribution:** 空 session 不得被 sanitize 成占位符——runHazardLog 与 hook 主记录点直用入口归一值 (`9221bb8`)
* **bridge-verify:** 回检 86 分残留收口——多行 import/括号取用/README --json (`52ae4fe`)
* **canary:** S1011——吸收 main 新增 artifactdrill 用例的循环 append(main 无金丝雀门禁带入,合并后金丝雀抓住) (`47e0881`)
* **ci:** deadcode 金丝雀补牙——纯报告器 exit 0，须判 stdout 非空才 fail (`a986c50`)
* **ci:** npm-verify 加 registry 传播竞态退避——官方 API 核实后再重试 (`f8f3027`)
* **ci:** npm-verify 重试改无条件退避 8×45s——v1.56.6 实录证伪两处设计假设 (`06cab27`)
* **ci:** permissions 提为 npm-verify job 级（step 级非法键，actionlint 实证整个 workflow 解析失败）+ 封守卫盲区（TestReleaseWorkflow_NoStepLevelPermissions，突变验证） (`9005291`)
* **ci:** Release workflow test 前加 npm 版本可见步——TestNpmPlatformVersionsAligned 失败时一眼定位 (`eaa7518`)
* **ci:** Release workflow test 前加 npm 版本可见步——TestNpmPlatformVersionsAligned 失败时一眼定位 (`1526a7f`)
* **ci:** watchdog 节流测试跨小时边界稳定化 + pluginpack 多 pack marketplace 生成器 (`a1938a1`)
* **ci:** Windows 兼容——GlobalProfile 用 filepath.Join、FrictionProbeArm shim 平台形态、latency Windows skip、forgedata profile 测试补齐 (`28dfe5f`)
* **ci:** Windows 兼容三处——GlobalProfile 用 filepath.Join、FrictionProbeArm shim 平台形态、latency 测试 Windows skip；gofmt 收敛 (`4f981fe`)
* **ci:** Windows 跨平台修复——HITL 确认语义统一 + worktree 删除 CWD 锁防护 (`1093f5b`)
* **ci:** 分支 CI 抓出的两类跨平台断言——dsh 名册同步 + Windows 路径分隔符 (`c0e9caf`)
* **ci:** 注入断言先解 JSON 信封再归一——信封级 ToSlash 会把转义反斜杠变成双斜杠 (`cc7dfd6`)
* **ci:** 评审回应 NEEDS-FIX 两条 Medium——守卫钉住「无条件」属性与超时预算 (`33bee69`)
* **ci:** 评审回应三条 Minor——SIGPIPE 安全 grep/守卫锚防掩盖分支/版本双判据 (`fead8cf`)
* **cli:** docs lint 帮助逐规则枚举（D5/D6/D7 独立可见，配对测试可断言） (`fe6bb19`)
* **cli:** enforcement 审查收口——全量 checklog/降格降噪/空会话不 join/错误告警 (`903937e`)
* **cli:** next 决策表对齐真实门禁链（P1 审查 FAIL 修正） (`1249dc2`)
* **clitask:** C.3 stderr 兜底——stdout 非 TTY 时 BLOCKED/next 行镜像 stderr (`3d703a7`)
* **clitask:** C.3 评审回应——镜像判定抽纯函数 + TTY/silent 漏测补齐 + 基线文档 Critical 修复 (`259ef08`)
* **cli:** 版本守卫兼容 patch 发版——previousVersion 修 Sscanf 双分支只认 .0 结尾 (`ff05b09`)
* **compat:** EscapeEnvs 补 FORGE_REQ_HYGIENE——req-hygiene 逃生舱须同步 roster（快照面 3 守卫） (`5586af1`)
* **dashboard:** 告警分级——alerts 只数可行动 nudge，降档噪音退出人面通道 (`ceee1ba`)
* **dead-code-sweep:** pluginpack README 守卫断言随 suggest 出清更新（forge off --commit 替位） (`9503796`)
* **dead-code-sweep:** 审查修复——marketplace 条目恢复 + 残留引用出清 + 承诺表裁决记录 (`fb8b53a`)
* **discipline-first:** compat 快照重钉（+3 命令/+2 检查，非破坏新增）+ skillRefAllowlist 收录 selfcheck 条目名 (`82f3756`)
* **discipline-first:** 同步清单——allCheckNames roster + README 命令表 + 命令归组 quality (`b51f601`)
* **discipline-first:** 增量复审 P3×3 收口——折叠计数守卫（PASS 穿插/&gt;8 前驱截断假折叠）+ 逗号编码守卫 + nextCmdVerifyAcceptance 常量单源 (`cd8501f`)
* **discipline-first:** 复审 P2×2/P3×7 修复——折叠全量集合比对/next-hint 事实分支/注释如实化/围栏对齐 (`ff2f95f`)
* **discipline-first:** 审查 P1/P2/P3 修复——任务边界重置/门禁事实锚定/最新文件点名 (`1ed30c8`)
* **doc-review:** round-2 两条 Minor 收尾——L41 枚举补结论位置、L47 压成指针句消双维护（93/100 PASS 遗留） (`c26049a`)
* **doc-review:** 独立评审 round-1 六条发现修复（89/100 PASS → 未决项清零） (`6794a0c`)
* **dogfood:** 实测三项修复——worktree ref 派生 / 归档陈旧快照 / 绑定残留 (`75f0028`)
* **env:** 环境性测试失败类根治——保留根守卫 + 测试密闭默认化 + registry remove 具名出口 (`8f8534e`)
* **eval:** B1 双轨评审回应——基线自证 + 分类器绕过面收窄 + E1/F2/A3 口径与设计对齐 (`5fd2642`)
* **eval:** golden e2e 断言跨平台化（真正落地——上一轮 replace 静默未生效被 CI 抓回） (`4549711`)
* **eval:** Windows CI 第二轮——golden 平台跳过机制 + e2e 断言跨平台化 + 词汇表补形态 (`6a324b4`)
* **eval:** Windows 三平台兼容——假二进制双形态 + 权限检查 GOOS 感知 + 测试去 sh 化 (`f829508`)
* **eval:** 两项审查遗留修复——混合 manifest 沙箱标签任务级粒度 + {dataDir} 硬报错 (`ccd58ad`)
* **eval:** 复审残留清理 + review 盖章（对抗审查闭环） (`b8c229c`)
* **eval:** 对抗性审查修复——C1-C3 全修 + I1-I8 全修 + Minor 挑修 (`94bec29`)
* **eval:** 确认轮两小项——loader_warnings 守卫反转为真断言 + E1 泄漏拆无 session/他会话两字段 (`057ec20`)
* **focus-b1l2:** skill-decisions 门禁 pack 树适配 + L2 回检发现修正 (`f285d26`)
* **focus-b1:** 命令树补全——gate hooks install 子命令化 + mirror 挂 github 子命令 (`5c278c2`)
* **focus-b1:** 复审轮修复——mirror 持久化真修 + heldout complete 路径留痕 + 覆盖缺口 (`714df36`)
* **focus-b1:** 对抗审查修复——blocker 全清 + should-fix 4 项 + notes 3 项 (`f04ee36`)
* **focus-d2:** 审查修复——exit 2 契约 + 复合键 + 根解析 + rune 截断 + 文档诚实化 (`1061315`)
* **golden:** artifact-chain 用例 Windows 可移植——{forge} 双引号包裹防 sh 反斜杠转义 (`8e28c24`)
* **harness:** dogfood 发现——gitignore 改根级允许清单（只跟踪 projects/） (`66ddf01`)
* **hazard:** halt release 文案语病——解锁前提表述修正 (`bd8fba9`)
* **hookdispatch:** doc-lint 登记 isInProcessHook 名册——修复 v1.61.0 装机 unknown hook (`a64cb6c`)
* **hookdispatch:** doc-lint 登记 isInProcessHook——修复装机 unknown hook（验收实锤） (`880e1ad`)
* **hooks:** B2-2 双轨评审回应——hook 分派接线 + F.1 裸 tag 主路径 + F.2a hook 层执法 + recall 补全 (`c937344`)
* **hooks:** P0 审查收口——dsh-only 陈旧注释/文档同步+不变量补钉 (`1220645`)
* **make:** premerge 交叉编译步加 CGO_ENABLED=0 (`893d588`)
* **mechanism-p012:** 复审轮修复——AllCheckNames 守卫去虚设 + schema 种子真填满 + 文档诚实注记 (`d927f45`)
* **mechanism-p012:** 对抗审查修复——blocker + 5 项 should-fix (`e24aa54`)
* **npm-guard:** 版本守卫放行发布窗口单版本滞后（v1.51.0 发版实证）+ 提交态对齐 1.51.0 (`02f41d0`)
* **npm:** optionalDependencies 平台包钉随 1.56.0 对齐——release-please 只 bump 主版本号不 bump 钉 (`5b9598c`)
* **npm:** optionalDependencies 平台包钉随 1.56.1 对齐（release-please 不 bump 钉的系统性缺口——测试放宽与 extra-files 补钉入 B4 批） (`cad038c`)
* **npm:** pins 对齐 1.56.3（release-please 不 bump 钉的根治方案——B4 批加 extra-files 更新器） (`ebeaa70`)
* **p1:** 代码审查三条修复——SecondScore 去 omitempty（合法 0 分不丢）、co-score 越界显式拒绝、tag: 后空内容 critical 拒绝（含测试） (`c47f0c1`)
* **policy:** forge policy state 对 managed 打印字面值——StatusManaged 空串的显示层映射 (`4ff41d0`)
* **policy:** plugin README 退出文案改走生成器单一真相源——资产源 + 新二进制重生成 (`5068806`)
* **premerge:** 预检修复——接续 fixture 对齐解析链 v2 + README 补新命令 + gofmt (`b445396`)
* **registry:** gc dry-run 汇总报计划处置数+Keys godoc 英文首句 (`c137ca4`)
* **registry:** 写锁同进程互斥 + 竞争退化可观测——CI 实证 24 并发 Add 丢条目根因 (`04321eb`)
* **release-flow:** L2 F1 接线修复——apply 生产路径改 npmInstallArgs 直构（argv+semver 消费点复验）+ Windows --plugin 契约 + spec 复审变更记录 (`b984512`)
* **release-flow:** 复审 P2+P3×8 收口——去重窗口真钳制/apply argv 直构+semver 消费点复验/自验精确比较/安装超时/Windows plugin 契约/gofmt 块短路/env 恢复/注释同步 (`a17cd5a`)
* **release:** pins 对齐步改推分支+开 PR（main 分支保护拒直推）+ 空/无 tag critical 文案分流 + ci 守卫封 step 级 permissions 盲区 (`f3dae10`)
* **release:** 发布链三项验收修复——npm-verify 显式平台同装、发布后 pins 自动对齐、空 critical 文案分流 (`b85a0a0`)
* **review-b1:** 两轮审查 B1 快速修复 28 项——静默降级补告警/Windows 平台修复/检测器小补 (`5483e20`)
* **review-b2-followup:** 修复 B2 引入的 e2e 回归——可执行脚本副本不可用 rename 原子写 (`32314b4`)
* **review-b2:** 数据丢失/原子写批次——用户资产全部改走 AtomicWrite + 读失败误判修复 (`b4ccd0a`)
* **review-b3:** 锁与并发批次——盖章路径入锁/串号覆盖中止/级联护史/锁身份自愈 (`506bc12`)
* **review-b4:** 信任与安全批次——--untrusted 剥离补洞/注入面封堵/发版脚本加固 (`158ba63`)
* **review-b5-followup:** srclint 白名单补录注释体 catch 正则 (`70fc13a`)
* **review-b5/b6:** 检测器批次 + 产品语义——消 contest/ 逃逸区/补 Go 族检测/init 自伤修复 (`e4f45b0`)
* **review:** A1 补齐收尾——孤儿 gitPorcelain 删除 + 现场过滤测试随领域迁 taskpipeline (`85e78c5`)
* **review:** clitask 迁移双轨审查收口——接缝兜底与注释/死链出清 (`f351094`)
* **review:** hookdispatch 双轨审查 3 建议收口 (`bc6da60`)
* **review:** tasktypes 下沉审查收口——3 项装饰项 (`a1ec456`)
* **review:** 代码审查 1C3M 修复——锁 pack 行重钉（verify exit 0）、看起来正常补测试、帮助断言 bullet 级+Short、v2 状态行推进 (`8c0cac9`)
* **review:** 任务下沉审查 5 项收口 (`eae19f4`)
* **review:** 双轨审查收口——注释残留出清与死常量裁决 (`f367d4f`)
* **review:** 复审残留——priorAttempts 真实接线进 HANDOFF + symlink 主检出判定 (`6bebee0`)
* **review:** 审查修复——B1 共享会话误标外来 / B2 finish 合并守卫 / H 台账 TTL / M1 M2 LOW 六项 (`b9fabd4`)
* **review:** 审查轮 4 项修复——逃生舱错位/词表真相源/无效测试/CI 诊断 (`d38838c`)
* **review:** 清扫任务双轨审查收口——4 项 LOW 全解 (`948d473`)
* **review:** 迁移审查收口——Version seam 改惰性闭包（阻断项） (`e56b4a0`)
* **skills:** decisions.md 补入 pack 镜像 + compat 快照重钉 (`563d0b5`)
* **skills:** R18 修复——5.5 步去 forge 化，retro-done 命令移至 Directive 生成面 (`dfa28d5`)
* **skills:** review 修复——notes 入库（C1）+ 悬空指针清零（C2）+ 漏迁条件块迁出（C3） (`4f01b25`)
* **skilltrigger:** B2-1 双轨评审回应——抑制计数接线/anyInline 聚合/编译器失败签名/B1 占位符口径等 (`0d64789`)
* **skilltrigger:** 确认轮 9 条新发现——anyInline 载荷侧/go 编译签名/词边界/夹具补齐 (`4d61a30`)
* **skilltrigger:** 终审轮 3 条——next_line_test 注释如实（complete 面未覆盖）、decisions.md 误抄两树修正、中文失败签名恢复全词（测试失败|编译失败） (`d1bd632`)
* **task:** dogfood [#6](https://github.com/MjxUpUp/Forge/issues/6)——--branch 共享 ref 派生 + 两项使用偏离引导 (`690be1a`)
* **task:** P3 审查收口——锁内变更/校验前置/wild 清计数/ID 基准/启发式范围 (`717e53d`)
* **taskpipeline:** task-verify 增零验收 advisory——重任务证据自述化的开工点拦截 (`5290057`)
* **test:** next-line 测试夹具补 git identity——CI 无全局 git config 时 commit exit 128 (`896ac0b`)
* **test:** runHookCapture 入口 cwd 自愈——前序测试删除 chdir 目录时 getwd 失败贯穿全组（CI -race 实证） (`8ec0fbf`)
* **test:** 三包 TestMain 隔离 FORGE_DATA_HOME——根治孤儿目录泄写 (`ee03257`)
* **test:** 合并 main 的语义消解——TestMain 单点化 + 管道捕获并发排水利族修复 (`392147f`)
* **test:** 审查回应——TestMain 显式清理替换 defer 死代码 (`1b06575`)
* **test:** 版本对齐守卫接受 release-please 瞬态——钉在当前或前一版且平台包对齐主包即合法（1.56.0/1.56.1/1.56.2 三轮发版实证的系统性缺口：release-please 不 bump optionalDependencies 钉） (`f5eda03`)
* **test:** 非 forge 场景 helper 入口 cwd 重锚——前序测试删除 chdir 目录后 getwd ENOENT 贯穿（CI 实证 hook_test.go:493/527） (`83d55ff`)
* **worktree:** [#4](https://github.com/MjxUpUp/Forge/issues/4) 二次修订——finish 真接 ClearByID + abort 全量清扫绑定 (`1b5c06e`)
* 双独立审查（代码双轨 + 文档四维）14 项发现全闭环 (`81b7725`)
* 死代码三重放行执法补强——req-hygiene 接线 / canary 补牙 / unused-gate / 存量清扫 (`600f052`)

## 1.65.0 (2026-09-18)


### Features

* **escape-hatch:** P0 修复——test-nudge 天花板审计行 + task-drift choke point (`db82842`)
* **escape-hatch:** P1 落地——dispatch 幂等守卫 + ratchet/Stop 有界阻断 + HITL/wild 补强 (`ae7f476`)
* **escape-hatch:** 门禁逃生口加固 P0+P1——审计不静默/choke point/幂等守卫/有界阻断 (`7403b2f`)

## [1.64.0](https://github.com/MjxUpUp/Forge/compare/v1.63.0...v1.64.0) (2026-09-18)


### Features

* **release-flow:** 发布流程硬化 P-A/P-B/P-C——update --apply/写入时刻 gofmt/dedup 窗口旋钮 (`fb9d371`)


### Bug Fixes

* **release-flow:** L2 F1 接线修复——apply 生产路径改 npmInstallArgs 直构（argv+semver 消费点复验）+ Windows --plugin 契约 + spec 复审变更记录 (`b984512`)
* **release-flow:** 复审 P2+P3×8 收口——去重窗口真钳制/apply argv 直构+semver 消费点复验/自验精确比较/安装超时/Windows plugin 契约/gofmt 块短路/env 恢复/注释同步 (`a17cd5a`)

## [1.63.0](https://github.com/MjxUpUp/Forge/compare/v1.62.0...v1.63.0) (2026-09-18)


### Features

* **discipline-first:** P1 落地——checklog outcome 分类 + test-nudge 文件级跨档升级 (`122fe1c`)
* **discipline-first:** P2-P4 落地 + 守护审计 3P1/3P2 修复 (`81a8e10`)


### Bug Fixes

* **discipline-first:** compat 快照重钉（+3 命令/+2 检查，非破坏新增）+ skillRefAllowlist 收录 selfcheck 条目名 (`82f3756`)
* **discipline-first:** 同步清单——allCheckNames roster + README 命令表 + 命令归组 quality (`b51f601`)
* **discipline-first:** 增量复审 P3×3 收口——折叠计数守卫（PASS 穿插/&gt;8 前驱截断假折叠）+ 逗号编码守卫 + nextCmdVerifyAcceptance 常量单源 (`cd8501f`)
* **discipline-first:** 复审 P2×2/P3×7 修复——折叠全量集合比对/next-hint 事实分支/注释如实化/围栏对齐 (`ff2f95f`)
* **discipline-first:** 审查 P1/P2/P3 修复——任务边界重置/门禁事实锚定/最新文件点名 (`1ed30c8`)

## [1.62.0](https://github.com/MjxUpUp/Forge/compare/v1.61.3...v1.62.0) (2026-09-16)


### Features

* **bridge-verify:** H2a 静态级 dsh 插件检查器（owner 拍板提前启动 H2） (`ac2281a`)
* **extensibility:** hostcap 行为注册表守卫 + compat 第七面外部桥契约 + forge-dsh H1 (`b5ee64c`)


### Bug Fixes

* **bridge-verify:** 回检 86 分残留收口——多行 import/括号取用/README --json (`52ae4fe`)

## [1.61.3](https://github.com/MjxUpUp/Forge/compare/v1.61.2...v1.61.3) (2026-09-16)


### Bug Fixes

* **release:** pins 对齐步改推分支+开 PR（main 分支保护拒直推）+ 空/无 tag critical 文案分流 + ci 守卫封 step 级 permissions 盲区 (`f3dae10`)

## [1.61.2](https://github.com/MjxUpUp/Forge/compare/v1.61.1...v1.61.2) (2026-09-16)


### Bug Fixes

* **ci:** permissions 提为 npm-verify job 级（step 级非法键，actionlint 实证整个 workflow 解析失败）+ 封守卫盲区（TestReleaseWorkflow_NoStepLevelPermissions，突变验证） (`9005291`)
* **release:** 发布链三项验收修复——npm-verify 显式平台同装、发布后 pins 自动对齐、空 critical 文案分流 (`b85a0a0`)

## [1.61.1](https://github.com/MjxUpUp/Forge/compare/v1.61.0...v1.61.1) (2026-09-15)


### Bug Fixes

* **hookdispatch:** doc-lint 登记 isInProcessHook 名册——修复 v1.61.0 装机 unknown hook (`a64cb6c`)
* **hookdispatch:** doc-lint 登记 isInProcessHook——修复装机 unknown hook（验收实锤） (`880e1ad`)

## [1.61.0](https://github.com/MjxUpUp/Forge/compare/v1.60.0...v1.61.0) (2026-09-15)


### Features

* **doclint:** 可读性门禁二期 P0——D8 结论位置 + D1/D2 清单二期 + 评委偏差防线 (`0115a76`)
* **p1:** 可读性门禁二期 P1——双评落档 + Finding.Tag 聚合 + doc-lint 写时 hook (`f96ad46`)
* **p1:** 可读性门禁二期 P1——双评落档 + Finding.Tag 聚合 + doc-lint 写时 hook (`0f03938`)


### Bug Fixes

* **cli:** docs lint 帮助逐规则枚举（D5/D6/D7 独立可见，配对测试可断言） (`fe6bb19`)
* **doc-review:** round-2 两条 Minor 收尾——L41 枚举补结论位置、L47 压成指针句消双维护（93/100 PASS 遗留） (`c26049a`)
* **doc-review:** 独立评审 round-1 六条发现修复（89/100 PASS → 未决项清零） (`6794a0c`)
* **p1:** 代码审查三条修复——SecondScore 去 omitempty（合法 0 分不丢）、co-score 越界显式拒绝、tag: 后空内容 critical 拒绝（含测试） (`c47f0c1`)
* **review:** 代码审查 1C3M 修复——锁 pack 行重钉（verify exit 0）、看起来正常补测试、帮助断言 bullet 级+Short、v2 状态行推进 (`8c0cac9`)

## [1.60.0](https://github.com/MjxUpUp/Forge/compare/v1.59.1...v1.60.0) (2026-09-15)


### Features

* **taskpipeline:** req-hygiene 接线 task-verify——补 63a9457 缺失的生产调用 (`d44a65e`)
* **taskpipeline:** unused-gate——internal/ 零引用 Go 导出升格 complete 硬前置 (`e422dc2`)


### Bug Fixes

* **ci:** deadcode 金丝雀补牙——纯报告器 exit 0，须判 stdout 非空才 fail (`a986c50`)
* **review:** 审查轮 4 项修复——逃生舱错位/词表真相源/无效测试/CI 诊断 (`d38838c`)
* 死代码三重放行执法补强——req-hygiene 接线 / canary 补牙 / unused-gate / 存量清扫 (`600f052`)

## [1.59.1](https://github.com/MjxUpUp/Forge/compare/v1.59.0...v1.59.1) (2026-09-14)


### Bug Fixes

* **compat:** EscapeEnvs 补 FORGE_REQ_HYGIENE——req-hygiene 逃生舱须同步 roster（快照面 3 守卫） (`5586af1`)

## [1.59.0](https://github.com/MjxUpUp/Forge/compare/v1.58.1...v1.59.0) (2026-09-14)


### Features

* **taskpipeline:** 需求卫生检查（req-hygiene advisory）——方向二第一子步 (`63a9457`)

## [1.58.1](https://github.com/MjxUpUp/Forge/compare/v1.58.0...v1.58.1) (2026-09-14)


### Bug Fixes

* **ci:** Release workflow test 前加 npm 版本可见步——TestNpmPlatformVersionsAligned 失败时一眼定位 (`eaa7518`)
* **ci:** Release workflow test 前加 npm 版本可见步——TestNpmPlatformVersionsAligned 失败时一眼定位 (`1526a7f`)

## [1.58.0](https://github.com/MjxUpUp/Forge/compare/v1.57.1...v1.58.0) (2026-09-14)


### Features

* **eval:** dead-checks v2 检查分类口径——blocking/advisory/gate/pipeline 四档判定 (`338ddb0`)


### Bug Fixes

* **ci:** Windows 兼容——GlobalProfile 用 filepath.Join、FrictionProbeArm shim 平台形态、latency Windows skip、forgedata profile 测试补齐 (`28dfe5f`)
* **ci:** Windows 兼容三处——GlobalProfile 用 filepath.Join、FrictionProbeArm shim 平台形态、latency 测试 Windows skip；gofmt 收敛 (`4f981fe`)

## [1.57.1](https://github.com/MjxUpUp/Forge/compare/v1.57.0...v1.57.1) (2026-09-13)


### Bug Fixes

* **test:** 非 forge 场景 helper 入口 cwd 重锚——前序测试删除 chdir 目录后 getwd ENOENT 贯穿（CI 实证 hook_test.go:493/527） (`83d55ff`)

## [1.57.0](https://github.com/MjxUpUp/Forge/compare/v1.56.7...v1.57.0) (2026-09-13)


### Features

* **cli,hooks:** W0 hook 瘦身三件套——档位/耗时预算门/死检查报告 (`12a7752`)
* **cli,hooks:** W0 hook 瘦身三件套——档位/耗时预算门/死检查报告 (`fa330b6`)
* **hooks:** hazard-guard 语义分词层——GuardFall 五类绕过补漏（W1 门禁可证化） (`cfe7835`)

## [1.56.7](https://github.com/MjxUpUp/Forge/compare/v1.56.6...v1.56.7) (2026-09-11)


### Bug Fixes

* **ci:** npm-verify 重试改无条件退避 8×45s——v1.56.6 实录证伪两处设计假设 (`06cab27`)
* **ci:** 评审回应 NEEDS-FIX 两条 Medium——守卫钉住「无条件」属性与超时预算 (`33bee69`)

## [1.56.6](https://github.com/MjxUpUp/Forge/compare/v1.56.5...v1.56.6) (2026-09-11)


### Bug Fixes

* **ci:** npm-verify 加 registry 传播竞态退避——官方 API 核实后再重试 (`f8f3027`)
* **ci:** 评审回应三条 Minor——SIGPIPE 安全 grep/守卫锚防掩盖分支/版本双判据 (`fead8cf`)

## [1.56.5](https://github.com/MjxUpUp/Forge/compare/v1.56.4...v1.56.5) (2026-09-11)


### Bug Fixes

* **clitask:** C.3 stderr 兜底——stdout 非 TTY 时 BLOCKED/next 行镜像 stderr (`3d703a7`)
* **clitask:** C.3 评审回应——镜像判定抽纯函数 + TTY/silent 漏测补齐 + 基线文档 Critical 修复 (`259ef08`)

## [1.56.4](https://github.com/MjxUpUp/Forge/compare/v1.56.3...v1.56.4) (2026-09-11)


### Bug Fixes

* **npm:** pins 对齐 1.56.3（release-please 不 bump 钉的根治方案——B4 批加 extra-files 更新器） (`ebeaa70`)

## [1.56.3](https://github.com/MjxUpUp/Forge/compare/v1.56.2...v1.56.3) (2026-09-11)


### Bug Fixes

* **test:** 版本对齐守卫接受 release-please 瞬态——钉在当前或前一版且平台包对齐主包即合法（1.56.0/1.56.1/1.56.2 三轮发版实证的系统性缺口：release-please 不 bump optionalDependencies 钉） (`f5eda03`)

## [1.56.2](https://github.com/MjxUpUp/Forge/compare/v1.56.1...v1.56.2) (2026-09-11)


### Bug Fixes

* **npm:** optionalDependencies 平台包钉随 1.56.1 对齐（release-please 不 bump 钉的系统性缺口——测试放宽与 extra-files 补钉入 B4 批） (`cad038c`)

## [1.56.1](https://github.com/MjxUpUp/Forge/compare/v1.56.0...v1.56.1) (2026-09-11)


### Bug Fixes

* **npm:** optionalDependencies 平台包钉随 1.56.0 对齐——release-please 只 bump 主版本号不 bump 钉 (`5b9598c`)

## [1.56.0](https://github.com/MjxUpUp/Forge/compare/v1.55.1...v1.56.0) (2026-09-11)


### Features

* **eval:** forge eval harness-audit——A-G 修复的可复算度量 + 门禁命令形态分类器（设计 B1/M） (`74d74cf`)
* **hooks:** C gate-cmd-form advisory hook + F.1 解释器 heredoc 数据上下文 + F.2a confirm 链式分离 + F.2b advisory（设计 B2 批） (`8f8ddbe`)
* **skills:** 演化纪律三缺口落地——事前成功度量门 + deferred 承诺三要素 + 不混车审查项 (`01a46d9`)
* **skills:** 演化纪律三缺口落地——事前成功度量门 + deferred 承诺三要素 + 不混车审查项 (`461ef57`)
* **skilltrigger:** A 通道重构——决策点加载/动作点内联 + 输出失败门；B next 推送化 (`c875b8a`)
* **taskpipeline,skillsqa:** G.1 覆盖预告 + G.2 注释清理抑制 + D refs_critical/R19/步骤 0（设计 B2-3+B3 批） (`3811583`)


### Bug Fixes

* **attribution:** code-review 回应——同口径读方补齐 + 探针覆盖全部 hook 写点 + hazard 去重加会话维度 (`c6db533`)
* **attribution:** E 完成即冻结归因 + F.3 hazard 事件去重（设计 B0 批） (`35b1cdb`)
* **attribution:** 空 session 不得被 sanitize 成占位符——runHazardLog 与 hook 主记录点直用入口归一值 (`9221bb8`)
* **eval:** B1 双轨评审回应——基线自证 + 分类器绕过面收窄 + E1/F2/A3 口径与设计对齐 (`5fd2642`)
* **eval:** 确认轮两小项——loader_warnings 守卫反转为真断言 + E1 泄漏拆无 session/他会话两字段 (`057ec20`)
* **hooks:** B2-2 双轨评审回应——hook 分派接线 + F.1 裸 tag 主路径 + F.2a hook 层执法 + recall 补全 (`c937344`)
* **skills:** decisions.md 补入 pack 镜像 + compat 快照重钉 (`563d0b5`)
* **skilltrigger:** B2-1 双轨评审回应——抑制计数接线/anyInline 聚合/编译器失败签名/B1 占位符口径等 (`0d64789`)
* **skilltrigger:** 确认轮 9 条新发现——anyInline 载荷侧/go 编译签名/词边界/夹具补齐 (`4d61a30`)
* **skilltrigger:** 终审轮 3 条——next_line_test 注释如实（complete 面未覆盖）、decisions.md 误抄两树修正、中文失败签名恢复全词（测试失败|编译失败） (`d1bd632`)
* **test:** next-line 测试夹具补 git identity——CI 无全局 git config 时 commit exit 128 (`896ac0b`)
* **test:** 三包 TestMain 隔离 FORGE_DATA_HOME——根治孤儿目录泄写 (`ee03257`)
* **test:** 审查回应——TestMain 显式清理替换 defer 死代码 (`1b06575`)

## [1.55.1](https://github.com/MjxUpUp/Forge/compare/v1.55.0...v1.55.1) (2026-09-09)


### Bug Fixes

* **taskpipeline:** task-verify 增零验收 advisory——重任务证据自述化的开工点拦截 (`5290057`)

## [1.55.0](https://github.com/MjxUpUp/Forge/compare/v1.54.0...v1.55.0) (2026-09-09)


### Features

* **act:** 证据式 ack——forge act retro-done 落 dispositions，已回顾 nudge 退出面板告警 (`f3f84df`)


### Bug Fixes

* **dashboard:** 告警分级——alerts 只数可行动 nudge，降档噪音退出人面通道 (`ceee1ba`)
* **skills:** R18 修复——5.5 步去 forge 化，retro-done 命令移至 Directive 生成面 (`dfa28d5`)

## [1.54.0](https://github.com/MjxUpUp/Forge/compare/v1.53.0...v1.54.0) (2026-09-08)


### Features

* **loopedge:** 审查回环回边——轮次预算/复发检测/exhausted 升级人工（G2 落地） (`e13e1b6`)


### Bug Fixes

* **canary:** S1011——吸收 main 新增 artifactdrill 用例的循环 append(main 无金丝雀门禁带入,合并后金丝雀抓住) (`47e0881`)
* **golden:** artifact-chain 用例 Windows 可移植——{forge} 双引号包裹防 sh 反斜杠转义 (`8e28c24`)
* **test:** 合并 main 的语义消解——TestMain 单点化 + 管道捕获并发排水利族修复 (`392147f`)
* **env:** 环境性测试失败类根治——保留根守卫（registry.Add 拒绝注册 home/系统临时根）+ 测试密闭默认化（hookdispatch TestMain 隔离）+ `forge registry remove` 具名出口 (`8f8534e`)

## [1.53.0](https://github.com/MjxUpUp/Forge/compare/v1.52.0...v1.53.0) (2026-09-08)


### Features

* **artifact-drill:** forge eval artifact-drill——产物链行为级演练沉淀 + 发版链路接线 (`963db17`)


### Bug Fixes

* **artifact-drill:** CI 守卫修正——release needs 链守卫纳入 drill 位 + raw string 字面 \n 改写 (`6b61df5`)

## [1.52.0](https://github.com/MjxUpUp/Forge/compare/v1.51.0...v1.52.0) (2026-09-07)


### Features

* **artifact-chain:** 产物链工作流化——L6 接线 + schema.yaml 分档门禁 + spec→acceptance 提取 (`43b9467`)
* **leverage-p0:** 六杠杆点 P0 批次——L5 命令面冻结仪表 + L1 楔子演练 + L2 spec-as-gate v2 数据形状 (`3af1301`)


### Bug Fixes

* **artifact-chain:** 复核 P2 修正——审批作废双侧删除防 ScoreTask 回写复活 + fail-open 补审计行 + escape 全段跳过语义 (`f0d4bd5`)
* **artifact-chain:** 独立审查修正——P0 flags 未注册 + P1 审批误杀/路径穿越 (`fa50594`)

## [1.51.0](https://github.com/MjxUpUp/Forge/compare/v1.50.0...v1.51.0) (2026-09-06)


### Features

* **focus-b1:** git/PR 收口 v1——forge gate push + pre-push 钩子（方向 A P0） (`28fda45`)
* **focus-b1l2:** 设计族拆包 + 教科书瘦身 + 死机制清理（方向 A 内容线） (`96e0182`)
* **focus-b1:** OTel GenAI exporter——checklog→OTLP/JSON 导出器（方向 D1） (`5f65b8c`)
* **focus-b1:** 自报一致性门禁 + 监控分段落地（方向 B P0） (`46262db`)
* **focus-b2cde:** issue-tracker 镜像 + task watchdog + eval 升级；B3 商业化设计（方向 C/E/F） (`b967906`)
* **focus-b2:** held-out gap 门禁 + safe-halt 语义（方向 B） (`8444093`)
* **focus-d2:** 标准卡位补齐——AAT mapper + skills inventory + 合规对照（P1 D2，批次切分漏项） (`9826c58`)
* **mechanism-p012:** P0 版本纪律 + P1-2/3 逃生舱升级与承诺表 (`e544c4a`)
* **mechanism-p012:** P1-1 compat 工件 + P2 指纹分流与生产者声明 (`f5c05e5`)


### Bug Fixes

* **ci:** watchdog 节流测试跨小时边界稳定化 + pluginpack 多 pack marketplace 生成器 (`a1938a1`)
* **dead-code-sweep:** pluginpack README 守卫断言随 suggest 出清更新（forge off --commit 替位） (`9503796`)
* **dead-code-sweep:** 审查修复——marketplace 条目恢复 + 残留引用出清 + 承诺表裁决记录 (`fb8b53a`)
* **focus-b1l2:** skill-decisions 门禁 pack 树适配 + L2 回检发现修正 (`f285d26`)
* **focus-b1:** 命令树补全——gate hooks install 子命令化 + mirror 挂 github 子命令 (`5c278c2`)
* **focus-b1:** 复审轮修复——mirror 持久化真修 + heldout complete 路径留痕 + 覆盖缺口 (`714df36`)
* **focus-b1:** 对抗审查修复——blocker 全清 + should-fix 4 项 + notes 3 项 (`f04ee36`)
* **focus-d2:** 审查修复——exit 2 契约 + 复合键 + 根解析 + rune 截断 + 文档诚实化 (`1061315`)
* **hazard:** halt release 文案语病——解锁前提表述修正 (`bd8fba9`)
* **mechanism-p012:** 复审轮修复——AllCheckNames 守卫去虚设 + schema 种子真填满 + 文档诚实注记 (`d927f45`)
* **mechanism-p012:** 对抗审查修复——blocker + 5 项 should-fix (`e24aa54`)

## [1.50.0](https://github.com/MjxUpUp/Forge/compare/v1.49.0...v1.50.0) (2026-09-04)


### Features

* **eval:** Forge 自评测体系 P0-P4 落地——forge eval 命令族 + internal/evalkit 双轨评测栈 (`bbfa97a`)
* **eval:** 爬坡项落地——两 trap 洞闭环 + 历史反哺 golden + judge-audit 首轮 + docker 首跑 (`9563843`)
* **eval:** 补齐设计量纲——golden 三门禁 12 例 + 接续演练 3 条 + Terminal-Bench 冻结 manifest 适配器 (`5c6db1a`)
* **eval:** 评测可见性与触发点接线——Pulse 看板事件 + status 健康行 + release-readiness R6 (`46fd466`)


### Bug Fixes

* **eval:** golden e2e 断言跨平台化（真正落地——上一轮 replace 静默未生效被 CI 抓回） (`4549711`)
* **eval:** Windows CI 第二轮——golden 平台跳过机制 + e2e 断言跨平台化 + 词汇表补形态 (`6a324b4`)
* **eval:** Windows 三平台兼容——假二进制双形态 + 权限检查 GOOS 感知 + 测试去 sh 化 (`f829508`)
* **eval:** 两项审查遗留修复——混合 manifest 沙箱标签任务级粒度 + {dataDir} 硬报错 (`ccd58ad`)
* **eval:** 复审残留清理 + review 盖章（对抗审查闭环） (`b8c229c`)
* **eval:** 对抗性审查修复——C1-C3 全修 + I1-I8 全修 + Minor 挑修 (`94bec29`)

## [1.49.0](https://github.com/MjxUpUp/Forge/compare/v1.48.1...v1.49.0) (2026-09-02)


### ⚠️ 行为变更（Behavior Change）

* **接管默认策略翻转（Project Policy Layer）**：出厂 takeover 默认由"静默自动接管所有 git 项目"改为**每项目首次询问一次（ask）**——安装 plugin 授予的是能力，不再等于对每个仓库行使接管。declined（`forge off`）、`.forge-decline` 团队声明（`forge off --commit`）、外来 harness 让位（`forge policy yield`）不可被任何默认路径穿透；恢复唯一通道 `forge on`。需要旧的无感静默接管：`forge config set` takeover 为 auto（或 env `FORGE_TAKEOVER=auto`；legacy `FORGE_AUTO_INIT=1` 仍等价 auto）。新命令族：`forge off [--all] [--commit]` / `forge on` / `forge config get/set takeover` / `forge policy state|yield`。用户级指令段（CLAUDE.md/AGENTS.md/global_rules.md）收缩为指针段，激活判据锚定 `[forge-session]` 会话横幅（autoSync 版本变更后自动重刷）。

### Features

* **policy:** Project Policy Layer P1——按项目退出/恢复接管（forge off/on） (`6d26e5a`)
* **policy:** Project Policy Layer P2-P4——默认 ask 翻转 + 全局通道感知 + 外来 harness 让位 + 注册表写锁 (`1c6230d`)


### Bug Fixes

* **policy:** forge policy state 对 managed 打印字面值——StatusManaged 空串的显示层映射 (`4ff41d0`)
* **policy:** plugin README 退出文案改走生成器单一真相源——资产源 + 新二进制重生成 (`5068806`)

## [1.48.1](https://github.com/MjxUpUp/Forge/compare/v1.48.0...v1.48.1) (2026-09-01)


### Bug Fixes

* **attribution:** porcelain 测试期望改正斜杠字面量（windows CI 实证） ([#38](https://github.com/MjxUpUp/Forge/issues/38)) (`f325d78`)
* **review:** A1 补齐收尾——孤儿 gitPorcelain 删除 + 现场过滤测试随领域迁 taskpipeline (`85e78c5`)
* **review:** clitask 迁移双轨审查收口——接缝兜底与注释/死链出清 (`f351094`)
* **review:** hookdispatch 双轨审查 3 建议收口 (`bc6da60`)
* **review:** tasktypes 下沉审查收口——3 项装饰项 (`a1ec456`)
* **review:** 任务下沉审查 5 项收口 (`eae19f4`)
* **review:** 双轨审查收口——注释残留出清与死常量裁决 (`f367d4f`)
* **review:** 清扫任务双轨审查收口——4 项 LOW 全解 (`948d473`)
* **review:** 迁移审查收口——Version seam 改惰性闭包（阻断项） (`e56b4a0`)

## [1.48.0](https://github.com/MjxUpUp/Forge/compare/v1.47.0...v1.48.0) (2026-08-31)


### Features

* **cli:** vNext P1 pull 侧引导——forge next 单命令 + task wild 申报 + 文案接线 (`5005f78`)
* **cli:** vNext P2 审计层——forge enforcement 报告+随机审计+双环/降格信号 (`649a314`)
* **hooks:** task-guard 执法谱系 v2——无视计数器+zcode 提升+测试锚定 (`565c2c5`)
* **registry:** gc 回收孤儿项目数据目录+测试夹具隔离 (`1282cef`)
* **task:** vNext P3 三段工件分层 + task-guard 缓冲窗口状态机 (`a5ff2d1`)


### Bug Fixes

* **cli:** enforcement 审查收口——全量 checklog/降格降噪/空会话不 join/错误告警 (`903937e`)
* **cli:** next 决策表对齐真实门禁链（P1 审查 FAIL 修正） (`1249dc2`)
* **hooks:** P0 审查收口——dsh-only 陈旧注释/文档同步+不变量补钉 (`1220645`)
* **registry:** gc dry-run 汇总报计划处置数+Keys godoc 英文首句 (`c137ca4`)
* **task:** P3 审查收口——锁内变更/校验前置/wild 清计数/ID 基准/启发式范围 (`717e53d`)

## [1.47.0](https://github.com/MjxUpUp/Forge/compare/v1.46.0...v1.47.0) (2026-08-30)


### Features

* **skills:** 零反向依赖迁移——R18 硬校验 + 集成知识出库 + forge 原生 skill 迁出 (`966f52b`)
* **skills:** 零反向依赖迁移——R18 硬校验 + 集成知识出库 + forge 原生 skill 迁出 (`6a235d1`)
* **T1/T4/T7:** 锁收口 + 状态完整性签名 + 产品语义修正 (`b15e3ef`)
* **T2:** 检测器黄金用例集 + 配对死条件修复 + untracked 源 + 批量 tracked (`1c292a7`)
* **T3:** checklog 轮转 janitor + stamps 清理——active 无界增长结构性收口 (`3ae37a4`)
* **T8a:** 清理批 A——卸载通道分流/断言结构化/protocol 校验/决策转义/安装盲区 (`8f1afb9`)
* **T8b:** 清理批 B——九项收尾：哨兵退出/死代码/session入口统一/跨仓依赖/白名单反转/git探测守卫/infra出口 (`631f3b7`)
* **T9:** 性能簇——更新负缓存+短超时/funnel 分桶去重复解析/MoveFile 流式/空会话隔离 (`d13228e`)


### Bug Fixes

* **make:** premerge 交叉编译步加 CGO_ENABLED=0 (`893d588`)
* **review-b1:** 两轮审查 B1 快速修复 28 项——静默降级补告警/Windows 平台修复/检测器小补 (`5483e20`)
* **review-b2-followup:** 修复 B2 引入的 e2e 回归——可执行脚本副本不可用 rename 原子写 (`32314b4`)
* **review-b2:** 数据丢失/原子写批次——用户资产全部改走 AtomicWrite + 读失败误判修复 (`b4ccd0a`)
* **review-b3:** 锁与并发批次——盖章路径入锁/串号覆盖中止/级联护史/锁身份自愈 (`506bc12`)
* **review-b4:** 信任与安全批次——--untrusted 剥离补洞/注入面封堵/发版脚本加固 (`158ba63`)
* **review-b5-followup:** srclint 白名单补录注释体 catch 正则 (`70fc13a`)
* **review-b5/b6:** 检测器批次 + 产品语义——消 contest/ 逃逸区/补 Go 族检测/init 自伤修复 (`e4f45b0`)
* **skills:** review 修复——notes 入库（C1）+ 悬空指针清零（C2）+ 漏迁条件块迁出（C3） (`4f01b25`)

## [1.46.0](https://github.com/MjxUpUp/Forge/compare/v1.45.4...v1.46.0) (2026-08-28)


### Features

* **conventions:** 依赖倒置契约化——CONVENTIONS §13 + R18 规则 + 17 skill 正文合规迁移 (`4b936be`)
* **conventions:** 后续三件套——lint 未跑门禁 + learn 纠正写回 + codex 写入时刻补全 (`c1f3e81`)
* skills 依赖倒置 + code-review-gate 边界重划 + 工程原则增强 (`2906f9c`)
* **skills:** 工程原则增强——真·SOLID 五原则 + 契约完整性 + 测试规格档 (`95304ec`)
* **skills:** 拆分 code-review-gate——doc-review 建家 + phase-*.md 归位 design-artifact-standards (`c214cbe`)


### Bug Fixes

* **ci:** 分支 CI 抓出的两类跨平台断言——dsh 名册同步 + Windows 路径分隔符 (`c0e9caf`)
* **ci:** 注入断言先解 JSON 信封再归一——信封级 ToSlash 会把转义反斜杠变成双斜杠 (`cc7dfd6`)
* 双独立审查（代码双轨 + 文档四维）14 项发现全闭环 (`81b7725`)

## [1.45.4](https://github.com/MjxUpUp/Forge/compare/v1.45.3...v1.45.4) (2026-08-28)


### Bug Fixes

* **task:** dogfood [#6](https://github.com/MjxUpUp/Forge/issues/6)——--branch 共享 ref 派生 + 两项使用偏离引导 (`690be1a`)

## [1.45.3](https://github.com/MjxUpUp/Forge/compare/v1.45.2...v1.45.3) (2026-08-28)


### Bug Fixes

* **attribution:** dogfood 发现 [#5](https://github.com/MjxUpUp/Forge/issues/5)——hook 记账路径归一（绝对→repo 相对） (`e3bc37d`)

## [1.45.2](https://github.com/MjxUpUp/Forge/compare/v1.45.1...v1.45.2) (2026-08-27)


### Bug Fixes

* **worktree:** [#4](https://github.com/MjxUpUp/Forge/issues/4) 二次修订——finish 真接 ClearByID + abort 全量清扫绑定 (`1b5c06e`)

## [1.45.1](https://github.com/MjxUpUp/Forge/compare/v1.45.0...v1.45.1) (2026-08-27)


### Bug Fixes

* **dogfood:** 实测三项修复——worktree ref 派生 / 归档陈旧快照 / 绑定残留 (`75f0028`)
* **harness:** dogfood 发现——gitignore 改根级允许清单（只跟踪 projects/） (`66ddf01`)

## [1.45.0](https://github.com/MjxUpUp/Forge/compare/v1.44.0...v1.45.0) (2026-08-27)


### Features

* **attribution:** L3 归属服务——session→文件台账 + Stop 对账 + 覆盖率度量（T2） (`e900633`)
* **attribution:** T3 消费者切换——四类工作树读取全部经归属过滤 (`ea1977e`)
* **harness:** T6 harness repo——git 化用户级台账 + 信任分类 + 边界批量提交 (`91c9e1d`)
* **harness:** T7 引导层——onboarding 状态机 + 触发点 + 防 nag (`e4c32c5`)
* **harness:** T9 传输换代——git remote push/pull + 首推出境 HITL (`e10d381`)
* **multi-task-concurrency:** T10 收尾——dispatcher 心跳接线 + 落地记录 + dogfood 门禁通过 (`7d6cb0f`)
* **observability:** T10 度量收尾——status 归属覆盖率行 + 并发矩阵总测 (`2adf07e`)
* **review:** 评审可观测性——finding 带轮次/快照、非 task 盖章落 checklog、审计去重标注 (`5392309`)
* **skills:** code-review-gate 收敛纪律——复审新发现归因 + 双轨分歧判读 + --note 实质留痕 (`f2320fa`)
* **specs:** T8 产物契约层——specs 文件产物 + 哈希引用 + attempts 回灌 (`1577585`)
* **state:** L2 事件化——task start 废 Clear 改边界事件 + stamp 内容寻址 (`dc495b9`)
* **worktree:** T4 身份层——workspace 绑定存储 + 解析链 v2 + P5 守卫 (`08acadc`)
* **worktree:** T5 worktree 生命周期——start --worktree / finish / janitor (`049946f`)


### Bug Fixes

* **ci:** Windows 跨平台修复——HITL 确认语义统一 + worktree 删除 CWD 锁防护 (`1093f5b`)
* **premerge:** 预检修复——接续 fixture 对齐解析链 v2 + README 补新命令 + gofmt (`b445396`)
* **review:** 复审残留——priorAttempts 真实接线进 HANDOFF + symlink 主检出判定 (`6bebee0`)
* **review:** 审查 nits——CheckReviewPass 注释同步双模式、unused-scan 同款去重标注 (`9d18d31`)
* **review:** 审查修复——B1 共享会话误标外来 / B2 finish 合并守卫 / H 台账 TTL / M1 M2 LOW 六项 (`b9fabd4`)

## [1.44.0](https://github.com/MjxUpUp/Forge/compare/v1.43.0...v1.44.0) (2026-08-26)


### Features

* **dashboard:** sync可观测——操作结果落checklog(project-sync,init/push/pull成败皆录,status只读不落;具名返回defer单点捕获)经feed上板为新kind sync;projects.json行带绑定与最近push/pull(sync-remote.json直读不走指纹缓存,omitempty未绑定零结构变化);observation类排除出证据分桶;复审minor全修(用法错误不落章/失败Detail带remote截断300rune) (`e95a0ad`)
* **dashboard:** 三契约上板——state.lease投影租约(持有者/有效/过期时刻/fencing,ExpiresAt抽单一公式)/docReview块(L2回检判定+rubric分+轮次,roundsTotal钳制防--round跳号自相矛盾)/skills总览送达列(复用BuildTriggerFunnel送达章,nil存量诚实单列);复审minor全修:ExpiresAt补nil对称防护/ReviewedAt改指针免零值假日期 (`85f84a7`)
* **dashboard:** 验签事件流——bundle验签verdict落checklog(bundle-verify,五档Level映射,Meta携verdict+signer结构化契约)并经feed上板为新kind sig-verify(severity取EffectiveLevel,标题读Meta不解析散文);observation类排除出证据分桶;dry-run不落章保--dry-run无侧效应契约;复审minor全修(nil防御/ZH注释补齐) (`ce23dd9`)
* **workspace:** 多 repo workspace——workspaces.json 清单+跨仓影响门禁+跨仓任务依赖 (`25acdfa`)


### Bug Fixes

* **dashboard:** 枚举兼容三修——Weak证据染红(原落绿色分支误导)/未知grade·kind中性兜底(原染红F·冒用task样式)/skill名改结构化字段FeedEvent.Skill(折叠卡原正则反解中文标题随措辞静默失效) (`2245c99`)
* **workspace:** 复审加固——key 格式 allowlist + status 守卫 + 仓根缓存 (`0c881e8`)

## [1.43.0](https://github.com/MjxUpUp/Forge/compare/v1.42.2...v1.43.0) (2026-08-25)


### Features

* **agentbridge:** 新增 ZCode (z.ai) 宿主适配——translator 合并写 ~/.zcode/cli/config.json + ~/.zcode 用户级检测与 .zcode 项目标记归因 + hostcap 行 + 卸载/doctor/init 摘要集成 (`7d9861f`)
* **readability:** AI 产物可读性三层约束与输出→回检门禁落地——L1 `forge docs lint`（D1-D7 确定性规则）+ L2 rubric 评审（`forge task doc-review`）+ task-complete doc gate + 5 个文档模板 + 评分新增表达质量维度（[设计](docs/design/output-readability-gates.md)） (`b36fa13`)


### ⚠ 行为变更（非 BREAKING，需知悉）

* 任务评分六维 → 七维：新增「表达质量」维度（权重 0.10，其余维度权重相应重平衡）——同一任务跨版本分数不可直接比较；纯代码任务（无文档产物变更）该维度打中性 100 不受影响
* task-complete 新增 doc gate 门禁：任务变更 markdown 产物时，complete 前须过 L1 lint + L2 回检证据（`forge task doc-review`，rubric ≥75 且零未决 Critical）；逃生舱 `forge task override --doc-gate disable` / `FORGE_DOC_GATE=disable`（落 checklog 审计，评分封顶 89/维度封顶 60）


### Bug Fixes

* CLI 一致性与人体工学 (`7aeae6c`)
* **doclint:** 类型匹配改用 BASE 名（修 session-retrospective 目录误判）+ decisions.md 豁免（append-only 治理日志非即时阅读产物） (`f0b8f58`)
* guard 准确性三修（assertion-check / read-before-edit / bash-guard） (`63ad68f`)
* hazard-guard 误报治理与授权路径协议 (`ec4e30b`)
* hook 文案死引用清理与 AGENTS.md 模板事实修正 (`66340fa`)
* kimi 宿主 advisory 改走 pending 队列 + UserPromptSubmit 攒发 (`9be4c40`)
* **readability:** L2 文档评审跟进——rubric 补类型覆盖范围声明/PR 模板段数契约修正/设计文档签名与豁免清单同步/模板删复述收尾句/rubric 独立性条款收紧/路由行去硬列举（评审 93 分零 Critical，逐条 delete-list 执行） (`2af8d77`)
* **readability:** 代码审查跟进——C1 CHANGELOG 豁免大小写死码/C2 存量文档过自身门禁（SKILL.md 反引号+checklist 收窄 release-）/C3 设计文档强制入库（全局 gitignore 吞未跟踪 docs）/I1 CLI --base 与门禁同集合（含未跟踪剔已删除）/I3 checklog Level 仅阻断分支/I4 git diff 失败落审计/I5 DocReview 增内容指纹（未提交修改判过期）/I6 skill 渲染反引号/M1-M9（围栏 run 长度/D4 散文限定/D7 非围栏计数/IO 规则登记/帮助文本同步/chore golden 案例恢复/CLI 单测/盲区声明） (`0b823e8`)
* **readability:** 双复审跟进——D4 触发词限定散文（修围栏内设问误报+行号保原始）/session-retrospective 验证指针改为 lint+test 双查（单测扫不出存量误伤）/设计文档笔误与版本口径/--base flag 帮助同步 (`d7d0aa5`)
* **readability:** 复审跟进——删未接线 HasHard/doclint 豁免 .zcode 会话目录/code-review-gate 补决策 (`8a2d759`)
* skill-trigger 控噪与 advisory 去重 (`ca529e7`)
* skills frontmatter 治理 (`d852444`)
* 门禁漏洞四修 (`2eb3e31`)

## [1.42.2](https://github.com/MjxUpUp/Forge/compare/v1.42.1...v1.42.2) (2026-08-24)


### Bug Fixes

* **hooks:** session marker 迁 FORGE_DATA_DIR/markers——MSYS /tmp 只读机器上 NOWARN 去噪静默失效 (`836c897`)
* **qa:** 回顾发现三问题的结构修复——audit 自指豁免/decide 拒写 embed 缓存/hazard confirm --last (`238c282`)
* **qa:** 复审跟进五项——decisions.md 豁免收窄为根级+仅 DC-10/decide 测试哨兵化/--last Args 测试+优先级说明/竞态披露 (`1a86925`)
* **skills:** DC-10 跟进——3 skill 的 npx 调用改 lockfile 锁定的本地依赖运行形态 (`6fe8c6b`)
* **skills:** 复审跟进四项——decisions 自回引改连字符形态/preview 改 npm exec 不依赖预置 script/审查污染还原提示/tsc 注释精确化（audit 复扫 0 finding） (`ad137cd`)

## [1.42.1](https://github.com/MjxUpUp/Forge/compare/v1.42.0...v1.42.1) (2026-08-23)


### Bug Fixes

* **act:** 逃生舱cap证据缩放+nudge 14天窗口+历史结论就地迁移 (`62c02eb`)
* **protocol:** 审查-修复-复审闭环补复审规定——多轮盖章 ADVISORY+SKILL.md+生成文案 (`6c54c68`)
* **review-r2:** 复审五项修复——窗口沿内联注释/override Short 谎报/笔误/豁免措辞统一+守卫测试 (`5d62720`)
* **review-r3:** 窗口内侧边界注释 1ns→1 秒，与代码 time.Second 一致（第三轮验证 INFO） (`848ca89`)
* **review:** code-review 六项修复——README第6处谎报文案/评分封顶独立性注释/豁免说明补齐/窗口沿契约/权限保留/过时注释 (`7bb65a8`)
* **review:** 复审跟进四项——快照增量触发/决策ID回归生成器/死断言/文档同步 (`0059111`)

## [1.42.0](https://github.com/MjxUpUp/Forge/compare/v1.41.0...v1.42.0) (2026-08-23)


### Features

* **hostcap:** dsh task-guard advisory 升级为 exit-2 硬阻断（PromoteAdvisory 路径 (b)） (`17fc107`)


### Bug Fixes

* **pulse-task:** task.json Truncated 透传+证据反编造守卫+前端无证据如实展示 (`c5d0eb7`)

## [1.41.0](https://github.com/MjxUpUp/Forge/compare/v1.40.1...v1.41.0) (2026-08-22)


### Features

* **agentbridge:** 扩接 failure-track/subagent-track 到 cursor+copilot，补 cursor payload 方言适配 (`e54971e`)
* **hooks:** 接线三观察hook补事件缺口+PreToolUse permissionDecision+Bash tool-track (`a8fd3c6`)


### Bug Fixes

* **deferred-batch1:** 延后项批量落地——uninstall codebuddy/kimi-manifest 出口/doctor 未装目标门控/dsh 文档补齐 (`a098b45`)
* **docs,doctor:** 补齐协议文档9个接线hook + doctor新增skills分发审计节 (`c701f6d`)
* **skillseval:** effectiveness 被动 join 修复非 git 数据目录解析+测试判别力（评审 M 级两项） (`4b53b85`)

## [1.40.1](https://github.com/MjxUpUp/Forge/compare/v1.40.0...v1.40.1) (2026-08-21)


### Bug Fixes

* **update:** forge update 感知 npm 安装通道——npm 用户改查 npm registry 并重定向到对应包管理器 ([#18](https://github.com/MjxUpUp/Forge/issues/18)) (`7c66a1a`)

## [1.40.0](https://github.com/MjxUpUp/Forge/compare/v1.39.1...v1.40.0) (2026-08-21)


### Features

* **git-sync:** forge project sync init/push/pull/status——git 传输通道（forge-sync 固定分支、nodes/&lt;node_id&gt;/&lt;key&gt;/ 前缀只写自己、bundle 覆盖式推送、pull 复用 project import 账本幂等）——Phase 1 传输层 (`ae14ee5`)
* **hlc:** 混合逻辑时钟——Timestamp(Wall+Logical)/Compare/Parse + Clock.Now/Observe，回拨下单调、并发唯一——多机器 Phase 0，sync-convergence §3 的 LWW 决胜键 (`7370fbe`)
* **nodeid:** 节点身份地基——ed25519 密钥对，node_id=公钥指纹（fnode_&lt;32hex&gt;），rotation_chain 格式预留，forge node show（私钥不出展示面）——多机器 Phase 0，设计见 docs/design/node-identity.md (`a587e88`)
* **nodestamp:** 事件打戳——Stamp(node_id/seq/ts_hlc/sig) 内嵌 checklog/toolusage/act/sessions 四收口点，node-seq 跨进程计数器（O_EXCL 锁+persist-before-use+原子落盘），fail-open 零戳，损坏禁用防 seq 复用——node-identity.md §4 (`acbdd3a`)
* **pulse-node:** Pulse 事件流渲染 node 归因——FeedEvent.Node（conclusion/skill-trigger 携 nodestamp，task-start 携租约持有者，存量无戳记录零字段）+ 前端 node-chip（fnode 短标签）——Phase 3 (`2ad2a5a`)
* **task-convergence:** MergeTaskStateSync 收敛层——规范排序+确定性决胜（交换律/幂等字节一致）、ReviewRounds 并集防采纳覆盖、SessionLinks/History 单侧重复归一、40 种子 property test + 双 DataDir 双向合并测试——sync-convergence §2 B 类 (`f2b916c`)
* **task-lease:** 跨机任务租约——Lease(holder/ts_hlc/ttl/fencing)+start 自动认领(fail-open)+gate 他机活跃租约 advisory+合并 fencing 高者胜——sync-convergence §4 个人档 (`905133f`)
* **trust:** 信任层——trust.json store（TOFU+0600+原子写）+ forge trust list/add/remove/require-signed + bundle .sig sidecar 签名（export/sync push 无条件签）与导入验签（invalid 恒拒/团队档未签拒/未知签名者告警）+ 双机 sign→verify e2e——node-identity §3 (`2484aa2`)


### Bug Fixes

* **ci:** 分支三平台 CI 首跑的 Windows 失败修复 (`a155235`)
* **git-sync:** skillRefAllowlist 收编 forge-sync（同步通道固定分支名，非 skill） (`939cfd6`)
* **git-sync:** 审查跟进——ls-remote 区分无分支/不可达（init 真 fail-fast 且不写半成品绑定）、push 一次重拉重试（并发非快进收敛）、commit 限定前缀+扫 tmp 残骸+关 gpgsign/hooks、pull 逐节点容错+ValidNodeID 形态检查、补不可达 init/坏节点跳过测试 (`6d3ef9a`)
* **hlc:** 审查跟进——Logical 饱和推进 Wall 替代 int32 回绕（静默破单调+不可解析）、String 全定宽（%019d.%010d，字符串序==Compare序全值域成立）、Parse 拒非数字/前导+、补溢出与等墙 recv 分支测试 (`a52c618`)
* **nodeid:** 审查跟进——Save 原子化（CreateTemp+fsync+rename）、CheckConsistent 拒 null rotation_chain、Load 收紧宽松权限、ValidNodeID 手写校验对齐 fpid 风格、私钥值级防泄断言、补篡改/损坏分支测试 (`c6cedda`)
* **pulse-node:** 复审跟进——task-start node 复用「过期即自由」单一规则（Lease.ActiveAt，崩溃机器 stale 认领不留看板）、测试补有效/过期双边界+wire 级 omitempty 断言+吞错修复、UI title 区分「当前持有/来源机器」语义 (`d080cc3`)
* **review-followup:** dsh 交付复审 14 项发现修复——静默丢推送/字面量\n/TOCTOU/验签前置/可观测性 (`8f28ebc`)
* **task-convergence:** 复审跟进——completionCanon 剔除并集字段 ReviewRounds + 纳入 AcceptanceForeign（同命令异标志=不同块）、标量验收决胜键含标志、dedupByKey 保持 nil/空表示（防决胜键跨轮翻转）、property test 补 stepwise 轮次收敛断言 (`7f91a4f`)
* **task-convergence:** 审查跟进——History 改全内容并集（保住重试 provenance，时间序保 lastGateAt 锚）、review 锚只随完成块走（防跨块混杂）、块决胜非空优先、AcceptanceForeign 随采纳块、SessionLinks 冲突 Sync 路径确定性裁决、不可信路径恢复本地权威、property test 共享 ID 池+全字段 op (`8497c30`)
* **task-lease:** 复审跟进——resume/attach 接手方认领租约（advisory 追踪实际工作机）、同值 fencing 破平带 oracle 定向测试（双机同时认领收敛） (`d878232`)
* **trust:** 复审跟进——篡改 e2e 分两层钉（unpack 完整性层 + 重打包挂旧 sidecar 的签名层真拒）、pull 失败节点汇总为 pull 级错误（策略拒收不再静默 exit 0）、团队档签名失败硬错误、.sig 原子写、trust CLI 面测试、设计文档实现校正 (`ffb4e7c`)

## [1.39.1](https://github.com/MjxUpUp/Forge/compare/v1.39.0...v1.39.1) (2026-08-20)


### Bug Fixes

* **dsh/opencode:** win32 spawn 走 cmd.exe 解析 npm .cmd shim——修掉全门禁静默失效 (`1a0c1a4`)
* **dsh:** @agent_forge/forge-dsh 0.1.1 随发版火车发布——Windows spawn 修复到达插件用户（release.yml 幂等发布，读插件自身 version）

## [1.39.0](https://github.com/MjxUpUp/Forge/compare/v1.38.2...v1.39.0) (2026-08-20)


### Features

* **agentbridge:** 接入 DeepSeek Harness 插件生态（plugins/forge-dsh） (`0a4b7f3`)


### Bug Fixes

* **release:** 首发前审查跟进——license 对齐 Apache-2.0 + forge-dsh dry-run 门禁 (`0e22f66`)

## [1.38.2](https://github.com/MjxUpUp/Forge/compare/v1.38.1...v1.38.2) (2026-08-20)


### Bug Fixes

* **agentbridge:** kimi plugin manifest 恢复 skill-trigger 全事件绑定——看板 kimi 任务仅 5 事件 (`b4a0a27`)

## [1.38.1](https://github.com/MjxUpUp/Forge/compare/v1.38.0...v1.38.1) (2026-08-20)


### Bug Fixes

* **cli:** sync/migrate/project help 加跨机器迁移交叉指引 (`a15a41a`)
* **skillsqa:** 修正安全规则数自述 22→21（实计 21=18 对齐 audit.py+3 本地） (`fb622c7`)

## [1.38.0](https://github.com/MjxUpUp/Forge/compare/v1.37.0...v1.38.0) (2026-08-19)


### Features

* **ci:** release-please 接管发版——Release PR 自动 bump/tag，dispatch 串联 release.yml (`e73609c`)


### Bug Fixes

* **ci:** release-please workflow 被 GitHub 静态拒绝——secrets 上下文移出 steps.if (`735bab7`)
* **ci:** 审查修复——守卫锚定断言/删always-update/串行化/先算后写 (`e68f28a`)
