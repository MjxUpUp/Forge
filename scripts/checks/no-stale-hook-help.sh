#!/usr/bin/env bash
# 守卫：命令帮助里不得再出现宿主层（hook）时代的措辞。
# 阶段 D 删除了 hook 接线，帮助文本若仍教 agent 调 `forge hook` 或等 Stop hook 拦截，就是假承诺。
set -euo pipefail

files=(
  internal/cli/root.go
  internal/cli/review.go
  internal/cli/verify.go
  internal/cli/config.go
  internal/clitask/task_continuity.go
)

if grep -n -e 'forge hook' -e 'Stop hook' -e '初始化管道' "${files[@]}"; then
  echo 'stale host-layer wording found in command help (see matches above)' >&2
  exit 1
fi