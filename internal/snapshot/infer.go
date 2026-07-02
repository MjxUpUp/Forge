package snapshot

import (
	"fmt"
	"strings"

	"github.com/MjxUpUp/Forge/internal/pipeline"
)

// InferCompletedGates scans project signals and determines which gates
// can be considered already completed for an existing project.
//
// The inference is monotonic: if gate N is inferred, all gates that
// gate N depends on (transitively) must also be inferred. If a gate's
// signals are met but its dependency's signals are not, the gate is
// NOT inferred — the chain must be continuous from the beginning.
func InferCompletedGates(snap *ProjectSnapshot, p *pipeline.Pipeline) []InferredGate {
	topo := p.TopoOrder()
	if len(topo) == 0 {
		return nil
	}

	signals := &snap.Signals

	// Build a map of gate ID -> inference result.
	// A gate is inferable if:
	//   1. Its own signals are met
	//   2. ALL its depends_on gates are also inferable
	inferable := make(map[string]*InferredGate)

	for _, gateID := range topo {
		gate, err := p.GetGate(gateID)
		if err != nil || !gate.Enabled {
			continue
		}

		// Check all dependencies are already inferred
		depsMet := true
		for _, dep := range gate.DependsOn {
			if _, ok := inferable[dep]; !ok {
				depsMet = false
				break
			}
		}
		if !depsMet {
			continue
		}

		// Check if this gate's signals are met
		ig := checkGateSignals(gateID, signals)
		if ig != nil {
			inferable[gateID] = ig
		}
	}

	// Return in topological order
	var result []InferredGate
	for _, gateID := range topo {
		if ig, ok := inferable[gateID]; ok {
			result = append(result, *ig)
		}
	}
	return result
}

// checkGateSignals returns an InferredGate if the gate's completion
// can be inferred from project signals, or nil if not.
func checkGateSignals(gateID string, s *Signals) *InferredGate {
	switch gateID {
	case "gate-0-research":
		return checkResearch(s)
	case "gate-1-prd":
		return checkPRD(s)
	case "gate-2-design":
		return checkDesign(s)
	case "gate-3-plan":
		return checkPlan(s)
	case "gate-4-implement":
		return checkImplement(s)
	case "gate-5-test":
		return checkTest(s)
	case "gate-6-acceptance":
		return checkAcceptance(s)
	case "gate-7-archive":
		return checkArchive(s)
	case "gate-8-release":
		// Release is never inferred — it requires explicit action.
		return nil
	default:
		return nil
	}
}

func checkResearch(s *Signals) *InferredGate {
	// Research is done if the project has significant git history
	// (implies the idea has been validated through earlier work)
	if s.CommitCount >= 20 {
		return &InferredGate{
			GateID:  "gate-0-research",
			Reason:  "project has significant development history",
			Signals: []string{fmt.Sprintf("%d commits", s.CommitCount)},
		}
	}
	return nil
}

func checkPRD(s *Signals) *InferredGate {
	// PRD is done if:
	// - Project has README + any git history (committed code = requirements exist)
	// - OR project has package manager + source code (structured project implies requirements)
	var sigs []string
	var reasons []string

	if s.HasREADME && s.HasGitHistory {
		sigs = append(sigs, "README.md", fmt.Sprintf("%d commits", s.CommitCount))
		reasons = append(reasons, "established scope")
	} else if s.HasPkgManager && s.HasSourceCode && s.SourceFileCount >= 3 {
		sigs = append(sigs, strings.Join(s.PkgManagerFiles, "/"), fmt.Sprintf("%d source files", s.SourceFileCount))
		reasons = append(reasons, "structured codebase implies clear requirements")
		sigs = append(sigs, "README.md", fmt.Sprintf("%d commits", s.CommitCount))
		reasons = append(reasons, "established scope")
	} else if s.HasPkgManager && s.SourceFileCount >= 10 {
		sigs = append(sigs, strings.Join(s.PkgManagerFiles, "/"), fmt.Sprintf("%d source files", s.SourceFileCount))
		reasons = append(reasons, "structured codebase with clear requirements")
	}

	if len(reasons) > 0 {
		return &InferredGate{
			GateID:  "gate-1-prd",
			Reason:  strings.Join(reasons, ", "),
			Signals: sigs,
		}
	}
	return nil
}

func checkDesign(s *Signals) *InferredGate {
	// Design is done if project has substantial source code
	// (implies architectural decisions have been made)
	if s.HasSourceCode && s.SourceFileCount >= 20 && s.SourceDirs >= 3 {
		return &InferredGate{
			GateID:  "gate-2-design",
			Reason:  "project has established architecture",
			Signals: []string{fmt.Sprintf("%d source files in %d directories", s.SourceFileCount, s.SourceDirs)},
		}
	}
	return nil
}

func checkPlan(s *Signals) *InferredGate {
	// Plan is done if project has a structured codebase
	if s.HasSourceCode && s.HasPkgManager && s.SourceFileCount >= 5 {
		return &InferredGate{
			GateID:  "gate-3-plan",
			Reason:  "project has structured implementation",
			Signals: []string{fmt.Sprintf("%d source files", s.SourceFileCount)},
		}
	}
	return nil
}

func checkImplement(s *Signals) *InferredGate {
	// Implementation is done if project has source code + package manager
	if s.HasSourceCode && s.HasPkgManager {
		return &InferredGate{
			GateID:  "gate-4-implement",
			Reason:  "project has working code",
			Signals: []string{fmt.Sprintf("%d source files", s.SourceFileCount)},
		}
	}
	// Also match if significant source code exists even without recognized pkg manager
	if s.SourceFileCount >= 10 {
		return &InferredGate{
			GateID:  "gate-4-implement",
			Reason:  "project has substantial codebase",
			Signals: []string{fmt.Sprintf("%d source files", s.SourceFileCount)},
		}
	}
	return nil
}

func checkTest(s *Signals) *InferredGate {
	// Tests are done if test files exist
	if s.HasTests {
		return &InferredGate{
			GateID:  "gate-5-test",
			Reason:  "project has tests",
			Signals: []string{fmt.Sprintf("%d test files", s.TestFileCount)},
		}
	}
	return nil
}

func checkAcceptance(s *Signals) *InferredGate {
	// Acceptance is done if project has README + CHANGELOG
	// (implies the project has been documented as "done")
	if s.HasREADME && s.HasCHANGELOG {
		return &InferredGate{
			GateID:  "gate-6-acceptance",
			Reason:  "project has release documentation",
			Signals: []string{"README.md", "CHANGELOG.md"},
		}
	}
	return nil
}

func checkArchive(s *Signals) *InferredGate {
	// Archive is done if project has very significant history
	// (implies lessons have been learned, even if not formally documented)
	if s.CommitCount >= 50 {
		return &InferredGate{
			GateID:  "gate-7-archive",
			Reason:  "project has mature development history",
			Signals: []string{fmt.Sprintf("%d commits", s.CommitCount)},
		}
	}
	return nil
}

// FormatInferred returns a human-readable summary of inferred gates.
func FormatInferred(gates []InferredGate) string {
	if len(gates) == 0 {
		return "  (no gates inferred — pipeline starts from the beginning)"
	}
	var lines []string
	for _, ig := range gates {
		lines = append(lines, fmt.Sprintf("  ✓ %-20s — %s (%s)", ig.GateID, ig.Reason, strings.Join(ig.Signals, ", ")))
	}
	return strings.Join(lines, "\n")
}

// InferredGateIDs returns the set of inferred gate IDs for quick lookup.
func InferredGateIDs(gates []InferredGate) map[string]bool {
	m := make(map[string]bool, len(gates))
	for _, ig := range gates {
		m[ig.GateID] = true
	}
	return m
}
