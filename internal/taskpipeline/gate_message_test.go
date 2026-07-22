package taskpipeline

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestGateBlockedPrefix: hard gate failures carry the BLOCKED: contract prefix
// so an agent cannot misread imperative-stop prose as soft advice (the M2 mode).
func TestGateBlockedPrefix(t *testing.T) {
	err := GateBlocked("gate %q cannot pass (edits=%d)", "task-verify", 3)
	if err == nil {
		t.Fatal("GateBlocked returned nil error")
	}
	if !strings.HasPrefix(err.Error(), blockedPrefix) {
		t.Fatalf("GateBlocked error = %q, want %q prefix", err.Error(), blockedPrefix)
	}
	if !strings.Contains(err.Error(), "cannot pass") {
		t.Fatalf("GateBlocked error = %q, want interpolated message", err.Error())
	}
}

// TestIsGateBlocked: the contract predicate distinguishes BLOCKED from plain errors.
func TestIsGateBlocked(t *testing.T) {
	if !IsGateBlocked(GateBlocked("hard stop")) {
		t.Error("IsGateBlocked(GateBlocked(...)) = false, want true")
	}
	if IsGateBlocked(fmt.Errorf("infrastructure error")) {
		t.Error("IsGateBlocked(plain error) = true, want false (infra errors stay un-prefixed)")
	}
	if IsGateBlocked(nil) {
		t.Error("IsGateBlocked(nil) = true, want false")
	}
	if IsGateBlocked(errors.New("ADVISORY: soft signal")) {
		t.Error("IsGateBlocked(advisory) = true, want false")
	}
}

// TestGateAdvisoryPrefix: soft signals carry the ADVISORY: prefix, exit-0 contract.
func TestGateAdvisoryPrefix(t *testing.T) {
	msg := GateAdvisory("[task-verify] %s", "missing tests")
	if !strings.HasPrefix(msg, advisoryPrefix) {
		t.Fatalf("GateAdvisory = %q, want %q prefix", msg, advisoryPrefix)
	}
	if !strings.Contains(msg, "missing tests") {
		t.Fatalf("GateAdvisory = %q, want interpolated message", msg)
	}
}
