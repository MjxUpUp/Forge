package pipeline

import (
	"fmt"
	"sort"
)

// DAGError records a pipeline DAG validation error.
type DAGError struct {
	Type    string // "cycle", "missing_dependency", "duplicate_id"
	Message string
	Detail  string
}

func (e *DAGError) Error() string {
	return e.Message
}

// ValidateDAG checks the pipeline for structural errors:
//   - duplicate gate IDs
//   - missing dependency references
//   - circular dependencies (cycles)
//
// On success, it caches the topological order on the Pipeline.
func (p *Pipeline) ValidateDAG() error {
	gates := p.PipelineDef.Gates

	// Check for duplicate IDs
	seen := make(map[string]bool)
	for _, g := range gates {
		if seen[g.ID] {
			return &DAGError{
				Type:    "duplicate_id",
				Message: fmt.Sprintf("duplicate gate ID: %s", g.ID),
				Detail:  g.ID,
			}
		}
		seen[g.ID] = true
	}

	// Check for missing dependency references
	for _, g := range gates {
		for _, dep := range g.DependsOn {
			if !seen[dep] {
				return &DAGError{
					Type:    "missing_dependency",
					Message: fmt.Sprintf("gate '%s' depends on non-existent gate '%s'", g.ID, dep),
					Detail:  fmt.Sprintf("%s -> %s", g.ID, dep),
				}
			}
		}
	}

	// Kahn's algorithm for topological sort + cycle detection
	// Build adjacency list and in-degree map (only for enabled gates)
	enabled := make(map[string]bool)
	for _, g := range gates {
		if g.Enabled {
			enabled[g.ID] = true
		}
	}

	inDegree := make(map[string]int)
	successors := make(map[string][]string)
	for _, g := range gates {
		if !g.Enabled {
			continue
		}
		inDegree[g.ID] = 0 // ensure all enabled gates appear
		successors[g.ID] = nil
	}
	for _, g := range gates {
		if !g.Enabled {
			continue
		}
		for _, dep := range g.DependsOn {
			if enabled[dep] {
				successors[dep] = append(successors[dep], g.ID)
				inDegree[g.ID]++
			}
		}
	}

	// Initialize queue with zero in-degree gates
	var queue []string
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}
	sort.Strings(queue) // deterministic order

	var sorted []string
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		sorted = append(sorted, current)

		nextBatch := successors[current]
		sort.Strings(nextBatch)
		for _, next := range nextBatch {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	if len(sorted) < len(inDegree) {
		// Find which gates are in the cycle
		var inCycle []string
		for id, deg := range inDegree {
			if deg > 0 {
				inCycle = append(inCycle, id)
			}
		}
		sort.Strings(inCycle)
		return &DAGError{
			Type:    "cycle",
			Message: fmt.Sprintf("circular dependency detected among gates: %v", inCycle),
			Detail:  fmt.Sprintf("%v", inCycle),
		}
	}

	// Cache topological order
	p.topoOrder = make(map[string]int)
	for i, id := range sorted {
		p.topoOrder[id] = i
	}

	return nil
}

// TopoOrder returns the cached topological order (gate IDs in execution order).
// Must call ValidateDAG() first.
func (p *Pipeline) TopoOrder() []string {
	if p.topoOrder == nil {
		return nil
	}
	type entry struct {
		id    string
		order int
	}
	var entries []entry
	for id, order := range p.topoOrder {
		entries = append(entries, entry{id, order})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].order < entries[j].order
	})
	result := make([]string, len(entries))
	for i, e := range entries {
		result[i] = e.id
	}
	return result
}

// NextReadyGate returns the next enabled gate whose dependencies are all satisfied.
// completedGates maps gate ID -> bool (true = passed).
// Returns "" if no gate is ready or all are complete.
func (p *Pipeline) NextReadyGate(completedGates map[string]bool) string {
	order := p.TopoOrder()
	if order == nil {
		return ""
	}
	for _, id := range order {
		if completedGates[id] {
			continue // already done
		}
		gate, err := p.GetGate(id)
		if err != nil {
			continue
		}
		if !gate.Enabled {
			continue
		}
		// Check all dependencies are completed
		allMet := true
		for _, dep := range gate.DependsOn {
			if !completedGates[dep] {
				allMet = false
				break
			}
		}
		if allMet {
			return id
		}
	}
	return ""
}

// EnabledGates returns all enabled gates in topological order.
func (p *Pipeline) EnabledGates() []Gate {
	var gates []Gate
	order := p.TopoOrder()
	if order != nil {
		for _, id := range order {
			if g, err := p.GetGate(id); err == nil && g.Enabled {
				gates = append(gates, *g)
			}
		}
		return gates
	}
	// Fallback: list order (should not happen after ValidateDAG)
	for _, g := range p.PipelineDef.Gates {
		if g.Enabled {
			gates = append(gates, g)
		}
	}
	return gates
}

// GetGate returns a gate by ID.
func (p *Pipeline) GetGate(gateID string) (*Gate, error) {
	for i := range p.PipelineDef.Gates {
		if p.PipelineDef.Gates[i].ID == gateID {
			return &p.PipelineDef.Gates[i], nil
		}
	}
	return nil, fmt.Errorf("gate '%s' not found in pipeline.yml", gateID)
}
