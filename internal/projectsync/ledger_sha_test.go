package projectsync

// ledger_sha_test.go — HasImportedSHA: digest-level ledger hits (before-unpack
// dedup), empty-digest never matches, corrupt lines skipped.
//
// ledger_sha_test.go —— HasImportedSHA：digest 级账本命中（解包前查重）、空摘要
// 永不命中、坏行跳过。

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHasImportedSHA(t *testing.T) {
	dir := t.TempDir()
	if ok, err := HasImportedSHA(dir, `abc123`); err != nil || ok {
		t.Fatalf("empty ledger must be a miss, got ok=%v err=%v", ok, err)
	}
	if err := AppendImportRecord(dir, ImportRecord{BundleID: `b1`, SHA256: `abc123`, ImportedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if ok, err := HasImportedSHA(dir, `abc123`); err != nil || !ok {
		t.Fatalf("same digest must hit, got ok=%v err=%v", ok, err)
	}
	if ok, err := HasImportedSHA(dir, `other`); err != nil || ok {
		t.Fatalf("different digest must miss, got ok=%v err=%v", ok, err)
	}
	if ok, err := HasImportedSHA(dir, ``); err != nil || ok {
		t.Fatalf("empty digest must never match (legacy records), got ok=%v err=%v", ok, err)
	}
	// corrupt line skipped, valid line still found
	ledger := filepath.Join(dir, `imports.jsonl`)
	f, err := os.OpenFile(ledger, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{not json` + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if ok, err := HasImportedSHA(dir, `abc123`); err != nil || !ok {
		t.Fatalf("corrupt lines must be skipped, got ok=%v err=%v", ok, err)
	}
}
