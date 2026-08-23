# Changelog

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
