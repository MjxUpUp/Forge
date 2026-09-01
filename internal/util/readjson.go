package util

import (
	"encoding/json"
	"fmt"
	"os"
)

// ReadJSONFile reads path and unmarshals it into v — the single read-side
// entry for the "ReadFile → IsNotExist → Unmarshal" pattern (2026-09 census
// P3-6; the write side has long been single-sourced on AtomicWrite).
//
// Contract:
//   - file missing → bare os.ErrNotExist (callers branch on errors.Is(err, fs.ErrNotExist))
//   - read failure → error wrapped with the path
//   - parse failure → error wrapped with the path
//
// Callers keep their own missing-file semantics (nil / zero-value / sentinel
// message) — this helper only unifies the mechanics and the path context.
//
// ReadJSONFile 读 path 并反序列化进 v——"ReadFile → IsNotExist → Unmarshal"
// 模式的读侧单一入口（2026-09 普查 P3-6；写侧早已单一源 AtomicWrite）。
//
// 契约：
//   - 文件不存在 → 裸 os.ErrNotExist（调用方用 errors.Is(err, fs.ErrNotExist) 分支）
//   - 读失败 → 带路径包装的错误
//   - 解析失败 → 带路径包装的错误
//
// missing 语义（nil / 零值 / 哨兵消息）由调用方各自保留——本助手只统一机制与
// 路径上下文。
func ReadJSONFile(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return err
		}
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}
