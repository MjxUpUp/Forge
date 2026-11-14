#!/usr/bin/env bash
# 守卫：task resume 的 hook 兼容模式必须彻底消失（flag、分支、sentinel 实现都不留）。
# 这些入口的调用方是已删除的 `forge hook` 子命令；留着只会误导使用者。
# 注意：测试文件里会引用这些名字做「不应存在」的断言，扫描时排除 _test.go。
set -euo pipefail

prod_files=$(ls internal/clitask/*.go | grep -v '_test.go$')

# Flags().Bool("hook"/"compact-flag"/"reinject", ...) 与 GetBool 两种写法都覆盖。
if grep -n -e 'compact-flag' -e '"reinject"' -e 'GetBool("hook")' -e 'Bool("hook"' -e 'Bool("compact-flag"' -e 'Bool("reinject"' $prod_files; then
  echo 'task resume still exposes hook-mode flags' >&2
  exit 1
fi

if grep -rn -e 'MarkResumeStale' -e 'ConsumeResumeStale' --include=*.go internal; then
  echo 'resume-stale sentinel helpers still referenced' >&2
  exit 1
fi
