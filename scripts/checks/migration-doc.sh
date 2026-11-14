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
grep -q '\.cursor/hooks\.json' "$f" || { echo "README 迁移节漏 cursor 的 hook 配置"; exit 1; }
grep -qE 'hooks\.json.*forge hook' "$f" || { echo "README 缺「只删 codex hooks.json 里的 forge 条目」说明"; exit 1; }
grep -q 'installed_plugins\.json' "$f" || { echo "README 缺插件登记清理"; exit 1; }
grep -q -- "-lname '\*skills-cache\*'" "$f" || { echo "README 缺「只删 skills-cache 链接」的清理"; exit 1; }

# 顺序断言：先处理链接、再删缓存；删缓存之后不能再有「先删链接」的提示。
ln=$(grep -n -- "-lname '\*skills-cache\*'" "$f" | head -1 | cut -d: -f1)
rmc=$(grep -n -- 'rm -rf ~/.forge/skills-cache' "$f" | head -1 | cut -d: -f1)
if [ -z "$ln" ] || [ -z "$rmc" ]; then
  echo "README 迁移节缺链接清理或缓存删除步骤"
  exit 1
fi
if [ "$ln" -gt "$rmc" ]; then
  echo "README 迁移节顺序错误：必须先删 skills-cache 链接，再删缓存目录"
  exit 1
fi
echo "migration-doc: ok"