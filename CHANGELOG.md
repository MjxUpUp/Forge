# Changelog

## [1.46.0](https://github.com/MjxUpUp/Forge/compare/v1.45.4...v1.46.0) (2026-08-28)


### Features

* **conventions:** 依赖倒置契约化——CONVENTIONS §13 + R18 规则 + 17 skill 正文合规迁移 ([4b936be](https://github.com/MjxUpUp/Forge/commit/4b936be79fc86f30eb19ba3e3bf9c68bc473f18f))
* **conventions:** 后续三件套——lint 未跑门禁 + learn 纠正写回 + codex 写入时刻补全 ([c1f3e81](https://github.com/MjxUpUp/Forge/commit/c1f3e81b265a754e80426cc79c458959e37b6c78))
* skills 依赖倒置 + code-review-gate 边界重划 + 工程原则增强 ([2906f9c](https://github.com/MjxUpUp/Forge/commit/2906f9c42417166f8cce996f6845da72627e013e))
* **skills:** 工程原则增强——真·SOLID 五原则 + 契约完整性 + 测试规格档 ([95304ec](https://github.com/MjxUpUp/Forge/commit/95304ec26395032368863adf65da9b34dae5a54c))
* **skills:** 拆分 code-review-gate——doc-review 建家 + phase-*.md 归位 design-artifact-standards ([c214cbe](https://github.com/MjxUpUp/Forge/commit/c214cbe228ac789ff78453d5819edb372b301383))


### Bug Fixes

* **ci:** 分支 CI 抓出的两类跨平台断言——dsh 名册同步 + Windows 路径分隔符 ([c0e9caf](https://github.com/MjxUpUp/Forge/commit/c0e9caf7d7fad13aeb8997716f3958be4c7ca768))
* **ci:** 注入断言先解 JSON 信封再归一——信封级 ToSlash 会把转义反斜杠变成双斜杠 ([cc7dfd6](https://github.com/MjxUpUp/Forge/commit/cc7dfd6934952a63c2baea091b038dd64b681074))
* 双独立审查（代码双轨 + 文档四维）14 项发现全闭环 ([81b7725](https://github.com/MjxUpUp/Forge/commit/81b772505841eb113e966be57ad335532934fb04))

## [1.45.4](https://github.com/MjxUpUp/Forge/compare/v1.45.3...v1.45.4) (2026-08-28)


### Bug Fixes

* **task:** dogfood [#6](https://github.com/MjxUpUp/Forge/issues/6)——--branch 共享 ref 派生 + 两项使用偏离引导 ([690be1a](https://github.com/MjxUpUp/Forge/commit/690be1a740b2b9d8a942cea21ebbb55c406a6099))

## [1.45.3](https://github.com/MjxUpUp/Forge/compare/v1.45.2...v1.45.3) (2026-08-28)


### Bug Fixes

* **attribution:** dogfood 发现 [#5](https://github.com/MjxUpUp/Forge/issues/5)——hook 记账路径归一（绝对→repo 相对） ([e3bc37d](https://github.com/MjxUpUp/Forge/commit/e3bc37defce4b4665157f027bba203621e9ea1ca))

## [1.45.2](https://github.com/MjxUpUp/Forge/compare/v1.45.1...v1.45.2) (2026-08-27)


### Bug Fixes

* **worktree:** [#4](https://github.com/MjxUpUp/Forge/issues/4) 二次修订——finish 真接 ClearByID + abort 全量清扫绑定 ([1b5c06e](https://github.com/MjxUpUp/Forge/commit/1b5c06e4d77271af9e56f73b105166f24f615eeb))

## [1.45.1](https://github.com/MjxUpUp/Forge/compare/v1.45.0...v1.45.1) (2026-08-27)


### Bug Fixes

* **dogfood:** 实测三项修复——worktree ref 派生 / 归档陈旧快照 / 绑定残留 ([75f0028](https://github.com/MjxUpUp/Forge/commit/75f002800fe08ffac4adda92fc339345bfd415cd))
* **harness:** dogfood 发现——gitignore 改根级允许清单（只跟踪 projects/） ([66ddf01](https://github.com/MjxUpUp/Forge/commit/66ddf01556c28873d7cb788639c8ec144add8f18))

## [1.45.0](https://github.com/MjxUpUp/Forge/compare/v1.44.0...v1.45.0) (2026-08-27)


### Features

* **attribution:** L3 归属服务——session→文件台账 + Stop 对账 + 覆盖率度量（T2） ([e900633](https://github.com/MjxUpUp/Forge/commit/e90063352dc093d621057a150b89ad31f23ef38d))
* **attribution:** T3 消费者切换——四类工作树读取全部经归属过滤 ([ea1977e](https://github.com/MjxUpUp/Forge/commit/ea1977e129818cae6aa663d7001600eeaaebf1d2))
* **harness:** T6 harness repo——git 化用户级台账 + 信任分类 + 边界批量提交 ([91c9e1d](https://github.com/MjxUpUp/Forge/commit/91c9e1df682b3ac2a6c9f8a0cae21681191d2379))
* **harness:** T7 引导层——onboarding 状态机 + 触发点 + 防 nag ([e4c32c5](https://github.com/MjxUpUp/Forge/commit/e4c32c5603b51317159f471f8ce0f7a49c66ddef))
* **harness:** T9 传输换代——git remote push/pull + 首推出境 HITL ([e10d381](https://github.com/MjxUpUp/Forge/commit/e10d381591ae451561f8c27609093e1e8101bb45))
* **multi-task-concurrency:** T10 收尾——dispatcher 心跳接线 + 落地记录 + dogfood 门禁通过 ([7d6cb0f](https://github.com/MjxUpUp/Forge/commit/7d6cb0f7c763e40c3e81ffccbbf494b8d4d68db3))
* **observability:** T10 度量收尾——status 归属覆盖率行 + 并发矩阵总测 ([2adf07e](https://github.com/MjxUpUp/Forge/commit/2adf07e437537f13c58d1149d88f126f821dcc1f))
* **review:** 评审可观测性——finding 带轮次/快照、非 task 盖章落 checklog、审计去重标注 ([5392309](https://github.com/MjxUpUp/Forge/commit/5392309b5c5ca72976738d71a969dc7999ad1f1a))
* **skills:** code-review-gate 收敛纪律——复审新发现归因 + 双轨分歧判读 + --note 实质留痕 ([f2320fa](https://github.com/MjxUpUp/Forge/commit/f2320fafe00cd4ba06f3740827503a4fdaadda97))
* **specs:** T8 产物契约层——specs 文件产物 + 哈希引用 + attempts 回灌 ([1577585](https://github.com/MjxUpUp/Forge/commit/1577585046de066a0d1778721bd30fa6ab43669c))
* **state:** L2 事件化——task start 废 Clear 改边界事件 + stamp 内容寻址 ([dc495b9](https://github.com/MjxUpUp/Forge/commit/dc495b929512b3e1b9669517fa300201835bc51a))
* **worktree:** T4 身份层——workspace 绑定存储 + 解析链 v2 + P5 守卫 ([08acadc](https://github.com/MjxUpUp/Forge/commit/08acadc8422e54912f6a591b7472be785f6bd293))
* **worktree:** T5 worktree 生命周期——start --worktree / finish / janitor ([049946f](https://github.com/MjxUpUp/Forge/commit/049946f40efcdeb785bf34e1f5c1cb10433dbc83))


### Bug Fixes

* **ci:** Windows 跨平台修复——HITL 确认语义统一 + worktree 删除 CWD 锁防护 ([1093f5b](https://github.com/MjxUpUp/Forge/commit/1093f5bcf4a0b47fb05a1bc099f59d5a96cf8e14))
* **premerge:** 预检修复——接续 fixture 对齐解析链 v2 + README 补新命令 + gofmt ([b445396](https://github.com/MjxUpUp/Forge/commit/b445396af7b83bda561b179af25dd87eb4253fc2))
* **review:** 复审残留——priorAttempts 真实接线进 HANDOFF + symlink 主检出判定 ([6bebee0](https://github.com/MjxUpUp/Forge/commit/6bebee0226a2194067b86cdfca71f04ad50e730f))
* **review:** 审查 nits——CheckReviewPass 注释同步双模式、unused-scan 同款去重标注 ([9d18d31](https://github.com/MjxUpUp/Forge/commit/9d18d3189c61fadfc781555f29f3aac00e3c17bf))
* **review:** 审查修复——B1 共享会话误标外来 / B2 finish 合并守卫 / H 台账 TTL / M1 M2 LOW 六项 ([b9fabd4](https://github.com/MjxUpUp/Forge/commit/b9fabd44097fee183f06a4b5891441ed709351e8))

## [1.44.0](https://github.com/MjxUpUp/Forge/compare/v1.43.0...v1.44.0) (2026-08-26)


### Features

* **dashboard:** sync可观测——操作结果落checklog(project-sync,init/push/pull成败皆录,status只读不落;具名返回defer单点捕获)经feed上板为新kind sync;projects.json行带绑定与最近push/pull(sync-remote.json直读不走指纹缓存,omitempty未绑定零结构变化);observation类排除出证据分桶;复审minor全修(用法错误不落章/失败Detail带remote截断300rune) ([e95a0ad](https://github.com/MjxUpUp/Forge/commit/e95a0ada33a2811a16dec8e41396bf5767aef613))
* **dashboard:** 三契约上板——state.lease投影租约(持有者/有效/过期时刻/fencing,ExpiresAt抽单一公式)/docReview块(L2回检判定+rubric分+轮次,roundsTotal钳制防--round跳号自相矛盾)/skills总览送达列(复用BuildTriggerFunnel送达章,nil存量诚实单列);复审minor全修:ExpiresAt补nil对称防护/ReviewedAt改指针免零值假日期 ([85f84a7](https://github.com/MjxUpUp/Forge/commit/85f84a7c675a2f7a6ee861bde402d7f9efcdbc93))
* **dashboard:** 验签事件流——bundle验签verdict落checklog(bundle-verify,五档Level映射,Meta携verdict+signer结构化契约)并经feed上板为新kind sig-verify(severity取EffectiveLevel,标题读Meta不解析散文);observation类排除出证据分桶;dry-run不落章保--dry-run无侧效应契约;复审minor全修(nil防御/ZH注释补齐) ([ce23dd9](https://github.com/MjxUpUp/Forge/commit/ce23dd98f90132dde53aec466301702d4b4e2f42))
* **workspace:** 多 repo workspace——workspaces.json 清单+跨仓影响门禁+跨仓任务依赖 ([25acdfa](https://github.com/MjxUpUp/Forge/commit/25acdfab988133f800dc44f0c1403b4b4f970678))


### Bug Fixes

* **dashboard:** 枚举兼容三修——Weak证据染红(原落绿色分支误导)/未知grade·kind中性兜底(原染红F·冒用task样式)/skill名改结构化字段FeedEvent.Skill(折叠卡原正则反解中文标题随措辞静默失效) ([2245c99](https://github.com/MjxUpUp/Forge/commit/2245c994e07d34e97721554f3ce50a2b6f41a782))
* **workspace:** 复审加固——key 格式 allowlist + status 守卫 + 仓根缓存 ([0c881e8](https://github.com/MjxUpUp/Forge/commit/0c881e8d3c78f9f2e57fb1ca841eeff2e64d36c1))

## [1.43.0](https://github.com/MjxUpUp/Forge/compare/v1.42.2...v1.43.0) (2026-08-25)


### Features

* **agentbridge:** 新增 ZCode (z.ai) 宿主适配——translator 合并写 ~/.zcode/cli/config.json + ~/.zcode 用户级检测与 .zcode 项目标记归因 + hostcap 行 + 卸载/doctor/init 摘要集成 ([7d9861f](https://github.com/MjxUpUp/Forge/commit/7d9861f6ebfa494627aa35bb0c9fa51662f640f2))
* **readability:** AI 产物可读性三层约束与输出→回检门禁落地——L1 `forge docs lint`（D1-D7 确定性规则）+ L2 rubric 评审（`forge task doc-review`）+ task-complete doc gate + 5 个文档模板 + 评分新增表达质量维度（[设计](docs/design/output-readability-gates.md)） ([b36fa13](https://github.com/MjxUpUp/Forge/commit/b36fa13bd99ec6855888b8ed9275db1d9d8fb39e))


### ⚠ 行为变更（非 BREAKING，需知悉）

* 任务评分六维 → 七维：新增「表达质量」维度（权重 0.10，其余维度权重相应重平衡）——同一任务跨版本分数不可直接比较；纯代码任务（无文档产物变更）该维度打中性 100 不受影响
* task-complete 新增 doc gate 门禁：任务变更 markdown 产物时，complete 前须过 L1 lint + L2 回检证据（`forge task doc-review`，rubric ≥75 且零未决 Critical）；逃生舱 `forge task override --doc-gate disable` / `FORGE_DOC_GATE=disable`（落 checklog 审计，评分封顶 89/维度封顶 60）


### Bug Fixes

* CLI 一致性与人体工学 ([7aeae6c](https://github.com/MjxUpUp/Forge/commit/7aeae6cd9184163451006472c0d845137ed0d7a9))
* **doclint:** 类型匹配改用 BASE 名（修 session-retrospective 目录误判）+ decisions.md 豁免（append-only 治理日志非即时阅读产物） ([f0b8f58](https://github.com/MjxUpUp/Forge/commit/f0b8f58e5c5210fb6c8f224dffaee7689fbacca7))
* guard 准确性三修（assertion-check / read-before-edit / bash-guard） ([63ad68f](https://github.com/MjxUpUp/Forge/commit/63ad68fd4465082cb7ca686f0e5759736c8935e6))
* hazard-guard 误报治理与授权路径协议 ([ec4e30b](https://github.com/MjxUpUp/Forge/commit/ec4e30b7b640c90e275f38ccadef4a3fa047a3a0))
* hook 文案死引用清理与 AGENTS.md 模板事实修正 ([66340fa](https://github.com/MjxUpUp/Forge/commit/66340fa223bd467acace6fb63ab3b1e65f3c6871))
* kimi 宿主 advisory 改走 pending 队列 + UserPromptSubmit 攒发 ([9be4c40](https://github.com/MjxUpUp/Forge/commit/9be4c4042c06b100fd52030bad4405df1ee877b4))
* **readability:** L2 文档评审跟进——rubric 补类型覆盖范围声明/PR 模板段数契约修正/设计文档签名与豁免清单同步/模板删复述收尾句/rubric 独立性条款收紧/路由行去硬列举（评审 93 分零 Critical，逐条 delete-list 执行） ([2af8d77](https://github.com/MjxUpUp/Forge/commit/2af8d77778a2a0b3805188b0e88b0918bfc46a2e))
* **readability:** 代码审查跟进——C1 CHANGELOG 豁免大小写死码/C2 存量文档过自身门禁（SKILL.md 反引号+checklist 收窄 release-）/C3 设计文档强制入库（全局 gitignore 吞未跟踪 docs）/I1 CLI --base 与门禁同集合（含未跟踪剔已删除）/I3 checklog Level 仅阻断分支/I4 git diff 失败落审计/I5 DocReview 增内容指纹（未提交修改判过期）/I6 skill 渲染反引号/M1-M9（围栏 run 长度/D4 散文限定/D7 非围栏计数/IO 规则登记/帮助文本同步/chore golden 案例恢复/CLI 单测/盲区声明） ([0b823e8](https://github.com/MjxUpUp/Forge/commit/0b823e8403ba71f31e8df00d0fc8f07ca91d11a9))
* **readability:** 双复审跟进——D4 触发词限定散文（修围栏内设问误报+行号保原始）/session-retrospective 验证指针改为 lint+test 双查（单测扫不出存量误伤）/设计文档笔误与版本口径/--base flag 帮助同步 ([d7d0aa5](https://github.com/MjxUpUp/Forge/commit/d7d0aa5b9b503828f2472d9e0998cdcfec00b44d))
* **readability:** 复审跟进——删未接线 HasHard/doclint 豁免 .zcode 会话目录/code-review-gate 补决策 ([8a2d759](https://github.com/MjxUpUp/Forge/commit/8a2d759115892ba95ffd84249242696c1c8d8205))
* skill-trigger 控噪与 advisory 去重 ([ca529e7](https://github.com/MjxUpUp/Forge/commit/ca529e7de187abd986debfdf9a5377e01e435c83))
* skills frontmatter 治理 ([d852444](https://github.com/MjxUpUp/Forge/commit/d852444fdc0acdc6beeb801de77e21af375462ce))
* 门禁漏洞四修 ([2eb3e31](https://github.com/MjxUpUp/Forge/commit/2eb3e31debf2918f0a2765e769fd8e26b8fd13d0))

## [1.42.2](https://github.com/MjxUpUp/Forge/compare/v1.42.1...v1.42.2) (2026-08-24)


### Bug Fixes

* **hooks:** session marker 迁 FORGE_DATA_DIR/markers——MSYS /tmp 只读机器上 NOWARN 去噪静默失效 ([836c897](https://github.com/MjxUpUp/Forge/commit/836c897fddd73c5ff7b07e3b973ec18c361d1aad))
* **qa:** 回顾发现三问题的结构修复——audit 自指豁免/decide 拒写 embed 缓存/hazard confirm --last ([238c282](https://github.com/MjxUpUp/Forge/commit/238c28241ae05827736a1610e22431556b5d51dd))
* **qa:** 复审跟进五项——decisions.md 豁免收窄为根级+仅 DC-10/decide 测试哨兵化/--last Args 测试+优先级说明/竞态披露 ([1a86925](https://github.com/MjxUpUp/Forge/commit/1a869259b1ec62a98076514c99555dfef6436ec5))
* **skills:** DC-10 跟进——3 skill 的 npx 调用改 lockfile 锁定的本地依赖运行形态 ([6fe8c6b](https://github.com/MjxUpUp/Forge/commit/6fe8c6b473b5442b9e875fc9fbb6c736fe33b4b1))
* **skills:** 复审跟进四项——decisions 自回引改连字符形态/preview 改 npm exec 不依赖预置 script/审查污染还原提示/tsc 注释精确化（audit 复扫 0 finding） ([ad137cd](https://github.com/MjxUpUp/Forge/commit/ad137cd82af69b3300a5000d0f734345812b27cf))

## [1.42.1](https://github.com/MjxUpUp/Forge/compare/v1.42.0...v1.42.1) (2026-08-23)


### Bug Fixes

* **act:** 逃生舱cap证据缩放+nudge 14天窗口+历史结论就地迁移 ([62c02eb](https://github.com/MjxUpUp/Forge/commit/62c02ebadc7aa14f194afca1361e0d9413ded8ef))
* **protocol:** 审查-修复-复审闭环补复审规定——多轮盖章 ADVISORY+SKILL.md+生成文案 ([6c54c68](https://github.com/MjxUpUp/Forge/commit/6c54c682c50a84d1e981e215da46ebc1cdee6c11))
* **review-r2:** 复审五项修复——窗口沿内联注释/override Short 谎报/笔误/豁免措辞统一+守卫测试 ([5d62720](https://github.com/MjxUpUp/Forge/commit/5d627205446fe7e28123512f554802f445539faf))
* **review-r3:** 窗口内侧边界注释 1ns→1 秒，与代码 time.Second 一致（第三轮验证 INFO） ([848ca89](https://github.com/MjxUpUp/Forge/commit/848ca89171463144dfc533c62c7b32d8b6ed2dcb))
* **review:** code-review 六项修复——README第6处谎报文案/评分封顶独立性注释/豁免说明补齐/窗口沿契约/权限保留/过时注释 ([7bb65a8](https://github.com/MjxUpUp/Forge/commit/7bb65a8e7382ac8373e6aa99d93f632ce7336fb4))
* **review:** 复审跟进四项——快照增量触发/决策ID回归生成器/死断言/文档同步 ([0059111](https://github.com/MjxUpUp/Forge/commit/0059111c97ee86e0f9c145e577ccaa121c072a7a))

## [1.42.0](https://github.com/MjxUpUp/Forge/compare/v1.41.0...v1.42.0) (2026-08-23)


### Features

* **hostcap:** dsh task-guard advisory 升级为 exit-2 硬阻断（PromoteAdvisory 路径 (b)） ([17fc107](https://github.com/MjxUpUp/Forge/commit/17fc107a1b0afbc2e87813cfc79daedec18c0b7f))


### Bug Fixes

* **pulse-task:** task.json Truncated 透传+证据反编造守卫+前端无证据如实展示 ([c5d0eb7](https://github.com/MjxUpUp/Forge/commit/c5d0eb7809268424e96674d723eae558c4ef4717))

## [1.41.0](https://github.com/MjxUpUp/Forge/compare/v1.40.1...v1.41.0) (2026-08-22)


### Features

* **agentbridge:** 扩接 failure-track/subagent-track 到 cursor+copilot，补 cursor payload 方言适配 ([e54971e](https://github.com/MjxUpUp/Forge/commit/e54971e974d2130ef8e2bebf275186b556c12ffe))
* **hooks:** 接线三观察hook补事件缺口+PreToolUse permissionDecision+Bash tool-track ([a8fd3c6](https://github.com/MjxUpUp/Forge/commit/a8fd3c692dadfecaef7b3ad27bf50038dbea43d5))


### Bug Fixes

* **deferred-batch1:** 延后项批量落地——uninstall codebuddy/kimi-manifest 出口/doctor 未装目标门控/dsh 文档补齐 ([a098b45](https://github.com/MjxUpUp/Forge/commit/a098b454a2d67eafe82244f6590fc9865310bd21))
* **docs,doctor:** 补齐协议文档9个接线hook + doctor新增skills分发审计节 ([c701f6d](https://github.com/MjxUpUp/Forge/commit/c701f6d9dbd37ec874dd6ac75386fac50fc184e6))
* **skillseval:** effectiveness 被动 join 修复非 git 数据目录解析+测试判别力（评审 M 级两项） ([4b53b85](https://github.com/MjxUpUp/Forge/commit/4b53b85093578dc276f6d9049625b7b0bf688b23))

## [1.40.1](https://github.com/MjxUpUp/Forge/compare/v1.40.0...v1.40.1) (2026-08-21)


### Bug Fixes

* **update:** forge update 感知 npm 安装通道——npm 用户改查 npm registry 并重定向到对应包管理器 ([#18](https://github.com/MjxUpUp/Forge/issues/18)) ([7c66a1a](https://github.com/MjxUpUp/Forge/commit/7c66a1ab62da2a3890d03e67343573853b587586))

## [1.40.0](https://github.com/MjxUpUp/Forge/compare/v1.39.1...v1.40.0) (2026-08-21)


### Features

* **git-sync:** forge project sync init/push/pull/status——git 传输通道（forge-sync 固定分支、nodes/&lt;node_id&gt;/&lt;key&gt;/ 前缀只写自己、bundle 覆盖式推送、pull 复用 project import 账本幂等）——Phase 1 传输层 ([ae14ee5](https://github.com/MjxUpUp/Forge/commit/ae14ee5406d3c8bbb9b1081e1bf2a37f06a59ef8))
* **hlc:** 混合逻辑时钟——Timestamp(Wall+Logical)/Compare/Parse + Clock.Now/Observe，回拨下单调、并发唯一——多机器 Phase 0，sync-convergence §3 的 LWW 决胜键 ([7370fbe](https://github.com/MjxUpUp/Forge/commit/7370fbeea0b7b1871105646f9155ea86faa7dbad))
* **nodeid:** 节点身份地基——ed25519 密钥对，node_id=公钥指纹（fnode_&lt;32hex&gt;），rotation_chain 格式预留，forge node show（私钥不出展示面）——多机器 Phase 0，设计见 docs/design/node-identity.md ([a587e88](https://github.com/MjxUpUp/Forge/commit/a587e88b6fc9778af363a6c201eca612e3e93c0f))
* **nodestamp:** 事件打戳——Stamp(node_id/seq/ts_hlc/sig) 内嵌 checklog/toolusage/act/sessions 四收口点，node-seq 跨进程计数器（O_EXCL 锁+persist-before-use+原子落盘），fail-open 零戳，损坏禁用防 seq 复用——node-identity.md §4 ([acbdd3a](https://github.com/MjxUpUp/Forge/commit/acbdd3ac5895d931ebc5c4cc4091673005a76309))
* **pulse-node:** Pulse 事件流渲染 node 归因——FeedEvent.Node（conclusion/skill-trigger 携 nodestamp，task-start 携租约持有者，存量无戳记录零字段）+ 前端 node-chip（fnode 短标签）——Phase 3 ([2ad2a5a](https://github.com/MjxUpUp/Forge/commit/2ad2a5a0e06e25af817fa3d262b46578a1699d7e))
* **task-convergence:** MergeTaskStateSync 收敛层——规范排序+确定性决胜（交换律/幂等字节一致）、ReviewRounds 并集防采纳覆盖、SessionLinks/History 单侧重复归一、40 种子 property test + 双 DataDir 双向合并测试——sync-convergence §2 B 类 ([f2b916c](https://github.com/MjxUpUp/Forge/commit/f2b916c24c7b896510d5a4f7eab881542c371e23))
* **task-lease:** 跨机任务租约——Lease(holder/ts_hlc/ttl/fencing)+start 自动认领(fail-open)+gate 他机活跃租约 advisory+合并 fencing 高者胜——sync-convergence §4 个人档 ([905133f](https://github.com/MjxUpUp/Forge/commit/905133f6282d5abf4adb90d575e637615a951024))
* **trust:** 信任层——trust.json store（TOFU+0600+原子写）+ forge trust list/add/remove/require-signed + bundle .sig sidecar 签名（export/sync push 无条件签）与导入验签（invalid 恒拒/团队档未签拒/未知签名者告警）+ 双机 sign→verify e2e——node-identity §3 ([2484aa2](https://github.com/MjxUpUp/Forge/commit/2484aa2869bc6071434d369ed1fdf49c183ce60f))


### Bug Fixes

* **ci:** 分支三平台 CI 首跑的 Windows 失败修复 ([a155235](https://github.com/MjxUpUp/Forge/commit/a155235501936b7c48575c5edab795849c14a5de))
* **git-sync:** skillRefAllowlist 收编 forge-sync（同步通道固定分支名，非 skill） ([939cfd6](https://github.com/MjxUpUp/Forge/commit/939cfd6aeef7d2fd6788fd184b34b819e1ebcba3))
* **git-sync:** 审查跟进——ls-remote 区分无分支/不可达（init 真 fail-fast 且不写半成品绑定）、push 一次重拉重试（并发非快进收敛）、commit 限定前缀+扫 tmp 残骸+关 gpgsign/hooks、pull 逐节点容错+ValidNodeID 形态检查、补不可达 init/坏节点跳过测试 ([6d3ef9a](https://github.com/MjxUpUp/Forge/commit/6d3ef9a1baaa8e44e27d798b21a7832b7bca5122))
* **hlc:** 审查跟进——Logical 饱和推进 Wall 替代 int32 回绕（静默破单调+不可解析）、String 全定宽（%019d.%010d，字符串序==Compare序全值域成立）、Parse 拒非数字/前导+、补溢出与等墙 recv 分支测试 ([a52c618](https://github.com/MjxUpUp/Forge/commit/a52c6185a72395d5b374d720de657bb006f0fe37))
* **nodeid:** 审查跟进——Save 原子化（CreateTemp+fsync+rename）、CheckConsistent 拒 null rotation_chain、Load 收紧宽松权限、ValidNodeID 手写校验对齐 fpid 风格、私钥值级防泄断言、补篡改/损坏分支测试 ([c6cedda](https://github.com/MjxUpUp/Forge/commit/c6ceddaa87c21b89df24b887d692d85ac3c025f1))
* **pulse-node:** 复审跟进——task-start node 复用「过期即自由」单一规则（Lease.ActiveAt，崩溃机器 stale 认领不留看板）、测试补有效/过期双边界+wire 级 omitempty 断言+吞错修复、UI title 区分「当前持有/来源机器」语义 ([d080cc3](https://github.com/MjxUpUp/Forge/commit/d080cc3464a9784e72b1aca2cd5ee7d641f461e2))
* **review-followup:** dsh 交付复审 14 项发现修复——静默丢推送/字面量\n/TOCTOU/验签前置/可观测性 ([8f28ebc](https://github.com/MjxUpUp/Forge/commit/8f28ebc078db3ca8d4b7cf680f0188a99d0afbb3))
* **task-convergence:** 复审跟进——completionCanon 剔除并集字段 ReviewRounds + 纳入 AcceptanceForeign（同命令异标志=不同块）、标量验收决胜键含标志、dedupByKey 保持 nil/空表示（防决胜键跨轮翻转）、property test 补 stepwise 轮次收敛断言 ([7f91a4f](https://github.com/MjxUpUp/Forge/commit/7f91a4fe69ae2465cd09c29cfbbe11ca8fea77d5))
* **task-convergence:** 审查跟进——History 改全内容并集（保住重试 provenance，时间序保 lastGateAt 锚）、review 锚只随完成块走（防跨块混杂）、块决胜非空优先、AcceptanceForeign 随采纳块、SessionLinks 冲突 Sync 路径确定性裁决、不可信路径恢复本地权威、property test 共享 ID 池+全字段 op ([8497c30](https://github.com/MjxUpUp/Forge/commit/8497c30b15757165a3b2a8ba4f4f2031b906b697))
* **task-lease:** 复审跟进——resume/attach 接手方认领租约（advisory 追踪实际工作机）、同值 fencing 破平带 oracle 定向测试（双机同时认领收敛） ([d878232](https://github.com/MjxUpUp/Forge/commit/d878232ed1665a8901e9854811bc54d36427a0ff))
* **trust:** 复审跟进——篡改 e2e 分两层钉（unpack 完整性层 + 重打包挂旧 sidecar 的签名层真拒）、pull 失败节点汇总为 pull 级错误（策略拒收不再静默 exit 0）、团队档签名失败硬错误、.sig 原子写、trust CLI 面测试、设计文档实现校正 ([ffb4e7c](https://github.com/MjxUpUp/Forge/commit/ffb4e7c1d8819bbcb1a24253044e61cc0c26aba9))

## [1.39.1](https://github.com/MjxUpUp/Forge/compare/v1.39.0...v1.39.1) (2026-08-20)


### Bug Fixes

* **dsh/opencode:** win32 spawn 走 cmd.exe 解析 npm .cmd shim——修掉全门禁静默失效 ([1a0c1a4](https://github.com/MjxUpUp/Forge/commit/1a0c1a48cfb3fac0dc132b0ca9a8fd8e799967c8))
* **dsh:** @agent_forge/forge-dsh 0.1.1 随发版火车发布——Windows spawn 修复到达插件用户（release.yml 幂等发布，读插件自身 version）

## [1.39.0](https://github.com/MjxUpUp/Forge/compare/v1.38.2...v1.39.0) (2026-08-20)


### Features

* **agentbridge:** 接入 DeepSeek Harness 插件生态（plugins/forge-dsh） ([0a4b7f3](https://github.com/MjxUpUp/Forge/commit/0a4b7f38938fbc70d339359d71513e0c7c8d077f))


### Bug Fixes

* **release:** 首发前审查跟进——license 对齐 Apache-2.0 + forge-dsh dry-run 门禁 ([0e22f66](https://github.com/MjxUpUp/Forge/commit/0e22f66da5d3df9fdb3d82d7ace1d1357bcf6b85))

## [1.38.2](https://github.com/MjxUpUp/Forge/compare/v1.38.1...v1.38.2) (2026-08-20)


### Bug Fixes

* **agentbridge:** kimi plugin manifest 恢复 skill-trigger 全事件绑定——看板 kimi 任务仅 5 事件 ([b4a0a27](https://github.com/MjxUpUp/Forge/commit/b4a0a27b429b366da030aba08e7ce2da26d39a7b))

## [1.38.1](https://github.com/MjxUpUp/Forge/compare/v1.38.0...v1.38.1) (2026-08-20)


### Bug Fixes

* **cli:** sync/migrate/project help 加跨机器迁移交叉指引 ([a15a41a](https://github.com/MjxUpUp/Forge/commit/a15a41a38e03042373830de25f8357e985db6395))
* **skillsqa:** 修正安全规则数自述 22→21（实计 21=18 对齐 audit.py+3 本地） ([fb622c7](https://github.com/MjxUpUp/Forge/commit/fb622c7111193df78d64d93a5d9cce417ddd6c03))

## [1.38.0](https://github.com/MjxUpUp/Forge/compare/v1.37.0...v1.38.0) (2026-08-19)


### Features

* **ci:** release-please 接管发版——Release PR 自动 bump/tag，dispatch 串联 release.yml ([e73609c](https://github.com/MjxUpUp/Forge/commit/e73609c5c47fd3b98235d5ec97939034c03b0d7f))


### Bug Fixes

* **ci:** release-please workflow 被 GitHub 静态拒绝——secrets 上下文移出 steps.if ([735bab7](https://github.com/MjxUpUp/Forge/commit/735bab7e5b8d334d7dc600b95f955e03a8b77cdb))
* **ci:** 审查修复——守卫锚定断言/删always-update/串行化/先算后写 ([e68f28a](https://github.com/MjxUpUp/Forge/commit/e68f28a2a5edfa194ba98cc8341f5cf1058f9000))
