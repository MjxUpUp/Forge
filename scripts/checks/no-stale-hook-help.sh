#!/usr/bin/env bash
# 守卫：命令帮助里不得再出现宿主层（hook）时代的措辞与死分支。
# 阶段 D 删除了 hook 接线，帮助文本若仍教 agent 调 `forge hook` 或等 Stop hook 拦截，就是假承诺。
set -euo pipefail

files=(
  internal/cli/root.go
  internal/cli/review.go
  internal/cli/verify.go
  internal/cli/config.go
  internal/cli/shim_heal.go
  internal/clitask/task_continuity.go
)

# 文件缺失时 grep 会退 2，`if grep` 会把它当成「没命中」而放行；先确认都在。
for f in "${files[@]}"; do
  if [[ ! -f "$f" ]]; then
    echo "guard target missing: $f" >&2
    exit 2
  fi
done

if grep -n -e 'forge hook' -e 'Stop hook' -e '初始化管道' -e 'compact-resume' -e 'resume-reinject' -e 'os.Args\[1\] == "hook"' "${files[@]}"; then
  echo 'stale host-layer wording found in command help (see matches above)' >&2
  exit 1
fi
