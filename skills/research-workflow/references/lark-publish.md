# Lark Publish — 飞书发布规范

本文件是 `research-workflow` Phase 6 的详细命令与异常处理。主文件 SKILL.md 给主控要点。

## 前置：认证

首次使用或遇 permission denied / scope 错误，先走 lark-shared skill 完成认证登录。
发布动作需要 wiki 节点创建权限 + docs 写入权限。

## 步骤 1：创建主报告节点（父节点）

```bash
lark-cli wiki +node-create --space-id 7642344528036252853 --title "{报告标题}"
```

从返回的 JSON 中提取：
- `node_token` —— 用于 wiki 链接 `https://my.feishu.cn/wiki/{node_token}`，**后续作为父节点 token 创建子文档**
- `obj_token` —— 用于 docs 写入

## 步骤 2：写入主报告内容

```bash
cd {run_dir}
lark-cli docs +update --api-version v2 --doc {obj_token} --command overwrite --doc-format markdown --content "@report.md"
```

**关键注意**：
- `@file` 语法必须先 `cd` 到文件所在目录
- Windows 环境如遇编码问题，设 `$env:PYTHONIOENCODING="utf-8"`（PowerShell）
- bash 环境设 `export PYTHONIOENCODING=utf-8`
- `--command overwrite` 会覆盖整篇；写入前确认 report.md 是最终版

## 步骤 3：创建维度子文档（dive_NN 作为主报告子节点）

每个维度的深度产出（`{run_dir}/dive_NN.md`）**必须作为主报告的子文档发布**，形成调研报告目录树：

```
主报告（总览 + 跨维洞察 + 行业影响总结）
├── 维度1 · <维度名>
├── 维度2 · <维度名>
...
└── 维度N · <维度名>（每个含 Current State / Key Evidence / Tensions / Sources Cited）
```

主报告作为总览与决策入口，维度子文档提供可追溯的深度证据与一手数据。

**3.1 并行创建 N 个子节点**（复用步骤 1 的 `node_token` 作父节点）：

```bash
for i in 1 2 3 4 5 6 7; do
  TITLE="维度${i} · <维度名>"  # 维度名取自工序 2 切分结果
  lark-cli wiki +node-create --space-id 7642344528036252853 \
    --parent-node-token {主报告 node_token} --title "$TITLE"
done
```

每个子节点返回独立的 `node_token` + `obj_token`，**逐一登记到映射表**（建议写 `{run_dir}/publish_map.md`）：

```
维度1 · 市场规模与资本流向    node_token=At0...  obj_token=S5Z8...
维度2 · 芯片与算力硬件         node_token=SJb...  obj_token=Vz4...
...
```

**3.2 逐个写入 dive 内容**（每个子节点的 obj_token + 对应 dive_NN.md）：

```bash
cd {run_dir}
lark-cli docs +update --api-version v2 --doc {子文档1 obj_token} \
  --command overwrite --doc-format markdown --content "@dive_01.md"
# 重复至 dive_NN.md
```

**强制要求**（这一步不可跳过）：
- **完整调研 / 并行调研模式**：所有 `dive_NN.md` 全部发布为子文档
- **增量调研模式**：本轮新建的 `dive_NN.md` 发布；融入旧报告的可不单独建子文档
- **文件 only 模式**：`file_analysis.md` 等同于维度产出，同样作为子文档发布
- 跳过此步 = 用户只能看到总览而无法回溯每个维度的原始证据，违反 "Raw evidence required" 原则

**并行优化**：子节点创建与内容写入可与步骤 2 主报告写入并行执行（互不依赖），推荐同一批 bash 调用。

## 步骤 4：返回结果

向用户报告（不暴露内部工序编号，用自然语言）：
- ✅ 报告标题
- 📎 主报告链接 `https://my.feishu.cn/wiki/{node_token}`
- 🌳 **维度子文档目录树**（逐条列出标题 + 链接），让用户一眼看到可下钻的深度证据
- 📁 本地文件 `{run_dir}/report.md`
- 📊 维度数量 / 子代理数量 / 总搜索次数

## 异常处理

| 异常 | 处理 |
|---|---|
| lark-cli 命令失败 | 检查认证状态（走 lark-shared）；确认 space_id 正确 |
| 子代理超时/失败 | 标记该维度为"调研失败"，在报告中说明，不阻塞其他维度 |
| 报告过长（>20000字） | 先生成完整版 report.md，再提供精简版（<8000字）单独发布 |
| 飞书上传失败 | 报告本地文件路径，提供手动上传命令 |
| docs +update 报格式错 | 确认 report.md 是纯 markdown；去掉 report.md 开头的 frontmatter（---）再写 |
| 维度子文档创建/写入失败 | 单个子文档失败不阻塞其他维度：记录失败维度名 + obj_token，继续发布其余子文档；最后报告失败清单与本地 dive_NN.md 路径供手动补发 |
| --parent-node-token 报无权限 | 主报告 node_token 未生成子节点权限；确认账号对目标 wiki 空间有 editor 以上权限，或改在根目录创建后手动移动到主报告下 |
| **子文档标题变 "Untitled" 或显示文档内部标题** | **飞书 wiki 节点标题会从 docx 文档的第一个一级标题 `#` 自动同步**（node-create 的 --title 只是初始值，docs +update overwrite 写入后会被文档首行覆盖）。若 dive_NN.md 首行是 `##` 二级标题 → 同步不到 → 显示 Untitled；若首行是 `# 内部标签文字`（如 "工序F/Dimension 01/insight.md"）→ 标题同步成内部标签，不符合对外措辞规范。**修复/预防**：发布前确保每个 markdown 首行是 `# {期望的对外节点标题}`（如 `# 维度2 · AI编码产出偏差根因`），避免内部工序号/文件名出现在一级标题；写入后用 `wiki +node-list --parent-node-token {父}` 核对所有子节点 title 非 Untitled |

## 对用户的话术

不要暴露内部工序编号。用自然语言：
- "开始调研，分 {N} 个维度并行搜索"
- "维度 1/3 完成，继续等待..."
- "全部完成，正在上传飞书..."
- "搞定。📎 {飞书链接}"

## 飞书知识库信息

- 空间地址：https://my.feishu.cn/wiki/KDUfw7MNtiIduZk3awpcelI0nRb
- space_id：7642344528036252853
- 所有调研报告统一发布到此知识库根目录
