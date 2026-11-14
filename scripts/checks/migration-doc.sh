#!/usr/bin/env bash
# 守卫：README 必须保留从 1.84 及更早版本升级时的旧注入清理说明。
set -euo pipefail
f="README.md"
[ -f "$f" ] || { echo "缺 README.md"; exit 1; }
grep -q '从 1.84 及更早版本升级' "$f" || { echo "README 缺 1.84 迁移节"; exit 1; }
grep -q 'FORGE:START' "$f" || { echo "README 迁移节缺旧注入定位命令"; exit 1; }
grep -q '\.codex/hooks\.json' "$f" || { echo "README 迁移节缺旧 hook 配置清理"; exit 1; }
echo "migration-doc: ok"
