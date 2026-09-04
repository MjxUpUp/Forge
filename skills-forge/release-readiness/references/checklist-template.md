# checklist.md 产出格式

发布决策的归档产物（gate-8 产出物）：清单 + 命令输出 + 决策结论。

```markdown
# Release Readiness: <project> <NEW_VER>

**决策结论**：✅ GO / ⚠ GO-WITH-RISK / 🚫 NO-GO
**发版窗口**：YYYY-MM-DD HH:MM ~ HH:MM (TZ)
**决策人**：<name>

## Mandatory（必须全绿才 GO）
| 项 | 命令 | 结果 | 状态 |
|---|---|---|---|
| M1 版本号一致性 | `grep ...` | 4/4 出处一致 | ✅ |
| M2 CHANGELOG | `head -20 ...` | 含 [X.Y.Z] + Breaking 段 | ✅ |
| M3 构建产物 | `npm run build` | exit 0, 1.2MB (+5%) | ✅ |
| M4 迁移 + 回滚 | staging up+down | 双向 OK | ✅ |
| M5 secrets | grep sk-/ghp_ | 0 命中 | ✅ |
| M6 文档一致性 | docs-consistency-guard | 守卫全绿 | ✅ |
| M7 smoke | staging e2e | 关键路径 8/8 | ✅ |

## Recommended
| 项 | 状态 | 说明 |
|---|---|---|
| R1 回滚预案 | ✅ | RUNBOOK.md 已演练 |
| R2 已知问题 | ⚠ | issue #123 未修，发布说明已列 |
| R3 观测 | ✅ | 仪表盘 + 错误率告警 |
| R4 通知 | ✅ | 邮件已发 |
| R5 灰度 | ⚠ | CLI 二进制无法灰度，已内部 dogfood |

## 已知风险（GO-WITH-RISK 时必填）
- R2: issue #123 ... — 影响 ... — 降级策略 ... — 回滚触发条件 ...

## 签字
- 发布人：<name>
- 干系人 ack：<name>（GO-WITH-RISK 必填）
```
