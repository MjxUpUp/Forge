#!/bin/sh
# 判据：internal/scoring 与 internal/scoringtypes 已删除，仓库内 Go 源码不得再引用它们。
# git grep 无匹配时退出码为 1，这里用 if 条件把它转成「通过」（acceptance 不允许 || 写法）。
set -eu

if git grep -n -e "internal/scoring" -- "*.go"; then
	echo "FAIL: 仍有 Go 源码引用已删除的评分包" >&2
	exit 1
fi
exit 0
