package projectsync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// ledger.go — the machine-local import ledger (DataDir/imports.jsonl): one line per
// import, keyed by bundle_id. Re-importing the same bundle is skipped (the merge is
// idempotent anyway; the ledger makes it FREE and makes the intent explicit). The
// ledger deliberately does NOT travel in bundles: syncing it would leak every
// machine's hostname/user to every peer and would need its own dedup semantics.
//
// ledger.go —— 机器本地导入账本（DataDir/imports.jsonl）：每次导入一行，以
// bundle_id 为键。重复导入同一 bundle 被跳过（合并本身幂等；账本让这件事免费且
// 意图显式）。账本刻意不随 bundle 旅行：同步它会向每个对端泄露每台机器的
// hostname/user，且它自身需要另一套去重语义。

// ImportRecord is one ledger line.
//
// ImportRecord 是一条账本行。
type ImportRecord struct {
	BundleID   string    `json:"bundle_id"`
	SHA256     string    `json:"sha256"` // whole-bundle-file hash at import time
	ImportedAt time.Time `json:"imported_at"`
	FromKey    string    `json:"from_key,omitempty"`
	ToKey      string    `json:"to_key,omitempty"`
	Counts     string    `json:"counts,omitempty"` // human-readable action summary
}

// ledgerPath returns DataDir/imports.jsonl.
//
// ledgerPath 返回 DataDir/imports.jsonl。
func ledgerPath(dataDir string) string {
	return filepath.Join(dataDir, `imports.jsonl`)
}

// AppendImportRecord appends one record to the machine-local ledger.
//
// AppendImportRecord 向机器本地账本追加一条记录。
func AppendImportRecord(dataDir string, rec ImportRecord) error {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return err
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(ledgerPath(dataDir), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, werr := f.Write(append(line, '\n'))
	return werr
}

// HasImportedBundle reports whether this machine already imported the given bundle
// id. Corrupt lines are skipped (a damaged ledger must never block imports), so a
// read error only surfaces when the file is unreadable.
//
// HasImportedBundle 报告本机是否已导入过给定 bundle id。坏行跳过（损坏的账本
// 绝不阻塞导入），只有文件不可读才报错。
func HasImportedBundle(dataDir, bundleID string) (bool, error) {
	data, err := os.ReadFile(ledgerPath(dataDir))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	for _, line := range splitLines(data) {
		var rec ImportRecord
		if json.Unmarshal(line, &rec) != nil {
			continue // 坏行跳过
		}
		if rec.BundleID == bundleID {
			return true, nil
		}
	}
	return false, nil
}

// splitLines splits JSONL bytes into non-empty lines (CRLF tolerant).
//
// splitLines 把 JSONL 字节切成非空行（容忍 CRLF）。
func splitLines(data []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			line := trimCR(data[start:i])
			if len(line) > 0 {
				out = append(out, line)
			}
			start = i + 1
		}
	}
	if tail := trimCR(data[start:]); len(tail) > 0 {
		out = append(out, tail)
	}
	return out
}

func trimCR(b []byte) []byte {
	for len(b) > 0 && b[len(b)-1] == '\r' {
		b = b[:len(b)-1]
	}
	return b
}
