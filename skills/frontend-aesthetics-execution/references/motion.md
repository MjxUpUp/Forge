# 动效曲线 token 与微交互范式

SKILL.md 阶段 3.1 的自建模板与细节参数。动效库选型表（Rive/Spline/R3F）见主文件阶段 3.2，reduced-motion 实现模板见主文件阶段 4，此处不重复。

## 动效曲线 token（四家都未公开，需自建）

2026 调研发现四家（Linear/Vercel/Raycast/Arc）design token 文件**均无 motion/easing token**——这是 token 体系的结构性缺口。建议自建：

```css
@theme {
  /* 进入动画 — ease-out-expo（Framer Motion 默认）*/
  --ease-enter: cubic-bezier(0.16, 1, 0.3, 1);
  /* 状态切换 — spring 而非 keyframe tween */
  --spring-stiffness: 300;
  --spring-damping: 30;
  /* hover/微交互 — 快速 */
  --duration-micro: 0.15s;
  --ease-micro: cubic-bezier(0.4, 0, 0.2, 1);
}
```

**反 AI 信号**：① 避开 `linear` 和默认 `ease`（最算法）；② spring 给"质量感"；③ 微交互 duration 0.15-0.2s。

## 微交互范式（四家收敛规律）

- **hover**：duration 0.15s，背景色微变（surface 升一档）或 scale 1.02
- **focus**：ring（`box-shadow: 0 0 0 2px var(--color-ring)`），不用 outline
- **状态切换**：spring（stiffness 300/damping 30），不用 tween
- **页面转场**：duration 0.3s ease-out-expo，淡入 + 轻微位移（y: 8px → 0）
