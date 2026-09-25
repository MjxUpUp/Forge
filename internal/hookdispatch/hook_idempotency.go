package hookdispatch

// hook_idempotency.go —— dispatch 层短窗幂等守卫(escape-hatch-hardening P0-C)。
//
// 根因(2026-09-18 实证,zcode):forge 的钩子经两条通道同时进入 zcode——
// ① zcode translator 写入的用户级 ~/.zcode/cli/config.json(batch 接线,
// --agent zcode);② 用户从 plugins/forge 安装的 Claude Code 格式插件
// (.claude-plugin/plugin.json 全量 per-hook 接线,installed_plugins.json
// source=./plugins/forge)。zcode 两条都执行,且对 matcher 不敏感(实证:两侧
// Write|Edit 组均不含 tool-track,toollog 的 Write/Edit 行仍精确 2×——钩子组
// 按事件全量触发)。后果:每次工具调用 2× 进程拉起、SessionStart 注入双份、
// toollog 双行、test-nudge 抑制计数双记;3s block-dedup 曾长期掩盖重复 block。
//
// 修法(spec P0-C 候选 c,前两候选需卸载插件/破坏归因,弃):runHook 在窗口内
// 对 (hook,event,tool,session,input) 完全一致的重复调用幂等跳过——**仅观测型
// 钩子**(isObservationOnlyHook 白名单)。阻断型钩子(hazard-guard/task-guard/
// bash-guard/read-before-edit/gate-cmd-form/file-sentinel/task-drift…)**永不
// 跳过**:skip = 合法 allow,对阻断型是语义反转——deny 后 3s 内逐字重试同命令
// 会被静默放行执行(只读审查必改项 1,实证打挂 e2e 的 hazard release 与
// task-guard 阶梯测试)。双投递的第二次 deny 无害(重复拦截同一命令,各阻断型
// 钩子自带去重:hazard 的 3s blockDedupWindow 等)。窗口默认 3s(与
// blockDedupWindow 同量级——实证双通道间隔 ~1s),FORGE_HOOK_DEDUP_WINDOW
// 秒数旋钮,0=禁用,钳制上限 10s。fail-open 方向:竞态双读双跑 = 退回现状;
// 已知代价是并行同参调用的第二条少记一行 toollog(本就去重对象)。
// marker 随 DataDir(markers/hook-dedup/),>64 条时机会性清 >1h 陈旧文件。

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/util"
)

const (
	forgeHookDedupEnv      = "FORGE_HOOK_DEDUP_WINDOW"
	hookDedupWindowDefault = 3 * time.Second
	hookDedupWindowMax     = 10 * time.Second
)

// observationOnlyHooks 是幂等守卫的白名单:纯观测/注入型钩子——skip 丢失的
// 最多是一次重复记录或重复注入。任何具备阻断语义的钩子(含「现在 advisory、
// 未来 ratchet BLOCK」的)都不得入列:skip=allow 对它们是执法反转。列新钩子时
// 按「skip 的最坏后果」判:最坏=多一行日志 → 可入;最坏=放行危险命令 → 禁入。
var observationOnlyHooks = map[string]bool{
	"tool-track":          true, // toollog 双行——实证痛点
	"failure-track":       true, // 观察行
	"subagent-track":      true, // 观察行
	"test-nudge":          true, // 抑制计数双记
	"auto-compile":        true, // 重复 advisory 注入
	"skill-trigger":       true, // 重复注入(marker 幂等)
	"conventions-context": true,
	"conventions-write":   true,
	"doc-lint":            true,
	"workflow-test-guard": true, // PostToolUse:exit 1 是写后反馈通道,不可回溯 deny——skip 最坏=丢一次注入
	"resume-reinject":     true, // SessionStart/压缩注入
	"init-suggest":        true, // SessionStart 注入
	"skill-scan":          true, // SessionStart 注入(双份实证痛点)
	"mcp-scan":            true, // SessionStart 注入(双份实证痛点)
	"task-resume":         true, // SessionStart 注入
	"compact-resume":      true, // PostCompact 注入
}

func isObservationOnlyHook(name string) bool {
	return observationOnlyHooks[name]
}

// hookDedupWindow 解析幂等窗口(env 秒数旋钮;空=默认 3s;0=禁用;非法/负数
// 回落默认——旋钮永远不能因取值错误弄断 hook;上限钳 10s)。
func hookDedupWindow() time.Duration {
	v := strings.TrimSpace(os.Getenv(forgeHookDedupEnv))
	if v == "" {
		return hookDedupWindowDefault
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return hookDedupWindowDefault
	}
	if n == 0 {
		return 0
	}
	if d := time.Duration(n) * time.Second; d <= hookDedupWindowMax {
		return d
	}
	return hookDedupWindowMax
}

// skipDuplicateHookRun 报告并登记一次 (name × hookInput) 调用:窗口内已有完全
// 一致的调用 → true(调用方静默跳过);否则落时间戳并返回 false。key 刻意不含
// agent——跨通道去重(用户级 --agent zcode vs 插件无旗标)必须同名同键才命中;
// root 为空(global hook)不守。写 ts 早于执行:首跑超窗后第二通道放行 = 良性
// fail-open,与竞态双跑同界。
func skipDuplicateHookRun(root, name string, in HookInput) bool {
	w := hookDedupWindow()
	if w == 0 || root == "" || !isObservationOnlyHook(name) {
		return false
	}
	dir := filepath.Join(forgedata.DataDirFor(root), "markers", "hook-dedup")
	sum := sha256.Sum256([]byte(name + "\x00" + in.HookEventName + "\x00" + in.ToolName + "\x00" + in.SessionID + "\x00" + string(in.ToolInput)))
	path := filepath.Join(dir, hex.EncodeToString(sum[:16]))
	if data, err := os.ReadFile(path); err == nil {
		if ts, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); err == nil {
			if since := time.Since(time.Unix(ts, 0)); since >= 0 && since < w {
				return true
			}
		}
	}
	_ = os.MkdirAll(dir, 0o755)
	_ = util.AtomicWrite(path, []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0o644)
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 64 {
		cutoff := time.Now().Add(-time.Hour)
		for _, e := range entries {
			if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
	return false
}
