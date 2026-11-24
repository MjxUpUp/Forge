# 澄清：边界与非目标

change: `doc-review-fix`

## 边界（做什么 / 不做什么）

边界：只按第二个模型独立评审逐条核实后修文档事实（README 质量检查范围/评分口径/示例参数、positioning 验收入口、task-tree-guard 评分语义、wedge-demo 唯一来源、compat-commitments 版本与承诺范围、standards-crosswalk 锚点与 toollog/bundle、zh 时间线与产物数、npm 命令表）。授权来自用户 m03615「修复这些所有的文档问题」与 m03893「claude cli codex cli 我试过都能用…」（要求真用可用模型做独立评审）。非目标：不改代码行为、不新增/删除命令、不处理 evalkit 可达性等代码面残留、不重写整篇文档；评审误报（--project no-op、change gate、安装标题、compat 归档链接）不照改，只在交付说明里注明。

## 非目标

- 不改代码行为，不新增/删除命令；只改文档措辞与事实陈述。
- 不追究评审提到的「evalkit 仍可从 review.go 到达」这类代码面残留（属另一批）。
- 不重写整篇文档，只做定点修正。

人确认：2026-11-24T03:10:54Z
