# judge-audit 采样协议（首轮 2026-09-04）

## 样本构成（8 份）

- 6 份真实文档：docs/design/ 五篇 + golden README（全部经 `forge docs lint` 0 硬失败）
- 2 份构造坏样本：bad-no-evidence.md（无证据结论/空泛通过断言）、
  bad-restate-diff.md（纯复述改动）

## 双侧协议

- **human 侧（ground truth）**：维护者判定 6 pass / 2 fail（辅助锚：
  `forge docs lint`——注意首轮实测发现 lint 对"无证据结论"语义弱，两份坏样本
  只触发建议不触发硬失败，human 判定因此以人工为准）。
- **judge 侧**：维护者会话内的 LLM 评审者按 docgate 可读性 rubric 独立评 3 轮
  （0-100，threshold=75）。judge 具体型号/版本记录于本地运行日志，公开仓库
  脱敏（脱敏纪律：公开工件不落维护者模型栈/内部路径/外部系统名）。

## 首轮结果（scores-v1.json）

- κ=1.00（阈值 0.60）——judge 与 human 完全一致，docgate 75 分阈值当前可靠。
- 重放极差 2-5 分（阈值同侧噪声）；首轮机制缺陷（同侧抖动误报"自洽性不足"）
  已修正为"跨阈值才报"，并加单测钉住。
- **局限（诚实边界）**：judge 与 human 同源（同一维护者驱动的 LLM），κ 有
  同源虚高风险；样本 8 份偏小。下一轮应引入独立评审者与 15+ 份样本。

## 节奏

季度复评 + rubric 措辞变更后立即复评（κ<0.6 时 docgate BLOCKED 决策自动降级
ADVISORY——forge eval judge-audit 的裁决契约）。
