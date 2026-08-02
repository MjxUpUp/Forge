# 防注水自检（避免 skill 写得比实际做法松）

新建或修改 skill 后，强制跑一次防注水扫描，检出三类"skill 声称有校验但无具体可执行方法"的注水：

| 类型 | 特征 | 示例 | 修复 |
|---|---|---|---|
| 弱校验措辞 | "人工校验/肉眼对比/大致核对/应该通过" | `生成后必人工校验` | 换成可执行命令 + 量化阈值（`跑 jscpd 重复率 >15% 警告`）|
| 门控无方法 | 有"门控/必跑/强制"关键字但附近无具体命令/工具 | `门控：导入后验证还原度` | 补命令（`用 get_node_dsl 读回，跑 snapshot-verify check`）|
| checklist 无命令 | `- [ ] 编译是否通过` 无配套命令 | `- [ ] 编译是否通过` | 每项配具体命令 + 通过标准（`cargo build --all-targets, exit 0`）|

## 命令

```bash
# 全仓扫描（CI / 提交前必跑）
bash references/skill-anti-degradation-check.sh

# 扫描单个 skill（开发时自检）
bash references/skill-anti-degradation-check.sh <skill-name>
```

## 退出码

- `0` = 干净，无注水点
- `1` = 发现可疑项，需逐项检查（脚本优先检出不漏检，少量 false positive 需 human review 最终判定）

## 已知 false positive 模式（脚本会标记但 human reviewer 判定为正向引用）

- Rationalization 表格中的反例引用（`| "xxx 应该通过" |`——这是在堵借口，不是注水）
- "核心原则/红线"段中作为反例的弱措辞（"**红线**：用'应该通过'代替实际运行"——这是在说不能用它）
- Inversion/Pipeline 门控（"阶段 0 确认前不要进阶段 1"——不需要工具）

脚本标记后 human reviewer 逐项判定，以上三类直接标"正向引用，跳过"即可。

## 脚本文件

[references/skill-anti-degradation-check.sh](skill-anti-degradation-check.sh) — 三类检测的完整实现，可直接在 CI 或 pre-commit hook 中调用。

真实注水案例（project-acceptance 维度 3 / code-review-gate 步骤 5）的修复手法对应上表。
