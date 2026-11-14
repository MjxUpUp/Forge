#!/bin/sh
# 守卫：README 必须保留从 1.84 及更早版本升级时的旧注入清理说明。
# POSIX sh（dash 也能跑）：acceptance 用 `sh` 调用本脚本。
set -eu
f="README.md"
[ -f "$f" ] || { echo "缺 README.md"; exit 1; }
grep -q '从 1.84 及更早版本升级' "$f" || { echo "README 缺 1.84 迁移节"; exit 1; }
grep -q 'FORGE:START' "$f" || { echo "README 迁移节缺旧注入定位命令"; exit 1; }
grep -q '\.codeium/windsurf' "$f" || { echo "README 迁移节漏 windsurf 的全局注入"; exit 1; }
grep -q '\.kimi-code/config\.toml' "$f" || { echo "README 迁移节漏 kimi 的全局注入"; exit 1; }
grep -qE 'hooks\.json.*forge hook' "$f" || { echo "README 缺「只删 codex hooks.json 里的 forge 条目」说明"; exit 1; }
grep -q 'installed_plugins\.json' "$f" || { echo "README 缺插件登记清理"; exit 1; }
grep -q 'type l -delete' "$f" || { echo "README 缺悬空符号链接清理"; exit 1; }
echo "migration-doc: ok"