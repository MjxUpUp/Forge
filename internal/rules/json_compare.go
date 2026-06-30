package rules

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
)

// JSONEqualsEvaluator checks json_equals: field value equals expected value.
type JSONEqualsEvaluator struct{}

func (e *JSONEqualsEvaluator) Evaluate(ctx Context, params CheckParams) Result {
	absPath := filepath.Join(ctx.GateDir, params.File)
	data, err := os.ReadFile(absPath)
	if err != nil {
		return Result{
			Name:    "json_equals",
			Passed:  false,
			Detail:  fmt.Sprintf("file not found: %s", params.File),
			Message: fmt.Sprintf("Required file %s not found", params.File),
		}
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return Result{
			Name:    "json_equals",
			Passed:  false,
			Detail:  fmt.Sprintf("invalid JSON in %s", params.File),
			Message: fmt.Sprintf("File %s does not contain valid JSON object", params.File),
		}
	}

	actual := getJSONField(m, params.Field)
	if actual == nil {
		return Result{
			Name:    "json_equals",
			Passed:  false,
			Detail:  fmt.Sprintf("field %s not found in %s", params.Field, params.File),
			Message: fmt.Sprintf("JSON field '%s' not found in %s", params.Field, params.File),
		}
	}

	if compareJSONValues(actual, params.Value) {
		return Result{
			Name:   "json_equals",
			Passed: true,
			Detail: fmt.Sprintf("%s == %v", params.Field, actual),
		}
	}
	return Result{
		Name:    "json_equals",
		Passed:  false,
		Detail:  fmt.Sprintf("%s == %v (expected: %v)", params.Field, actual, params.Value),
		Message: fmt.Sprintf("Field %s is %v, expected %v", params.Field, actual, params.Value),
	}
}

// JSONCompareEvaluator checks json_gte / json_lte.
type JSONCompareEvaluator struct {
	Op string // ">=" or "<="
}

func (e *JSONCompareEvaluator) Evaluate(ctx Context, params CheckParams) Result {
	absPath := filepath.Join(ctx.GateDir, params.File)
	data, err := os.ReadFile(absPath)
	if err != nil {
		return Result{
			Name:    fmt.Sprintf("json_%s", e.Op),
			Passed:  false,
			Detail:  fmt.Sprintf("file not found: %s", params.File),
			Message: fmt.Sprintf("Required file %s not found", params.File),
		}
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return Result{
			Name:    fmt.Sprintf("json_%s", e.Op),
			Passed:  false,
			Detail:  fmt.Sprintf("invalid JSON in %s", params.File),
			Message: fmt.Sprintf("File %s does not contain valid JSON object", params.File),
		}
	}

	actual := getJSONField(m, params.Field)
	actualNum := toFloat64(actual)
	expectedNum := toFloat64(params.Value)

	passed := false
	switch e.Op {
	case ">=":
		passed = actualNum >= expectedNum
	case "<=":
		passed = actualNum <= expectedNum
	}

	detail := fmt.Sprintf("%s %.0f %s %.0f", params.Field, actualNum, e.Op, expectedNum)
	if passed {
		return Result{Name: fmt.Sprintf("json_%s", e.Op), Passed: true, Detail: detail}
	}
	return Result{
		Name:    fmt.Sprintf("json_%s", e.Op),
		Passed:  false,
		Detail:  detail,
		Message: detail + " (failed)",
	}
}

// JSONArrayMinCountEvaluator checks that a JSON file has at least min_count elements.
type JSONArrayMinCountEvaluator struct{}

func (e *JSONArrayMinCountEvaluator) Evaluate(ctx Context, params CheckParams) Result {
	absPath := filepath.Join(ctx.GateDir, params.File)
	data, err := os.ReadFile(absPath)
	if err != nil {
		return Result{
			Name:    "json_array_min_count",
			Passed:  false,
			Detail:  fmt.Sprintf("file not found: %s", params.File),
			Message: fmt.Sprintf("Required file %s not found", params.File),
		}
	}

	var arr []interface{}
	if err := json.Unmarshal(data, &arr); err == nil {
		count := len(arr)
		passed := count >= params.MinCount
		detail := fmt.Sprintf("array length %d >= %d", count, params.MinCount)
		if passed {
			return Result{Name: "json_array_min_count", Passed: true, Detail: detail}
		}
		return Result{
			Name:    "json_array_min_count",
			Passed:  false,
			Detail:  detail,
			Message: fmt.Sprintf("Array has %d elements, need at least %d", count, params.MinCount),
		}
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return Result{
			Name:    "json_array_min_count",
			Passed:  false,
			Detail:  fmt.Sprintf("invalid JSON in %s", params.File),
			Message: fmt.Sprintf("File %s is not a JSON array or object", params.File),
		}
	}

	if countVal, ok := m["count"]; ok {
		count := toFloat64(countVal)
		passed := int(count) >= params.MinCount
		detail := fmt.Sprintf("count %.0f >= %d", count, params.MinCount)
		if passed {
			return Result{Name: "json_array_min_count", Passed: true, Detail: detail}
		}
		return Result{
			Name:    "json_array_min_count",
			Passed:  false,
			Detail:  detail,
			Message: fmt.Sprintf("Count is %.0f, need at least %d", count, params.MinCount),
		}
	}

	return Result{
		Name:    "json_array_min_count",
		Passed:  false,
		Detail:  fmt.Sprintf("%s is not a JSON array and has no 'count' field", params.File),
		Message: fmt.Sprintf("File %s is not a JSON array and has no 'count' field", params.File),
	}
}

func getJSONField(m map[string]interface{}, path string) interface{} {
	parts := splitFieldPath(path)
	current := m
	for i, p := range parts {
		if i == len(parts)-1 {
			return current[p]
		}
		val, ok := current[p]
		if !ok {
			return nil
		}
		if nm, ok := val.(map[string]interface{}); ok {
			current = nm
		} else {
			return nil
		}
	}
	return nil
}

func splitFieldPath(path string) []string {
	var parts []string
	current := ""
	for _, ch := range path {
		if ch == '.' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case json.Number:
		f, _ := val.Float64()
		return f
	case string:
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return 0
		}
		return f
	default:
		return 0
	}
}

func compareJSONValues(actual, expected interface{}) bool {
	if actual == nil && expected == nil {
		return true
	}
	if actual == nil || expected == nil {
		return false
	}
	// Try numeric comparison first — JSON numbers are float64, YAML ints are int.
	// Both must be actual numeric types (not strings or bools) to enter numeric path.
	_, actualIsStr := actual.(string)
	_, expectedIsStr := expected.(string)
	if !actualIsStr && !expectedIsStr && isNumericType(actual) && isNumericType(expected) {
		return toFloat64(actual) == toFloat64(expected)
	}
	return reflect.DeepEqual(actual, expected)
}

// isNumericType returns true if v is a numeric type (not a string or bool).
func isNumericType(v interface{}) bool {
	switch v.(type) {
	case float64, float32, int, int64, json.Number:
		return true
	default:
		return false
	}
}
