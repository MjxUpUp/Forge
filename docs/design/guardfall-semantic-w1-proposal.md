# W1 Proposal：为什么是语义分词层而不是别的

## 提案

hazard-guard 的 PreToolUse 检测从「原始串模式匹配」补强为「canonicalize-then-check」
语义分词层（bash/awk 内嵌实现，零新增进程/hook 条目）。

## 备选与取舍

- **Go 侧检测器（forge 子进程判定）**：每个 Bash 工具调用多一次 forge 进程拉起
  （~50-100ms），直接加重用户头号痛点「hook 太多太重」——否。
- **只加模式规则修补五类绕过**：GuardFall 的核心教训是「三十个模式的 guard 和三个
  模式的以同样比率倒下」——denial-list 迭代修补在结构上追不上词法混淆——否。
- **事后 file-sentinel 单层兜底**：对仓库外副作用与不可逆操作失明，且 PreToolUse
  的 HITL 确认链是唯一能「问人」的时点——不充分，作为第二层保持不变——部分采纳。
- **bash/awk 内嵌语义分词（本方案）**：替换式补强，不加权重；词法层不可判定的
  形态 fail-closed 交既有 confirm 链（HITL），与 GuardFall 幸存者 Continue 的
  tokenize-then-evaluate 架构同构。

## 验收判据

事前声明（evidence-based）：五类绕过 golden 全拦截 + 既有行为契约（339 行
hazard_script_test + 2 条既有 golden）零回归 + staticcheck/deadcode 双零 +
hook 接线规模棘轮不增。全部可执行、已在任务内登记/落地。
