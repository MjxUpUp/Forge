#!/bin/sh
# 守卫：forge 写进仓库的 agent 协议必须是可执行指令，且不再宣称已删除的 hook 执法。
# POSIX sh（dash 也能跑）：acceptance 用 `sh` 调用本脚本。
set -eu
f="internal/change/templates/agents-protocol.md"
[ -f "$f" ] || { echo "缺模板：$f"; exit 1; }
grep -q '不要把 `forge next` 当默认第一步' "$f" || { echo "模板缺少「不要把 forge next 当默认第一步」硬规则"; exit 1; }
grep -q '## Task 流程' "$f" || { echo "模板缺少 Task 流程段"; exit 1; }
grep -q '门禁是命令，不是 hook' "$f" || { echo "模板缺少「门禁是命令」说明"; exit 1; }
grep -q 'verify-acceptance' "$f" || { echo "模板的 Task 流程缺实跑验收（verify-acceptance）"; exit 1; }
grep -q 'gate task-complete' "$f" || { echo "模板的 Task 流程缺 task-complete 门禁"; exit 1; }
grep -q "review pass" "$f" || { echo "模板缺 review pass（task-complete 硬前置）"; exit 1; }
grep -q -- "--criteria-reviewed" "$f" || { echo "模板缺 review pass 的 --criteria-reviewed"; exit 1; }
grep -q 'task override' "$f" || { echo "模板逃生舱清单缺 task override"; exit 1; }
if grep -qE 'hazard-guard|hooks: on' "$f"; then
  echo "模板仍宣称已删除的 hook 执法能力"
  exit 1
fi
echo "protocol-actionable: ok"