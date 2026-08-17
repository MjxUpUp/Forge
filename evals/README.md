# evals/ — skill eval 数据资产

本目录是 skill eval 闭环中**进 VCS、可评审**的那部分数据。机器产物（runs/baselines）默认落 `~/.forge/evals`，CI 上用 `forge skills --dir evals ...` 指向本目录。

## 目录语义

| 目录 | 语义 | 谁写 |
|------|------|------|
| `golden/<skill>/cases.json` | 人工策展黄金 case 集：真实用户话语改写（非 description 自我派生），进 VCS 可评审 | 人工 |
| `mutex/cases.json` | 跨 skill 互斥 case 集（SKIP 让渡边派生的路由对抗样本） | `forge skills mutex-gen` |
| `runs/`、`baselines.json` | run 记录与回归基准（机器产物，仅 CI 场景提交） | `eval-record` / `eval-baseline` |
| `checklists/` | eval-gen 生成的 markdown 清单 | `forge skills eval-gen` |

## golden 集约定

- **格式**：复用 CaseSet（`{"skill", "cases": [...]}`），case 带 `origin: "curated"`。
- **ID**：统一 `g-` 前缀（如 `g-<skill>-t1`），与派生 case 的 sha1[:12] ID 域隔离，两套来源可共存可追溯。
- **不带 desc_hash**：策展 case 锚定真实话语而非 description——description 变更不会让 golden 集过期（submit 的 stale 校验只对派生 case 生效）。
- **加载合并**：`LoadCases`/`LoadCaseSet` 加载时 golden 优先、派生补充；同 ID golden 胜出。
- **策展要求**（区别于派生 case）：真实话语改写（不是 description 原词）；每 skill ≥3 正例 + ≥2 负例 + 1 边界例（近让渡边的 prompt，ID 用 `-b1`）。
