# Changelog

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
