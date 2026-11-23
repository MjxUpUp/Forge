# 澄清：边界与非目标

change: `core-cleanup`

## 边界（做什么 / 不做什么）

边界：① change 侧 -run 空匹配（no tests to run）判负，判据口径抽到 internal/criteria 两侧共用；② 删 testcoverage 死门禁函数与 golden、删无调用者死文件（workspace doctor、registry audit）、删已摘除的 skills 脚手架包（cliskills + skillseval/skillsdecisions/skilltrigger/skillscanonical/skillsqa/skillmetrics/skillintegrate/skillsdist/skillsfm + 两个 embed）；③ 清掉用户可见文案里指向 core-only 已删命令的提示，并加守卫测试钉死。非目标：不改活门禁语义、不删 skills/ 内容目录、不动 act/disposition.go、不改封印格式、不重写历史 change。人确认依据：用户 2026-11-24 指示「这些发现的问题都得清理 / 从我们本次目标的本意出发…验收」（m02939），授权的正是「已发现的问题全部清理」这一范围。

## 非目标

- 不改任何仍活着的能力语义（门禁判定口径、change 生命周期、task 状态机）。
- 不删 `skills/`、`skills-forge/` 下的技能内容（taskpipeline 的 skill-decisions 门禁按路径读 `skills/<name>/SKILL.md`）；只删它们的 embed 包装。
- 不动 `internal/act/disposition.go`（零调用但仍在 act 包内，需要单独判断是否属于经验层）。
- 不改 evidence/archive 的封印格式，不重写历史 change。

人确认：2026-11-23T17:26:26Z
