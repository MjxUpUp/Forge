package taskpipeline

import (
	"fmt"
	"strings"
)

// Gate 消息契约：gate 结果携带两个无歧义前缀之一，使 agent 不必解析散文即可知道
// 是否被门禁拦下。
//
//	BLOCKED:  — 硬失败，非零退出。门禁未通过。修了重跑。
//	ADVISORY: — 软信号，exit 0（门禁仍过）。记入 checklog；应修但不阻塞。
//
// 通过的 gate 打印 ✅ <gate> — passed，无需前缀。
//
// 此契约源于真实失败模式：硬性 read-before-edit 错误（Read and understand the
// code before modifying it）被当成软提醒，agent 照常继续——因措辞像提醒而非
// 阻断，非零退出被忽略。BLOCKED: 前缀 + 显式 HARD stop, not a reminder 让契约
// 不再可误解，并配 ADVISORY: 走软路径。镜像业界 signal-over-noise / Block pattern
// 共识：pass 静默、fail 喧哗，agent 须遵从的是 exit code——而非措辞。
const (
	// blockedPrefix 标记硬性、非零退出的 gate 失败。
	blockedPrefix = "BLOCKED: "
	// advisoryPrefix 标记软性、exit 0 的 gate 信号（记日志、不阻塞）。
	advisoryPrefix = "ADVISORY: "
)

// GateBlocked 以 BLOCKED 契约包装 agent 可行动的硬门禁失败。返回的 error 传播
// 到非零进程退出；前缀使阻断无歧义，不会被误读为软提醒。用于行为门禁
// （read-before-edit、work-activity、prerequisites、review）——不用于基础设施
// 错误（未知 gate id、命令执行失败），后者保持普通 fmt.Errorf。
func GateBlocked(format string, args ...any) error {
	return fmt.Errorf(blockedPrefix+format, args...)
}

// IsGateBlocked 报告 err 是否为硬性 BLOCKED gate 失败。供基于契约分支（而非
// 仅看非零退出）的 caller/hook 使用。
func IsGateBlocked(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), blockedPrefix)
}

// GateAdvisory 格式化软性、不阻塞的 gate 信号。caller 仍返回 nil / exit 0；
// ADVISORY: 前缀告诉 agent 这是应修但不阻塞——区别于 BLOCKED 硬失败。用于
// agent 可行动的软信号（缺测试、docs drift），不用于内部诊断（grace-window 提示、
// persist 警告），后者保持 [forge] note。
func GateAdvisory(format string, args ...any) string {
	return advisoryPrefix + fmt.Sprintf(format, args...)
}
