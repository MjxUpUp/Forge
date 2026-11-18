package change

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestGateAndVerifyShareFirstError 钉住 gate 与 verify 的首错同序：
// 产物没填完时两边都必须先报「产物还不完整」，不能一边先抱怨缺 clarify。
// 没有这条测试时，把 gate.go 的 DocProblems 短路掉仍然全绿（review-rfe-1 P3-b）。
func TestGateAndVerifyShareFirstError(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "parity")
	writeTasks(t, dir, "- [ ] 第一步（判据：acceptance #1）")
	// 删掉 change.md：产物不完整 + clarify 也缺，两边都该先说产物不完整。
	if err := os.Remove(filepath.Join(dir, FileChange)); err != nil {
		t.Fatal(err)
	}
	_, vErr := Verify(context.Background(), root, "parity", VerifyOptions{Timeout: 10 * time.Second})
	_, gErr := GateTask(context.Background(), root, "parity", 1, GateOptions{Timeout: 10 * time.Second})
	if vErr == nil || gErr == nil {
		t.Fatalf("脚手架 change 应被两边拒绝：verify=%v gate=%v", vErr, gErr)
	}
	if !strings.Contains(vErr.Error(), "产物还不完整") {
		t.Fatalf("verify 首错应为产物不完整，got %v", vErr)
	}
	if !strings.Contains(gErr.Error(), "产物还不完整") {
		t.Fatalf("gate 首错应与 verify 同序（产物不完整），got %v", gErr)
	}
}
