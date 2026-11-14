#!/bin/sh
# 守卫：README 必须保留从 1.84 及更早版本升级时的旧注入清理说明。
# POSIX sh（dash 也能跑）：acceptance 用 `sh` 调用本脚本。
set -eu
f="README.md"
[ -f "$f" ] || { echo "缺 README.md"; exit 1; }
grep -q '从 1.84 及更早版本升级' "$f" || { echo "README 缺 1.84 迁移节"; exit 1; }
grep -q 'FORGE:START' "$f" || { echo "README 迁移节缺旧注入定位"; exit 1; }
grep -q '\.codeium/windsurf' "$f" || { echo "README 迁移节漏 windsurf 的全局注入"; exit 1; }
grep -q '\.kimi-code/config\.toml' "$f" || { echo "README 迁移节漏 kimi 的全局注入"; exit 1; }
grep -q '\.cursor/hooks\.json' "$f" || { echo "README 迁移节漏 cursor 的 hook 配置"; exit 1; }

# 锚点必须落在「动作行」上：文件名在表格/备份行里也会出现，只 grep 文件名会假绿。
grep -qE '编辑 .*hooks\.json.*forge hook' "$f" \
  || { echo "README 缺「编辑 codex hooks.json 只删 forge 条目」动作"; exit 1; }
grep -qE '编辑 .*windsurf/hooks\.json' "$f" \
  || { echo "README 缺「编辑 windsurf hooks.json」动作"; exit 1; }
grep -qE '编辑 .*installed_plugins\.json' "$f" \
  || { echo "README 缺「编辑 installed_plugins.json 删 forge 条目」动作"; exit 1; }
grep -q "grep -l 'FORGE:START'" "$f" \
  || { echo "README 缺 FORGE:START 定位命令"; exit 1; }
grep -q 'FORGE:END 整块' "$f" \
  || { echo "README 缺 codex config.toml 的 FORGE 标记块清理"; exit 1; }

# 顺序断言：先处理链接、再删缓存；删缓存之后不能再有「先删链接」的提示。
ln=$(grep -n '\*skills-cache\*' "$f" | head -1 | cut -d: -f1)
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