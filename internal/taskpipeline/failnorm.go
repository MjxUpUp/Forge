package taskpipeline

// failnorm.go —— 验收失败输出的规范化与指纹（loop-contract B1-1）。
// 同一个失败在两次实跑之间会带不同的耗时、时间戳、地址、goroutine 号和临时目录，
// 不去噪就算不出稳定指纹；指纹必须在【完整】输出上算，再取尾部若干行——
// AcceptanceCriterion.Output 是按字节截取的最后 500 字节，噪音长度一变、截取
// 起点就挪一个字节，截断后再去噪救不回来。指纹本体复用 LoopFingerprint
// （loopedge.go），不另写一份。
//
// failnorm.go — normalization and fingerprint of acceptance failure output
// (loop-contract B1-1). The same failure carries different durations,
// timestamps, addresses, goroutine ids and temp dirs between two runs;
// without stripping them no stable fingerprint exists. The fingerprint is
// computed on the FULL output and only then narrowed to the tail lines: the
// persisted Output is a byte-sliced 500-byte tail whose start shifts with
// noise length, so normalizing after truncation cannot recover it. The
// fingerprint itself reuses LoopFingerprint (loopedge.go).

import (
	"os"
	"regexp"
	"strings"
)

// failureTailLines is how many trailing non-empty lines feed the fingerprint.
//
// failureTailLines 是参与指纹的尾部非空行数。
const failureTailLines = 20

var (
	// 耗时：0.12s / 123ms / 1.5µs（go test 的 "ok pkg 0.12s"、jest 的 "(12 ms)"、pytest 的 "in 0.34s"）。
	failNoiseDuration = regexp.MustCompile(`\b\d+(?:\.\d+)?\s*(?:ms|µs|us|ns|s)\b`)
	// RFC3339 / ISO 时间戳与 HH:MM:SS（可带毫秒）。
	failNoiseTimestamp = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?(?:Z|[+-]\d{2}:?\d{2})?\b|\b\d{2}:\d{2}:\d{2}(?:[.,]\d+)?\b`)
	// 十六进制地址与指针。
	failNoiseHexAddr = regexp.MustCompile(`\b0x[0-9a-fA-F]+\b`)
	// goroutine 编号（Go panic / race 报告）。
	failNoiseGoroutine = regexp.MustCompile(`\bgoroutine \d+\b`)
	// 随机临时目录段：/tmp/xxx123、TestFoo123456789/001、TMPDIR 下的路径。
	failNoiseTempSeg = regexp.MustCompile(`/(?:tmp|var/folders)/[^\s:]+|\b[A-Za-z]+\d{6,}\b`)
	// $TMPDIR 之下的整段路径（jest-9981/run 这类随机子目录随 TMPDIR 一起折叠）。
	failNoiseTmpdirTail = regexp.MustCompile(`<TMPDIR>/[^\s:]+`)
)

// NormalizeFailureOutput strips run-to-run noise from a command's output so two
// runs of the same failure normalize to the same text: durations, timestamps,
// hex addresses, goroutine ids, temp-dir segments and $TMPDIR itself. Line
// structure is preserved (one line in, one line out); CRLF folds to LF.
//
// NormalizeFailureOutput 去掉命令输出里每次实跑都会变的噪音，使同一失败的两次
// 输出规范化成同一文本：耗时、时间戳、十六进制地址、goroutine 号、临时目录段与
// $TMPDIR 本身。保留行结构（一行进一行出）；CRLF 折成 LF。
func NormalizeFailureOutput(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if tmp := strings.TrimRight(os.Getenv("TMPDIR"), "/"); tmp != "" {
		s = strings.ReplaceAll(s, tmp, "<TMPDIR>")
		s = failNoiseTmpdirTail.ReplaceAllString(s, "<TMPDIR>")
	}
	s = failNoiseTimestamp.ReplaceAllString(s, "<TS>")
	s = failNoiseDuration.ReplaceAllString(s, "<DUR>")
	s = failNoiseHexAddr.ReplaceAllString(s, "<ADDR>")
	s = failNoiseGoroutine.ReplaceAllString(s, "goroutine <N>")
	s = failNoiseTempSeg.ReplaceAllString(s, "<TMP>")
	return s
}

// FailureTail returns the last failureTailLines non-empty lines of the
// normalized output, joined by "\n" — the text the fingerprint is taken over.
//
// FailureTail 返回规范化输出的最后 failureTailLines 个非空行（"\n" 连接）——
// 指纹就取在这段文本上。
func FailureTail(normalized string) string {
	lines := strings.Split(normalized, "\n")
	var keep []string
	for i := len(lines) - 1; i >= 0 && len(keep) < failureTailLines; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			keep = append(keep, l)
		}
	}
	for i, j := 0, len(keep)-1; i < j; i, j = i+1, j-1 {
		keep[i], keep[j] = keep[j], keep[i]
	}
	return strings.Join(keep, "\n")
}

// FailureFingerprint is the stable fingerprint of a failed run's FULL output:
// NormalizeFailureOutput → FailureTail → LoopFingerprint. Empty output yields
// the fingerprint of the empty string (still comparable).
//
// FailureFingerprint 是一次失败实跑【完整】输出的稳定指纹：NormalizeFailureOutput
// → FailureTail → LoopFingerprint。空输出得到空串的指纹（仍可比较）。
func FailureFingerprint(fullOutput string) string {
	return LoopFingerprint(FailureTail(NormalizeFailureOutput(fullOutput)))
}
