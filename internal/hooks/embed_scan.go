package hooks

// embed_scan.go —— embed.go 同包分文件产物（2026-09 普查 P7：2322 行单文件按职能
// 拆分；内容逐字节不变——embeddedHooks 名册与 guard test 钉住等价性）。

const ToolTrackHook = `#!/bin/bash
# tool-track.sh — PostToolUse Read hook (silent toollog recorder).
# Records Read calls into toollog (via the forge hook dispatch in hook.go) so
# the read-before-edit gate at task-verify can confirm the agent read code
# before editing it. Deliberately minimal and silent: Read-only matcher, no
# checklog entry, no stderr output — the toollog append is the only effect.
#
# This restores the Read-recording the tool-track hook provided before 644b142
# removed it (alongside the untrusted tool-selection dimension). That removal
# left the gate with no Read data, making it always-fail on any task with
# edits. This minimal version records Read only — not Bash/Grep/Glob — to
# avoid re-introducing the toollog volume that motivated the original deletion.
echo "PASS"
`

const SkillScanHook = `#!/bin/bash
# skill-scan.sh — SessionStart hook (advisory, non-blocking).
# 会话开始扫描 skill 目录安全性（forge audit 21 规则：prompt 注入/数据外发/危险
# 代码/系统提示泄露/供应链执行向量）。补 install 门控的缺口：skill 经 install 之外的路径进入 agent
# 环境（手动 cp/clone、git pull 更新、external junction 如 lark-*）时 install 门控
# 扫不到，SessionStart 是天然检查点，覆盖所有来源。advisory：stdout PASS detail
# 列出有 finding 的风险 skill（含 MEDIUM），不阻塞会话（advisory 方向），
# 由 agent/用户自检是否使用。全局 hook：不依赖 forge project。
SCAN_DIR="$HOME/.claude/skills"
if [ ! -d "$SCAN_DIR" ]; then
  echo "PASS [skill-scan] no ~/.claude/skills (advisory)"
  exit 0
fi

# 捕获 stderr 到临时文件而非直接丢弃：exit code 只说"scan 崩了"，stderr（panic/错误）
# 说"怎么崩的"，是崩溃分支的诊断线索（review suggest#2）。trap EXIT 自动清理临时文件。
# mktemp 失败（极罕见）降级到 /dev/null 而非空串——2>"" 会触发 ambiguous redirect 让
# forge 调用本身失败（其 exit 被误报为崩溃）。trap 只清理 mktemp 创建的真实临时文件，
# 绝不 rm /dev/null（设备文件，rm 可能报权限噪声或被误判）。
STDERR_FILE=$(mktemp 2>/dev/null) || STDERR_FILE="/dev/null"
trap '[ -n "$STDERR_FILE" ] && [ "$STDERR_FILE" != "/dev/null" ] && rm -f "$STDERR_FILE"' EXIT

# --gate 编码 HIGH/CRITICAL 为 exit 4；正常 exit 0。两者都表示 scan 成功执行。
# audit scan 输出每行一个 skill：✓/✗ name score=X SEV (rec, N finding)。
OUTPUT=$(forge skills audit scan --canonical "$SCAN_DIR" --gate 2>"$STDERR_FILE")
CODE=$?

# 诚实信号：scan 成功 = exit 0 或 4；其他 exit code = 崩溃；空输出 = 未产生结果。
# 两者都报"未完成"，避免 scan 失败却报 "all SAFE" 的假阴性（advisory fail-open 的
# 副作用——宁放过不阻塞是对的，但报"全部安全"是撒谎；正确是报"没扫成"）。
if { [ "$CODE" != 0 ] && [ "$CODE" != 4 ]; } || [ -z "$OUTPUT" ]; then
  # stderr 尾部（≤400 字节）作为诊断线索——exit code 定性"崩了"，stderr 定位"为何"。
  STDERR_TAIL=$(tail -c 400 "$STDERR_FILE" 2>/dev/null | tr -d '\r')
  if [ -n "$STDERR_TAIL" ]; then
    echo "PASS [skill-scan] Advisory: skill 安全扫描未完成（forge audit scan exit=$CODE，stderr: $STDERR_TAIL）。建议手动 'forge skills audit scan' 核查（forge 不阻塞）。"
  else
    echo "PASS [skill-scan] Advisory: skill 安全扫描未完成（forge audit scan exit=$CODE），建议手动 'forge skills audit scan' 核查（forge 不阻塞）。"
  fi
  exit 0
fi

# scan 成功：✗ 行 = 有 finding 的 skill（含 MEDIUM CAUTION，advisory 列全部风险）。
# ✗ 为辅助列举（风险存在性已由 exit code 保证 scan 确实执行了），依赖 forge audit
# 文本格式（内部耦合，可控）。不用 --json：bash 解析 JSON 不比 grep ✗ 更稳，且更复杂。
RISKS=$(printf '%s' "$OUTPUT" | grep -E '✗' || true)

if [ -n "$RISKS" ]; then
  # 截断避免超 AdditionalContext 限制（forge hook 包装上限 9500 字符）。
  RISKS_SUMMARY=$(printf '%s' "$RISKS" | head -10 | tr '\n' ' ' | cut -c1-600)
  echo "PASS [skill-scan] Advisory: 发现风险 skill——${RISKS_SUMMARY}请核查（forge 不阻塞，由 agent/用户自检是否使用）。"
else
  echo "PASS [skill-scan] all skills SAFE (advisory, 21 rules)"
fi
`

const McpScanHook = `#!/bin/bash
# mcp-scan.sh — SessionStart hook (advisory, non-blocking, global).
# 会话开始扫描项目级 .mcp.json 的 server 配置安全性。补 skill-scan 盲区:
# skill-scan 只扫 ~/.claude/skills,但项目级 .mcp.json 在 SessionStart 被各 host
# 自动加载——攻击者可通过 PR/git 植入恶意 server,用户 clone 项目即自动连接,
# 是真实攻击面(2025 多起 MCP 供应链事件)。项目级聚焦:用户级 ~/.claude.json 等
# 是用户自装 server,风险自担,不在范围(全局扫用户级跨 host 路径不一且误报多)。
#
# 诚实边界(必读,不声称超出能力):.mcp.json 只含 server 连接配置(command/args/
# env/url),不含 tool descriptions。真正的 Tool Poisoning(恶意 tool description
# 注入 agent 上下文)live 在 server 运行时返回的 tool descriptions,config-layer
# 扫不到。本 hook 只审 config 层可检攻击面:管道执行(curl 管道到 sh)/任意包执行
# (npx/uvx 远程包)/内联代码(解释器 -c/-e)/非 https URL/env 明文凭证。runtime
# tool description 注入无 config 信号,只能在使用点察觉,不在本 hook 能力内。
#
# advisory:stdout PASS detail 列风险不阻塞(advisory 方向)。全局 hook:不依赖
# forge project(非 forge 项目的 .mcp.json 正是要发现的)。
# Protocol: stdout = PASS detail → additionalContext;exit 0 = 放行。

# 起点:FORGE_CWD(cli/hook.go 传 cwd)或回退 $PWD;Windows 反斜杠归一为正斜杠。
START="${FORGE_CWD:-$PWD}"
START="${START//\\//}"

# 找 git root(向上找 .git,与 init-suggest 同款;盘符根 %/* 返回原值时 break 防死循环)。
ROOT=""
D="$START"
while [ -n "$D" ] && [ "$D" != "/" ]; do
  if [ -e "$D/.git" ]; then ROOT="$D"; break; fi
  NEW="${D%/*}"
  if [ "$NEW" = "$D" ]; then break; fi
  D="$NEW"
done

# 无 git root → 非项目仓库,无项目级 .mcp.json 可审,静默。
if [ -z "$ROOT" ]; then
  echo "PASS [mcp-scan] no git project (no project-level .mcp.json to scan, advisory)"
  exit 0
fi

MCP_FILE="$ROOT/.mcp.json"
if [ ! -f "$MCP_FILE" ]; then
  echo "PASS [mcp-scan] no .mcp.json (advisory)"
  exit 0
fi

# 读取失败/空 → 无法判定,静默放行(不撒谎报"全部安全")。
CONTENT=$(cat "$MCP_FILE" 2>/dev/null)
if [ -z "$CONTENT" ]; then
  echo "PASS [mcp-scan] .mcp.json unreadable or empty (advisory)"
  exit 0
fi

# --- config-layer 风险检测 ---
# 全部 case-glob + grep -Fi/grep -qi(BSD 安全:不用 grep -E 交替,参 bash-guard/skill-scan,
# BSD/macOS grep 在 ERE 交替 abort "Unmatched (")。advisory 方向,宁误报勿漏。
# 大小写归一便于 URL/command/字段名匹配;[|] 字符类匹配字面管道符(pattern 间 | 才是 alternation)。
LOWER=$(printf '%s' "$CONTENT" | tr '[:upper:]' '[:lower:]')
RISKS=""

# 1. 管道执行:curl/wget 管道到 shell —— 远程下载即执行,经典植入形态。
# \| 转义为字面管道符(参 hazard-guard *git\ push* 的反斜杠转义);不能用 [|],
# bash case pattern parser 把 | 当 alternation separator,字符类内的 | 也被吞。
case "$LOWER" in
  *curl*\|*sh*|*wget*\|*sh*|*curl*\|*bash*|*wget*\|*bash*) RISKS="${RISKS}[pipe-exec] curl/wget 管道到 shell(远程下载即执行)。 ";;
esac

# 2. 任意包执行:npx/uvx/dlx/bunx 拉远程包执行——供应链/typosquat 风险(包名可仿冒)。
# 裸 token 不锚空格:JSON 里 command 值是 "npx",npx 后紧跟引号非空格,空格锚定会漏。
case "$LOWER" in
  *npx*|*uvx*|*dlx*|*bunx*) RISKS="${RISKS}[pkg-exec] npx/uvx/dlx/bunx 任意远程包(供应链/typosquat)。 ";;
esac

# 3. 内联代码:解释器 -c/-e 把字符串当代码执行(独立 grep -qi BRE,无 ERE 交替)。
INLINE=0
if printf '%s' "$CONTENT" | grep -qi 'python.*-c' || \
   printf '%s' "$CONTENT" | grep -qi 'node.*-e' || \
   printf '%s' "$CONTENT" | grep -qi 'ruby.*-e' || \
   printf '%s' "$CONTENT" | grep -qi 'perl.*-e'; then
  INLINE=1
fi
if [ "$INLINE" = "1" ]; then
  RISKS="${RISKS}[inline-code] 解释器 -c/-e 内联代码执行。 "
fi

# 4. 非 https URL:http:// 明文(中间人可篡改 server 响应)。
if printf '%s' "$CONTENT" | grep -qi 'http://'; then
  RISKS="${RISKS}[insecure-url] http:// 明文 URL(应为 https)。 "
fi

# 5. env 明文凭证:JSON key 形如 "token" / "secret" 等。grep -Fi 固定串,大小写不敏感,
#    多 -e 模式 OR;双引号用 printf 八进制 \042 运行时构造,避开源码 ASCII 双引号
#    被编辑器腐蚀成弯引号(memory: windows-input-quote-corruption)。
DQ=$(printf '\042')
if printf '%s' "$CONTENT" | grep -Fiq -e "${DQ}token${DQ}" -e "${DQ}secret${DQ}" -e "${DQ}api_key${DQ}" -e "${DQ}apikey${DQ}" -e "${DQ}password${DQ}" -e "${DQ}passwd${DQ}" -e "${DQ}credential${DQ}" -e "${DQ}access_key${DQ}"; then
  RISKS="${RISKS}[env-secret] JSON 含明文凭证字段名(token/secret/key/password,全文 grep -Fi 匹配 server 名/args 也命中,advisory 宁误报)。 "
fi

if [ -n "$RISKS" ]; then
  RISKS_SUMMARY=$(printf '%s' "$RISKS" | cut -c1-600)
  echo "PASS [mcp-scan] Advisory: 项目级 .mcp.json 发现风险信号——${RISKS_SUMMARY}请核查 server 来源是否可信(forge 不阻塞,agent/用户自检)。注:本扫描只审 config 层,runtime tool description 注入(Tool Poisoning)不在能力内。"
else
  echo "PASS [mcp-scan] 项目级 .mcp.json 无 config 层风险信号 (advisory; runtime tool description 注入不在扫描范围)"
fi
`
