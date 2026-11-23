package change

import (
	"fmt"

	"github.com/MjxUpUp/Forge/internal/agentproto"
	"github.com/MjxUpUp/Forge/internal/capability"
)

// SurfaceCommand 让 VerifyOptions 不必直接 import agentproto（同包类型别名）。
type SurfaceCommand = agentproto.Command

// Capability 是 verify 摘要里的能力面闭合表：文档里有多少处教了不存在的动作。
// 它不参与封印判定（封印只认判据实跑），但必须进摘要——否则「文档与二进制对不上」
// 这件事只能靠人记着问。
type Capability struct {
	Commands   int      `json:"commands"`
	Flags      int      `json:"flags"`
	Annotated  int      `json:"annotated"`
	DriftCount int      `json:"drift_count"`
	Drifts     []string `json:"drifts,omitempty"`
}

func buildCapability(root string, surface []SurfaceCommand) *Capability {
	if len(surface) == 0 {
		return nil
	}
	cp := &Capability{Commands: len(surface), Annotated: len(agentproto.Annotations)}
	for _, c := range surface {
		cp.Flags += len(c.Flags)
	}
	files, err := capability.LiveDocs(root)
	if err != nil {
		return cp
	}
	drifts, err := capability.ScanFiles(root, files, surface)
	if err != nil {
		return cp
	}
	cp.DriftCount = len(drifts)
	for _, d := range drifts {
		cp.Drifts = append(cp.Drifts, fmt.Sprintf("%s:%d %s", d.File, d.Line, d.Path))
	}
	return cp
}
