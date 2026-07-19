package clone

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJaccardIdentical(t *testing.T) {
	a := []string{"func", "main", "println", "hello"}
	b := []string{"func", "main", "println", "hello"}
	sim := jaccardSimilarity(a, b)
	if sim != 1.0 {
		t.Errorf("identical sets should have similarity 1.0, got %.3f", sim)
	}
}

func TestJaccardDisjoint(t *testing.T) {
	a := []string{"a", "b", "c"}
	b := []string{"x", "y", "z"}
	sim := jaccardSimilarity(a, b)
	if sim != 0.0 {
		t.Errorf("disjoint sets should have similarity 0.0, got %.3f", sim)
	}
}

func TestJaccardPartial(t *testing.T) {
	a := []string{"func", "main", "println"}
	b := []string{"func", "test", "assert"}
	sim := jaccardSimilarity(a, b)
	// 1 intersection (func), union = 5 -> 0.2
	if sim < 0.19 || sim > 0.21 {
		t.Errorf("partial overlap should be ~0.2, got %.3f", sim)
	}
}

func TestTokenizeFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "test.go")
	content := []byte("package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n")
	if err := os.WriteFile(src, content, 0644); err != nil {
		t.Fatal(err)
	}

	tokens, err := tokenizeFile(src)
	if err != nil {
		t.Fatalf("tokenizeFile: %v", err)
	}
	if len(tokens) == 0 {
		t.Error("expected tokens, got empty")
	}
	// Verify tokens include key identifiers
	found := make(map[string]bool)
	for _, tok := range tokens {
		found[tok] = true
	}
	for _, want := range []string{"package", "main", "func", "main()", "println(\"hello\")"} {
		if !found[want] {
			t.Errorf("missing token %q", want)
		}
	}
}

func TestDetectClonesSelfExcluded(t *testing.T) {
	dir := t.TempDir()
	src1 := filepath.Join(dir, "a.go")
	src2 := filepath.Join(dir, "b.go")
	sameContent := []byte("package p\nfunc foo() {}\nfunc bar() {}\nfunc baz() {}\n")
	os.WriteFile(src1, sameContent, 0644)
	os.WriteFile(src2, sameContent, 0644)

	results, err := DetectClones(dir, src1, 0.7)
	if err != nil {
		t.Fatal(err)
	}
	// src1 should match src2 but not itself
	for _, r := range results {
		if r.FileA == r.FileB {
			t.Error("should not compare file with itself")
		}
	}
	// Identical content should produce high similarity
	if len(results) == 0 {
		t.Error("identical files should produce a clone detection")
	}
}

func TestDetectClonesDissimilar(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package p\nfunc uniqueA() { return 1 }\n"), 0644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("package p\nfunc uniqueB() { return 2 }\nfunc extra() {}\n"), 0644)

	results, err := DetectClones(dir, filepath.Join(dir, "a.go"), 0.7)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) > 0 {
		t.Errorf("dissimilar files should not match, got %d results", len(results))
	}
}

func TestDetectClonesSkipsVendor(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	same := []byte("package main\nfunc f() {}\nfunc g() {}\nfunc h() {}\n")
	os.WriteFile(src, same, 0644)

	vendorDir := filepath.Join(dir, "vendor", "lib")
	os.MkdirAll(vendorDir, 0755)
	os.WriteFile(filepath.Join(vendorDir, "main.go"), same, 0644)

	results, err := DetectClones(dir, src, 0.7)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if strings.Contains(r.FileB, "vendor/") {
			t.Error("vendor files should be skipped")
		}
	}
}
