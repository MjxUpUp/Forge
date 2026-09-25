#!/usr/bin/env bash
# 方案 A：本地发布预演。对 6 包跑 npm publish --dry-run，走完整 npm-package-arg 路径解析 +
# tarball 打包，提前暴露 "npm publish <含/路径> 被 npa 当 github shorthand" 类 bug
#（v1.15.0 CI npm job 曾因此 git ls-remote 失败）。
#
# dry-run 行为细节（实测）：npm publish --dry-run 会查 configured registry 判断版本是否已
# 发布，已发布则报 "cannot publish over the previously published"。预演应在发布前（版本未发布）
# 跑；本脚本把"已发布"视为 pack 结构验证通过（npa 解析对、files 正确），只对 npa/结构类错误报失败。
#
# 用法：bash scripts/release-rehearsal.sh
# 退出码 0 = 6 包 dry-run 结构验证全过；非 0 = 有包 npa/结构错误。
# 完整 E2E（真实 publish + install 验证平台分包）见 scripts/release-e2e.sh。
set -euo pipefail

cd "$(dirname "$0")/.."

echo "=== 方案 A：本地发布预演（npm publish --dry-run × 6 包）==="
echo "验证：npm-package-arg 路径解析 + package.json 合法 + tarball 打包"
echo ""

# dry-run 检查：npa 解析 + pack 结构。版本已发布（registry 冲突）视为结构通过。
dry_run_check() {
  local pkg_dir="$1" pkg_name="$2"
  local out
  if ! out=$(cd "$pkg_dir" && npm publish --dry-run --access public 2>&1); then
    if echo "$out" | grep -q "cannot publish over the previously published"; then
      echo "✅ $pkg_name pack 结构通过（版本已在 registry——预演针对未发布版本，此处仅验证结构）"
      return 0
    fi
    echo "❌ $pkg_name dry-run 失败（npa/结构错误）："
    echo "$out" | grep -E "npm error|npm notice" | head -5
    return 1
  fi
  echo "✅ $pkg_name dry-run pack 通过"
  return 0
}

fail=0
# 5 平台子包（cd 进目录再 publish，与 release.yml 一致——裸路径含/会被 npa 当 github shorthand）
for pkg in darwin-arm64 darwin-x64 linux-arm64 linux-x64 win32-x64; do
  dry_run_check "npm/platforms/$pkg" "@agent_forge/forge-$pkg" || fail=1
done
# 主包（cwd=npm/，无路径参数，npa 当 folder）
dry_run_check "npm" "@agent_forge/forge (main)" || fail=1

echo ""
if [ "$fail" = 0 ]; then
  echo "=== ✅ 方案 A 预演通过：6 包 dry-run 结构验证全过，可推送 tag 触发真实发布 ==="
  exit 0
else
  echo "=== ❌ 方案 A 预演失败：修复后再发布 ==="
  exit 1
fi
