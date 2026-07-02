package rules

import (
	"fmt"
	"sort"
	"sync"
)

// registry maps rule type names to their evaluator implementations.
var registry = map[string]Evaluator{
	"file_contains":        &FileContainsEvaluator{Negated: false},
	"file_not_contains":    &FileContainsEvaluator{Negated: true},
	"json_equals":          &JSONEqualsEvaluator{},
	"json_gte":             &JSONCompareEvaluator{Op: ">="},
	"json_lte":             &JSONCompareEvaluator{Op: "<="},
	"json_array_min_count": &JSONArrayMinCountEvaluator{},
	"file_exists":          &FileExistsEvaluator{},
	"all_gates_passed":     &AllGatesPassedEvaluator{},
	"custom_script":        &CustomScriptEvaluator{},
	"knowledge_check":      &KnowledgeCheckEvaluator{},
}

// mu protects registry for concurrent read/write access.
var mu sync.RWMutex

// KnownTypes returns all registered rule type names, sorted for deterministic output.
func KnownTypes() []string {
	mu.RLock()
	defer mu.RUnlock()
	types := make([]string, 0, len(registry))
	for t := range registry {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

// IsKnownType checks whether a rule type is registered.
func IsKnownType(t string) bool {
	mu.RLock()
	defer mu.RUnlock()
	_, ok := registry[t]
	return ok
}

// Register adds a custom evaluator for a rule type.
func Register(typeName string, eval Evaluator) {
	mu.Lock()
	defer mu.Unlock()
	registry[typeName] = eval
}

// EvaluateChecks evaluates all checks for a gate.
// Fail-safe: any check with an unknown type immediately FAILS.
func EvaluateChecks(ctx Context, checks []Check) []Result {
	var results []Result
	for _, c := range checks {
		mu.RLock()
		eval, ok := registry[c.Type]
		mu.RUnlock()
		if !ok {
			results = append(results, Result{
				Name:    c.Name,
				Type:    c.Type,
				Passed:  false,
				Detail:  fmt.Sprintf("unknown rule type '%s'", c.Type),
				Message: fmt.Sprintf("Check '%s' uses unknown rule type '%s'. Valid types: %v", c.Name, c.Type, KnownTypes()),
			})
			continue
		}
		r := eval.Evaluate(ctx, c.Params)
		r.Name = c.Name
		r.Type = c.Type
		results = append(results, r)
	}
	return results
}
