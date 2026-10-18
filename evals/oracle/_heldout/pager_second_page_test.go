package pager

import "testing"

func TestSecondPage(t *testing.T) {
	got := Page([]int{1, 2, 3, 4}, 2, 1)
	if len(got) != 2 || got[0] != 3 || got[1] != 4 {
		t.Fatalf("second page = %v want [3 4]", got)
	}
}
