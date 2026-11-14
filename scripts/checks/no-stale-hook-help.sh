#!/usr/bin/env bash
# 守卫：命令帮助与非帮助注释里不得再出现宿主层（hook）时代的措辞与死分支。
# 阶段 D 删除了 hook 接线，帮助文本若仍教 agent 调 `forge hook` 或等 Stop hook 拦截，就是假承诺。
#
# 跨行 + 注释符归一：曾出现 `hook` 行尾折行接 `// 命令…`，直接 tr 拼接后是 `hook \t// 命令`，
# 模式 `hook 命令` 漏网（review 反例）。这里先去掉 `//`、把换行/制表符压成空格再匹配。
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

for f in "${files[@]}"; do
  flat=$(sed 's|//||g' "$f" | tr '\n\t' '  ' | tr -s ' ')
  if printf '%s' "$flat" | grep -e 'forge hook' -e 'Stop hook' -e '初始化管道' -e 'compact-resume' -e 'resume-reinject' -e 'os.Args\[1\] == "hook"' -e 'hook 命令' -e 'hook spawn' -e 'hook 模式' >/dev/null; then
    echo "stale host-layer wording in $f" >&2
    printf '%s' "$flat" | grep -o -e '.\{0,24\}forge hook.\{0,24\}' -e '.\{0,24\}Stop hook.\{0,24\}' -e '.\{0,24\}hook 命令.\{0,24\}' -e '.\{0,24\}hook spawn.\{0,24\}' -e '.\{0,24\}hook 模式.\{0,24\}' >&2 || true
    exit 1
  fi
done
