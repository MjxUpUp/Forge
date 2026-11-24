# 从 1.84 及更早版本升级：宿主层清理手册

> 2026-11 从根 README 拆出。1.85 删除宿主层后，旧版本留在用户目录里的注入、hook、插件与 skills 分发不会自动消失，需要手工清理（删除前先备份）。
> 下面命令按 bash 写；Windows 用 Git Bash / MSYS 执行。

1.85 删除了宿主层：hook 执法、用户级 `CLAUDE.md` / `AGENTS.md` 注入、插件、skills 分发都不再存在，
forge 的 `uninstall` 子命令也随宿主层一起删了。旧版本留在用户目录里的东西不会自动消失，需要手工清理
（删除前先备份）。下面命令按 bash 写，Windows 请用 Git Bash / MSYS 执行。

旧版会写的位置（按你用过的宿主看）：

| 宿主 | 全局注入 | hook 配置 | 插件 / skills |
| --- | --- | --- | --- |
| Claude Code | `~/.claude/CLAUDE.md`（`FORGE:START/END` 段） | `~/.claude/settings.json` 的 hooks 段 | `~/.claude/plugins/{marketplaces,cache}/forge`、`~/.claude/skills/*` 里的链接 |
| Codex | `~/.codex/AGENTS.md`（`FORGE:START/END` 段） | `~/.codex/hooks.json`，以及 `~/.codex/config.toml` 的 `[features] hooks` | `~/.codex/skills/*` 里的链接 |
| Windsurf | `~/.codeium/windsurf/memories/global_rules.md`（`FORGE:START/END` 段） | `~/.codeium/windsurf/hooks.json` 的 forge 条目 | 本版代码未见 skills 落点，如用过请检查 `~/.codeium/windsurf` |
| Cursor | — | `~/.cursor/hooks.json` | `~/.cursor/skills/*` 里的链接 |
| Kimi | `~/.kimi-code/config.toml`（`# FORGE:START` 注释段） | — | `~/.kimi-code/plugins/installed.json` 的 forge 条目 |

> 1.84 的用户级接线名册还有 cline / codebuddy / zcode / opencode 等宿主；它们在各自
> hooks 目录写文件，本表不逐一列出。不确定时先在这些文件里搜「forge」与「hook」两个词
> （`~/.claude/settings.json`、`~/.codex/hooks.json`、`~/.codeium/windsurf/hooks.json`、
> `~/.cursor/hooks.json`），以及这些宿主的 hooks 目录，再删。

这些段里最要命的一句是「只有看到 `[forge-session]` 横幅或 forge 的 hook 输出才按 forge 行事，
否则忽略并不要运行 forge 命令」——1.85 没有横幅也没有 hook，这个条件恒假，Claude 会主动无视 forge。

```bash
# 0) 备份（按宿主分目录：codex 与 kimi 都有 config.toml，平铺会互相覆盖）
base=~/forge-backup/upgrade-1.84
mkdir -p "$base"/claude "$base"/codex "$base"/windsurf "$base"/cursor "$base"/kimi
backup() {
  dir=$1; shift
  for f in "$@"; do
    if [ -f "$f" ]; then cp "$f" "$base/$dir/"; fi
  done
}
backup claude ~/.claude/CLAUDE.md ~/.claude/settings.json ~/.claude/plugins/installed_plugins.json
backup codex ~/.codex/AGENTS.md ~/.codex/hooks.json ~/.codex/config.toml
backup windsurf ~/.codeium/windsurf/memories/global_rules.md ~/.codeium/windsurf/hooks.json
backup cursor ~/.cursor/hooks.json
backup kimi ~/.kimi-code/config.toml ~/.kimi-code/plugins/installed.json
# 链接清单无法用 cp 备份，先落盘一份再动（`find -maxdepth` 在 macOS 上不存在，
# 所以用 shell glob；`readlink` 把目标也记下来，恢复时有据可查）：
for d in ~/.claude/skills ~/.cursor/skills ~/.codex/skills ~/.copilot/skills ~/.agents/skills; do
  [ -d "$d" ] || continue
  for l in "$d"/*; do
    if [ -L "$l" ]; then echo "$l -> $(readlink "$l")"; fi
  done
done > "$base"/skills-links.txt

# 1) 定位 FORGE:START
grep -l 'FORGE:START' ~/.claude/CLAUDE.md ~/.codex/AGENTS.md \
  ~/.codeium/windsurf/memories/global_rules.md ~/.kimi-code/config.toml 2>/dev/null
#    有就删掉整段（含标记行）；如果文件只剩这一段，直接删文件。
#    同一文件里 forge 段之外的内容（团队规范、你自己的偏好）保留不动。

# 2) Codex hook 配置：只删 forge 条目，别删整个文件
#    编辑 ~/.codex/hooks.json，删掉 command 里同时含 forge 与 hook 的条目；
#    再把 ~/.codex/config.toml 里 [features] hooks = true 改回 false（或删掉该行）。
#    如果 config.toml 里有 # FORGE:START … # FORGE:END 整块（旧版没有 [features] 表时
#    自动追加的），整块删掉，否则会留下孤立标记。

# 3) Claude settings / Windsurf / Cursor hooks：同样只删带 forge 与 hook 的条目
#    编辑 ~/.claude/settings.json 的 hooks 段；
#    编辑 ~/.codeium/windsurf/hooks.json，删 forge 条目；
#    编辑 ~/.cursor/hooks.json，删 forge 条目（旧版写过 user-level hook）。

# 4) 旧插件与插件登记
rm -rf ~/.claude/plugins/marketplaces/forge ~/.claude/plugins/cache/forge
#    再编辑 ~/.claude/plugins/installed_plugins.json，删掉 forge@… 条目
#    （或直接跑 claude plugin uninstall forge，如果这个版本的 CLI 还认识它）
#    Kimi：编辑 ~/.kimi-code/plugins/installed.json 删 forge 条目，并删 ~/.kimi-code/plugins/managed/forge。

# 5) skills 分发：只删指向 skills-cache 的链接，再删缓存，否则会留下悬空链接
#    先预览（应全是 .../skills-cache/embedded/...）：
for d in ~/.claude/skills ~/.cursor/skills ~/.codex/skills ~/.copilot/skills ~/.agents/skills; do
  [ -d "$d" ] || continue
  for l in "$d"/*; do
    if [ -L "$l" ]; then case "$(readlink "$l")" in *skills-cache*) echo "$l";; esac; fi
  done
done
#    确认无误后把上面的 echo 换成 rm -f 再跑一遍（用户自己 ln -s 的链接不受影响）。
#    旧版还会写真实目录 forge-quality（里面那句「只在横幅出现时才生效」正是要清掉的
#    文案）；它不在链接清单里，需要单独删。1.84 同时写 claude 与 ~/.agents 两处：
rm -rf ~/.claude/skills/forge-quality ~/.agents/skills/forge-quality
#    Reasonix 用户再加 ~/.reasonix/skills/forge-quality。
rm -rf ~/.forge/skills-cache
rm -f ~/.forge/skills-manifest.json

# 6) 自检：下面都应为空
grep -l 'FORGE:START' ~/.claude/CLAUDE.md ~/.codex/AGENTS.md \
  ~/.codeium/windsurf/memories/global_rules.md ~/.kimi-code/config.toml 2>/dev/null
grep -rl 'forge' ~/.claude/settings.json ~/.codex/hooks.json \
  ~/.codeium/windsurf/hooks.json ~/.cursor/hooks.json 2>/dev/null
grep -l 'forge' ~/.claude/plugins/installed_plugins.json ~/.kimi-code/plugins/installed.json 2>/dev/null
ls ~/.forge/skills-cache 2>/dev/null
ls -d ~/.claude/skills/forge-quality ~/.agents/skills/forge-quality 2>/dev/null
for d in ~/.claude/skills ~/.cursor/skills ~/.codex/skills ~/.copilot/skills ~/.agents/skills; do
  [ -d "$d" ] || continue
  for l in "$d"/*; do
    if [ -L "$l" ]; then case "$(readlink "$l")" in *skills-cache*) echo "$l";; esac; fi
  done
done

# 7) 确认上面都干净后，最后再删这些历史目录（它们是最后的回滚点）
rm -rf ~/.forge/skills-backup ~/.forge/.init-suggested
```

> 说明：第 5 步按 `readlink` 的目标里是否含 `skills-cache` 来判定，只匹配 forge 装的链接，
> 用户自己 `ln -s` 的不受影响；链接清单已在第 0 步落盘到 `~/forge-backup/upgrade-1.84/skills-links.txt`。
> 第 7 步放在自检之后，是因为 `skills-backup` 是旧版本留下的回滚副本。

清理后在仓库里跑 `forge status`，应看到 `spec/forge.yaml` 契约。较新的 Claude Code
（2.1.277 起）在没有 `CLAUDE.md` 时会直接读 `AGENTS.md`；不确定自己的版本是否支持时，
跑 `forge change agents --pointers claude` 生成 `CLAUDE.md` 指针最稳妥。
