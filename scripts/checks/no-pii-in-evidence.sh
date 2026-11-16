#!/bin/sh
# 守卫：spec/ 下随 git 走的证据（change verify 的 stdout/stderr/summary）不得
# 含本机绝对路径或用户名。封存时已替换为 <home>/<user>/<host> 占位符；这里
# 兜住绕过封存的手写证据与历史遗留文件——历史上 t.TempDir() 的输出曾把
# Windows 用户名提交进公开仓库。
set -eu
hits=$(grep -rInE '([A-Za-z]:[\\/]+Users[\\/])|(/(home|Users)/[^[:space:]/]+/)|(/[a-z]/Users/)|ADMINI~' spec 2>/dev/null || true)
if [ -n "$hits" ]; then
  echo "evidence contains local absolute paths or usernames (use <home>/<user> placeholders):"
  echo "$hits"
  exit 1
fi
exit 0
