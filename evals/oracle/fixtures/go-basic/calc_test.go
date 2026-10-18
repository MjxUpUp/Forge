package calc

import "testing"

func TestAdd(t *testing.T) {
	if got := Add(2, 3); got != 5 {
		t.Fatalf("Add(2,3)=%d want 5", got)
	}
}

func TestDiv(t *testing.T) {
	got, err := Div(6, 2)
	if err != nil || got != 3 {
		t.Fatalf("Div(6,2)=%d,%v want 3,nil", got, err)
	}
}
