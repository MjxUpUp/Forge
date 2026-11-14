#!/usr/bin/env bash
# 守卫：task resume 的 hook 兼容模式必须彻底消失（flag、分支、sentinel 实现都不留）。
# 这些入口的调用方是已删除的 `forge hook` 子命令；留着只会误导使用者。
set -euo pipefail

if grep -n -e 'compact-flag' -e '"reinject"' -e 'GetBool("hook")' internal/clitask/*.go; then
  echo 'task resume still exposes hook-mode flags' >&2
  exit 1
fi

if grep -rn -e 'MarkResumeStale' -e 'ConsumeResumeStale' --include=*.go internal; then
  echo 'resume-stale sentinel helpers still referenced' >&2
  exit 1
fi