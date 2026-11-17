import io
def rw(p, pairs):
    s = io.open(p, encoding="utf-8").read()
    for old, new in pairs:
        assert old in s, (p, old[:60])
        s = s.replace(old, new)
    io.open(p, "w", encoding="utf-8", newline="\n").write(s)

rw("scripts/checks/no-removed-checks.sh", [
 ("grep -vE '已删除|已移除|已下线|已退役|不再存在'", "grep -vE '已删除|已移除|已下线|已退役|已随'"),
 ("  set -- README.md READMEs/README.zh-CN.md npm/README.md CONTRIBUTING.md RELEASE.md skills \\\n    docs/reference internal/change/templates internal/skillintegrate/notes skills-forge",
  "  set -- README.md READMEs/README.zh-CN.md npm/README.md CONTRIBUTING.md RELEASE.md skills \\\n    docs/reference internal/change/templates internal/skillintegrate/notes skills-forge\n  # decisions.md 是历史决策日志，允许记录当时存在的机制。\n  set -- $(printf '%s\\n' \"$@\")"),
])

rw("internal/skillintegrate/notes/doc-review.md", [
 ("1.88 激进减法删除了 `forge task doc-review` 与 doc gate，交付单不再披露分数，也不再有门禁代你拦文档。",
  "`forge task doc-review` 与 doc gate 已删除（1.88 激进减法）：交付单不再披露分数，也不再有门禁代你拦文档。"),
])

rw("internal/skillintegrate/notes/code-review-gate.md", [
 ("评审者先按文件路径判断设计阶段（inferDesignPhases 已随 task-verify 删除，不再自动落盘）。",
  "评审者先按文件路径判断设计阶段（inferDesignPhases 已随 task-verify 删除，不再自动落盘）。"),
 ("cheat-scan 已随 1.88 激进减法删除（不再有机械预扫）；历史上它机械扫任务新增行的 `type-suppression`",
  "cheat-scan 已随 1.88 激进减法删除（不再有机械预扫）；历史上它扫描任务新增行的 `type-suppression`"),
])

rw("internal/skillintegrate/notes/design-artifact-standards.md", [
 ("方案引用由 review 人工核对（`forge task artifact --check` 已随 1.88 激进减法删除）；",
  "方案引用由 review 人工核对（`forge task artifact --check` 已随 1.88 激进减法删除）；"),
])

rw("internal/skillintegrate/notes/dev-workflow.md", [
 ("但它现在是给人和评审看的声明，不再有 task-verify 去比对差集：",
  "但 task-verify 的差集比对已随 1.88 删除；scope 现在是给人和评审看的声明："),
])

rw("skills/doc-review/SKILL.md", [
 ("（不要落成仓库内的 `.md`，会被 doc gate 当文档）", "（不要落成仓库内的 `.md`，会被当成交付文档）"),
])

rw("skills-forge/skill-evolution/SKILL.md", [
 ("## 与 task-verify 的衔接", "## 与评审的衔接"),
 ("改 skill 源码时，task-verify gate 会 advisory 提醒「记决策」（skill-decisions-advisory）：",
  "改 skill 源码时不再有 task-verify 的「记决策」advisory（已随 1.88 删除）；是否补 decisions.md 由评审者判断："),
])

rw("skills-forge/skill-routing/routes.json", [
 ('"doc gate 回检"', '"文档回检"'),
])

rw("RELEASE.md", [
 ("| **drill** | `eval wedge-drill` + `eval artifact-drill`——行为级冒烟（发布物必须真实跑过） | `test` |",
  "| **drill** | `eval wedge-drill` + `change check`——行为级冒烟（发布物必须真实跑过） | `test` |"),
])

rw("npm/README.md", [
 ("每个开发任务走 3 道门禁：实现 → 验证 → 完成", "每个开发任务走两道门禁（实现、完成）并实跑验收"),
 ("每个开发任务自动走 3 道门禁", "每个开发任务走两道门禁并实跑验收"),
])

rw("README.md", [
 ("fast 仍写 `goal + proposal + acceptance 三份产物外加 clarify`", "fast 仍写 `goal + proposal + acceptance 三份产物外加 clarify`"),
])

rw("skills/merge-release-choreography/SKILL.md", [
 ("正确顺序：三门禁 → commit → complete", "正确顺序：实现门禁 → 验收 → commit → 审阅 → 完成门禁 → complete"),
])
print("ok")