package calc

import "errors"

// ErrDivideByZero 在除数为零时返回。
var ErrDivideByZero = errors.New("divide by zero")

// Add 返回两数之和。
func Add(a, b int) int { return a + b }

// Div 返回 a/b，b 为零时报错。
func Div(a, b int) (int, error) {
	if b == 0 {
		return 0, ErrDivideByZero
	}
	return a / b, nil
}
