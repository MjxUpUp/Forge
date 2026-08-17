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

## 盲测迭代纪律（--blind dispatch）

用 `forge skills eval-cases --skill X --blind` 跑全库路由盲测时，三条纪律：

1. **改 description 后必须全量重跑**——盲测是全库竞争，改一个 skill 的 description 会让相邻 skill 的路由结果回归，只重跑被改的 skill 会漏掉邻域退化。
2. **歧义时改 eval 不改 skill**——case 本身有歧义时修 case（防过拟合：为通过一个歧义 case 而把 description 写得越来越长，是在训练路由器背答案）。
3. **borderline 误触发记录但不调掉**——边界例的误路由是路由器的真实能力边界，记录下来供趋势观察；把边界例从集里删掉会让指标虚高。
