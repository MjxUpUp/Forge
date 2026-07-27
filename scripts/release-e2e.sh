#!/usr/bin/env bash
# 方案 B：本地 Verdaccio E2E。起本地 npm registry，真实 publish 6 包 + 真实 npm install，
# 验证 optionalDependencies 平台分包（install 主包真只拉匹配当前平台的子包）。dry-run
# 验证不了 install，故有此 E2E——它能暴露 "子包 os/cpu 字段错、bin 路径错、平台分包失效" 类问题。
#
# 中间产物卫生（5 条铁律，绝不污染用户环境）：
#   1. 所有产物进 mktemp -d，trap EXIT rm -rf
#   2. Verdaccio 后台进程 trap kill
#   3. npm 配置隔离：npm_config_userconfig=<tmp> + npm_config_registry=<verdaccio>
#      （绝不 npm config set，绝不碰 ~/.npmrc——env 覆盖已实测生效）
#   4. forge 二进制 go build 到临时 bin/
#   5. install 验证用临时 npm 项目
#
# 用法：bash scripts/release-e2e.sh
# 退出码 0 = E2E 通过（平台分包 install 正确）；非 0 = 失败。
# 轻量预演（只 dry-run 不 install）见 scripts/release-rehearsal.sh。
set -euo pipefail

cd "$(dirname "$0")/.."

WORK=$(mktemp -d)
INSTALL_DIR=$(mktemp -d)
VPID=""
cleanup() {
  # 铁律 2：kill Verdaccio 后台进程；铁律 1/5：rm 临时目录。
  # Windows Git Bash 用 kill bash builtin（不走 taskkill，避免 /PID 被转成路径）。
  [ -n "${VPID:-}" ] && kill "$VPID" 2>/dev/null || true
  rm -rf "$WORK" "$INSTALL_DIR"
}
trap cleanup EXIT INT TERM

PORT=4873
# 版本号用变量拼接：源码里不出现字面 verdaccio@6（local@domain 模式会被 email-obfuscate
# 污染源码，参见 windows-input-quote-corruption memory）。
VERDACCIO_VER=6
echo "=== 方案 B：Verdaccio E2E（工作区 $WORK，install 验证 $INSTALL_DIR）==="

# 1. Verdaccio config（storage/htpasswd/plugins 隔离到 WORK，不落默认 ~/.local/share/verdaccio）
mkdir -p "$WORK/storage" "$WORK/plugins"
cat > "$WORK/config.yaml" <<EOF
storage: $WORK/storage
plugins: $WORK/plugins
max_body_size: 100mb
auth:
  htpasswd:
    file: $WORK/htpasswd
    max_users: 1000
security:
  api:
    legacy: true
uplinks:
  npmjs:
    url: https://registry.npmjs.org
packages:
  '@*/*':
    access: \$all
    publish: \$all
  '**':
    access: \$all
    publish: \$all
    proxy: npmjs
log: { type: stdout, format: pretty, level: warn }
EOF

# 2. 起 Verdaccio 后台
echo "--- 启动 Verdaccio @ 127.0.0.1:$PORT ---"
npx --yes verdaccio@"$VERDACCIO_VER" -c "$WORK/config.yaml" -l "127.0.0.1:$PORT" >/dev/null 2>&1 &
VPID=$!
# 首次 npx 下载 Verdaccio（~50MB+deps）可能 >30s，给 120s 轮询窗口
for _ in $(seq 1 240); do
  curl -sf "http://127.0.0.1:$PORT/-/ping" >/dev/null 2>&1 && break
  sleep 0.5
done
curl -sf "http://127.0.0.1:$PORT/-/ping" >/dev/null 2>&1 || { echo "❌ Verdaccio 启动失败"; exit 1; }
echo "✅ Verdaccio 就绪 (PID $VPID)"

# 3. npm 配置隔离（铁律 3：env 注入，不碰 ~/.npmrc / 全局 config）
export npm_config_registry="http://127.0.0.1:$PORT"
export npm_config_userconfig="$WORK/.npmrc"   # adduser 的 token 落这，绝不落 ~/.npmrc
# 注册临时用户：npm adduser 在 Git Bash 非 TTY 下 readline 失败（Username:/Password: 后立即
# exit，npm 11.18 实测），改直接 curl 调 npm registry adduser API（PUT /-/user/org.couchdb.user:<name>），
# 拿 token 写隔离的 .npmrc。email 用三段拼接避免字面 local@domain 被 obfuscate。
RUSER=rehearsal
REMAIL="rehearsal""@""local"
RESP=$(curl -sf -X PUT "http://127.0.0.1:$PORT/-/user/org.couchdb.user:$RUSER" \
  -H "content-type: application/json" \
  -d "{\"name\":\"$RUSER\",\"password\":\"$RUSER\",\"type\":\"user\",\"roles\":[],\"email\":\"$REMAIL\",\"_id\":\"org.couchdb.user:$RUSER\"}") \
  || { echo "❌ adduser API 失败"; exit 1; }
TOKEN=$(printf '%s' "$RESP" | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>{try{console.log(JSON.parse(s).token||"")}catch(e){console.log("")}})')
[ -n "$TOKEN" ] || { echo "❌ adduser 未返回 token"; exit 1; }
echo "//127.0.0.1:$PORT/:_authToken=$TOKEN" > "$WORK/.npmrc"
echo "✅ 临时用户 $RUSER 注册 + token 落 $WORK/.npmrc（~/.npmrc 不动）"

# 4. 构建当前平台二进制（铁律 4：临时 bin/）+ 组装 6 包
echo "--- go build 当前平台 forge 二进制 ---"
mkdir -p "$WORK/binbuild"
GOOS_VAL=$(go env GOOS)
GOARCH_VAL=$(go env GOARCH)
case "$GOOS_VAL-$GOARCH_VAL" in
  darwin-arm64)  CUR=darwin-arm64; EXE=forge ;;
  darwin-amd64)  CUR=darwin-x64;   EXE=forge ;;
  linux-arm64)   CUR=linux-arm64;  EXE=forge ;;
  linux-amd64)   CUR=linux-x64;    EXE=forge ;;
  windows-amd64) CUR=win32-x64;    EXE=forge.exe ;;
  *) echo "❌ 当前平台 $GOOS_VAL-$GOARCH_VAL 不在发布矩阵（须 5 平台之一才可 E2E）"; exit 1 ;;
esac
go build -o "$WORK/binbuild/$EXE" ./cmd/forge
echo "✅ 当前平台 $CUR 二进制就绪"

echo "--- 组装 6 包到 $WORK/publish ---"
mkdir -p "$WORK/publish/main"
cp npm/package.json "$WORK/publish/main/"
# E2E 隔离：删 publishConfig.registry（源里指向 npmjs.org，会让 publish 绕过 Verdaccio 打真
# registry → 失败）。只删组装副本，源 npm/package.json 不动（真实发布仍需它指向 npmjs.org）。
tmp=$(mktemp)
jq 'del(.publishConfig)' "$WORK/publish/main/package.json" > "$tmp" && mv "$tmp" "$WORK/publish/main/package.json"
cp npm/run.js "$WORK/publish/main/" 2>/dev/null || true
[ -f npm/README.md ] && cp npm/README.md "$WORK/publish/main/" || true
for pkg in darwin-arm64 darwin-x64 linux-arm64 linux-x64 win32-x64; do
  mkdir -p "$WORK/publish/$pkg/bin"
  cp "npm/platforms/$pkg/package.json" "$WORK/publish/$pkg/"
  if [ "$pkg" = "$CUR" ]; then
    cp "$WORK/binbuild/$EXE" "$WORK/publish/$pkg/bin/$EXE"
  fi
  # 非当前平台 bin/ 留空：install 时 npm 按 os/cpu 不装它们，验证平台过滤
done

# 5. publish 6 包到 Verdaccio
echo "--- publish 6 包到 Verdaccio ---"
for pkg in darwin-arm64 darwin-x64 linux-arm64 linux-x64 win32-x64; do
  (cd "$WORK/publish/$pkg" && npm publish --access public >/dev/null 2>&1) || { echo "❌ publish @agent_forge/forge-$pkg 失败"; exit 1; }
  echo "✅ @agent_forge/forge-$pkg"
done
(cd "$WORK/publish/main" && npm publish --access public >/dev/null 2>&1) || { echo "❌ publish main 失败"; exit 1; }
echo "✅ @agent_forge/forge (main)"

# 6. install 验证 optionalDependencies 平台分包（铁律 5：临时项目）
echo "--- install 验证（临时项目 $INSTALL_DIR）---"
cd "$INSTALL_DIR"
npm init -y >/dev/null 2>&1
npm install @agent_forge/forge >/dev/null 2>&1 || { echo "❌ npm install @agent_forge/forge 失败"; exit 1; }

# 断言 A：当前平台子包进 node_modules
if [ -d "node_modules/@agent_forge/forge-$CUR" ]; then
  echo "✅ node_modules/@agent_forge/forge-$CUR 存在（当前平台子包正确安装）"
else
  echo "❌ node_modules/@agent_forge/forge-$CUR 不存在（optionalDependencies 平台分包失效）"
  ls -la node_modules/@agent_forge/ 2>/dev/null || true
  exit 1
fi
# 断言 B：其他平台子包不进（npm 按 os/cpu 过滤）
INSTALLED_OTHERS=$(ls node_modules/@agent_forge/ 2>/dev/null | grep -vE "^forge$|^forge-$CUR$" || true)
if [ -z "$INSTALLED_OTHERS" ]; then
  echo "✅ 其他平台子包未安装（npm 按 os/cpu 正确过滤）"
else
  echo "❌ 意外安装了非当前平台子包: $INSTALLED_OTHERS"
  exit 1
fi

echo ""
echo "=== ✅ 方案 B E2E 通过：optionalDependencies 平台分包机制验证 ==="
echo "=== 卫生：trap 清理 $WORK（Verdaccio 存储/config）+ $INSTALL_DIR（install 项目），kill Verdaccio PID $VPID ==="
