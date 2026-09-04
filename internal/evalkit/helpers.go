package evalkit

// helpers.go — evalkit 内部的薄工具封装（单一出口，路径/写盘/JSON 约定集中一处）。
//
// helpers.go — thin shared helpers (single exit point for path/write/JSON
// conventions inside evalkit).

import (
	"encoding/json"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/util"
)

// evalDataDir resolves <evalDir>/forge — this subsystem's user-level data root.
//
// evalDataDir 解析 <evalDir>/forge——本子系统的用户级数据根。
func evalDataDir(evalDir string) string { return filepath.Join(evalDir, "forge") }

func jsonMarshal(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }

func filepathJoin(elem ...string) string { return filepath.Join(elem...) }

func atomicWriteFile(path string, data []byte) error { return util.AtomicWrite(path, data, 0o644) }
