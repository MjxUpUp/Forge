# design-artifact-standards · Forge 集成

仅 forge 项目适用。skills 零反向依赖契约（CONVENTIONS §13）下，本 skill 的 forge 集成知识由 forge 侧维护。

- 安装依赖提示：frontmatter `requires: doc-review` 由 `forge skills install` enforce 提示——单装本 skill 打印「requires doc-review 但本次未同装」警告（不阻断，仅提示）。
- 自动衔接：task-verify hook 的 `inferDesignPhases` 按已写文件路径推断设计阶段（PRD 需放 `docs/prd/`、API 需路径含 `openapi/api/proto` 等），code-review-gate 审查期据此加载本 skill references/ 下同一份 phase-*.md 复核（6 环节全覆盖；路径不匹配时回退通用清单）。
- 枚举同步：6 个环节枚举与 `internal/taskpipeline/phase_detect.go` 的 `allDesignPhases` 保持一致；phase-*.md 路径/文件名/环节增删变更时，code-review-gate 的 phase 加载表需同步更新。
- 文档产物空转检测：`forge docs lint`（D1-D7，机器可查）。
