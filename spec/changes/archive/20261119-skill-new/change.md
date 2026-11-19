# Change：skill 资本化

## 改动

1. 新增 `internal/skillcap`：`Draft`、`Validate`（id 字符集与三段必填）、`Render`（frontmatter + 正文）、`Write`（skillsDir 与 id 双重越界守卫 + 覆盖守卫）。
2. 新增 `forge skill new <id>`（`internal/cli/skill.go`）：`--description/--when/--lesson/--evidence/--dir/--force/--json`，默认写 `skills/<id>/SKILL.md`。

## 影响

- 命令面 +1（compat 重钉）；只写文件、不触门禁判定。
- 覆盖默认拒绝：skill 是长期资产，静默覆盖会抹掉别人的修订。

## 验证

- `internal/skillcap` 单测：渲染内容、非法 id/缺失字段、覆盖拒绝与 --force、`--dir` 越界。
- `forge skill new` 在临时仓库实跑一份并检查文件内容。
