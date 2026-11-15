#!/bin/sh
# gofmt-clean.sh —— CI 同款检查：任何未格式化文件都算失败。
# `gofmt -l` 自己总是 exit 0，所以必须看输出。
set -eu
out=$(gofmt -l cmd internal skills)
if [ -n "$out" ]; then
	echo "gofmt needed:" >&2
	echo "$out" >&2
	exit 1
fi