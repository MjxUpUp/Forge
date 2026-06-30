package skillsdist

import (
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/util"
)

// AdapterSpec 是一个 skill-routing adapter 单文件分发定义（对齐 sync.py ADAPTERS）。
// Codex 无文件型 adapter（经 AGENTS.md 文字注入），故此处只有 4 个文件型。
type AdapterSpec struct {
	SrcRel string // 相对 canonical 根的源文件路径
	Dst    string // 目标绝对路径（基于 home）
}

// Adapters 返回 4 个 skill-routing adapter 规格（对齐 sync.py ADAPTERS 常量）。
func Adapters(home string) []AdapterSpec {
	return []AdapterSpec{
		{"skill-routing/adapters/pi/index.ts", filepath.Join(home, ".pi", "agent", "extensions", "skill-router", "index.ts")},
		{"skill-routing/adapters/claude/skill-router-claude.sh", filepath.Join(home, ".claude", "hooks", "skill-router-claude.sh")},
		{"skill-routing/adapters/cursor/skill-routing.mdc", filepath.Join(home, ".cursor", "rules", "skill-routing.mdc")},
		{"skill-routing/routes.json", filepath.Join(home, ".pi", "agent", "skill-routes.json")},
	}
}

// AdapterAction 是单 adapter 文件的部署决策。
type AdapterAction struct {
	Spec   AdapterSpec
	Action string // "deploy" | "ok" | "skip"
	Detail string
}

// PlanAdapters 对比 canonical 源与目标，返回每个 adapter 的动作（不执行，dry-run）。
func PlanAdapters(canonical, home string) []AdapterAction {
	specs := Adapters(home)
	out := make([]AdapterAction, 0, len(specs))
	for _, sp := range specs {
		srcData, err := os.ReadFile(filepath.Join(canonical, sp.SrcRel))
		if err != nil {
			out = append(out, AdapterAction{Spec: sp, Action: "skip", Detail: "源文件缺失"})
			continue
		}
		dstData, err := os.ReadFile(sp.Dst)
		if err != nil {
			out = append(out, AdapterAction{Spec: sp, Action: "deploy", Detail: "目标缺失"})
			continue
		}
		if string(dstData) != string(srcData) {
			out = append(out, AdapterAction{Spec: sp, Action: "deploy", Detail: "内容分叉"})
		} else {
			out = append(out, AdapterAction{Spec: sp, Action: "ok", Detail: "已一致"})
		}
	}
	return out
}

// DeployAdapters 执行所有 deploy 动作（单文件原子拷贝），返回部署数 + 最终计划。
func DeployAdapters(canonical, home string) (int, []AdapterAction, error) {
	plan := PlanAdapters(canonical, home)
	done := 0
	for i := range plan {
		a := &plan[i]
		if a.Action != "deploy" {
			continue
		}
		src := filepath.Join(canonical, a.Spec.SrcRel)
		data, err := os.ReadFile(src)
		if err != nil {
			return done, plan, err
		}
		if err := os.MkdirAll(filepath.Dir(a.Spec.Dst), 0755); err != nil {
			return done, plan, err
		}
		if err := util.AtomicWrite(a.Spec.Dst, data, 0644); err != nil {
			return done, plan, err
		}
		done++
		a.Action = "ok"
		a.Detail = "已部署"
	}
	return done, plan, nil
}
