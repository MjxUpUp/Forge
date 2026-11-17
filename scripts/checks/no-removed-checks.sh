#!/usr/bin/env bash
# no-removed-checks: 禁止在用户可见文本里重新引入已删除的门禁/命令。
#
# 用法：no-removed-checks.sh [path...]
# 不带参数时扫描默认目标集合；带参数时只扫给定路径（目录会递归）。
# 退出码：0 = 干净；1 = 命中；2 = 目标不存在或扫描失败（不静默通过）。

set -euo pipefail

PATTERN='task doc-review|forge task mutation|forge selfcheck|task gate task-verify|--acknowledge-changes|artifact-chain|unused-scan|test-coverage-gate|self-refresh|FORGE_ARTIFACT_CHAIN|forge eval artifact-drill|task artifact --extract|task artifact --set|task artifact --check|task artifact --verify|task chain-init|plan-first'

if [ "$#" -eq 0 ]; then
  set -- README.md READMEs/README.zh-CN.md npm/README.md CONTRIBUTING.md RELEASE.md \
    docs/reference internal/change/templates internal/skillintegrate/notes
fi

status=0
for target in "$@"; do
  if [ ! -e "$target" ]; then
    echo "no-removed-checks: 目标不存在: $target" >&2
    status=2
    continue
  fi
  hits="$(grep -rnE -- "$PATTERN" "$target" 2>/dev/null | grep -v '删除' || true)"
  if [ -n "$hits" ]; then
    printf '%s\n' "$hits"
    echo "no-removed-checks: 上面这些位置还在引用已删除的门禁/命令（$target）" >&2
    status=1
  fi
done

if [ "$status" -eq 0 ]; then
  echo "no-removed-checks: ok"
fi
exit "$status"