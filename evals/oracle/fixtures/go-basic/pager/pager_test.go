package pager

import "testing"

func TestFirstPage(t *testing.T) {
	got := Page([]int{1, 2, 3, 4}, 2, 0)
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("first page = %v want [1 2]", got)
	}
}
