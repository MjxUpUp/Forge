package change

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/shellexec"
	"github.com/MjxUpUp/Forge/internal/specroot"
)

// 单条验收命令的默认超时。整包测试在慢机器上要几分钟，给足余量但必须有上限：
// 没有上限的验证会在 CI 里挂死，比失败更糟。
const DefaultCheckTimeout = 10 * time.Minute

// Check 是一条验收命令的实跑记录（证据目录里另存 stdout/stderr 原文）。
type Check struct {
	Index int `json:"index"`
	// Shell 记录这条判据实际用的壳（bash/sh）。证据要能回答"它到底在哪个
	// 解释器里跑过"，否则 per-fence 选壳只是实现细节，审计时看不出来。
	Shell      string `json:"shell,omitempty"`
	Command    string `json:"command"`
	Exit       int    `json:"exit"`
	DurationMS int64  `json:"duration_ms"`
	TimedOut   bool   `json:"timed_out,omitempty"`
	Skipped    bool   `json:"skipped,omitempty"`
	// StdoutFile/StderrFile 是相对证据目录的文件名（空表示没有输出）。
	StdoutFile string `json:"stdout_file,omitempty"`
	StderrFile string `json:"stderr_file,omitempty"`
	// WriteError 非空表示证据落盘失败（磁盘满/权限）。此时这条不算通过：
	// 证据写不下去的"通过"是不可复核的通过。
	WriteError string `json:"write_error,omitempty"`
}

// Command 是一条可执行判据：用哪种壳跑，以及围栏里的正文。
//
// 壳按围栏语言选（```bash → bash，```sh/```shell → sh）：文档里写着"按各自
// 解释器执行"，实现就不能只挑一个壳来跑全部围栏。
type Command struct {
	Shell string
	Body  string
}

// Acceptance 是 acceptance.md 解析出来的两类判据：
// 可执行命令（```bash 围栏）与人工检查项（- [ ] 清单）。
type Acceptance struct {
	Commands []Command
	Manual   []string
	// ManualDone 是已勾选（- [x]）的人工项。勾选不代表机器验过，同样不进封印，
	// 只是别让人以为自己写下的确认从报告里消失了。
	ManualDone []string
}

// ParseAcceptance 把 acceptance.md 拆成命令与人工检查项。
//
// 只认 ```bash / ```sh / ```shell 围栏：别的语言块是示意，不是判据。
// 人工检查项在验收时只列出、不阻断——但也不会被算作"已验证"。
func ParseAcceptance(raw []byte) Acceptance {
	var acc Acceptance
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	lines := strings.Split(text, "\n")

	inFence := false
	inHTMLComment := false
	lang := ""
	indent := 0
	var body []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// HTML 注释里的示例围栏不是判据：那是文档里"举例说明围栏长什么样"的
		// 唯一可靠写法。缩进围栏则相反——Markdown 列表项里的判据天生缩进四格，
		// 早先按"缩进=示例"跳过，会让真判据被静默丢掉（review 实测只跑 1 条
		// 却报全部通过），所以这里只去缩进、不再跳过。
		if !inFence {
			if inHTMLComment {
				if strings.Contains(line, "-->") {
					inHTMLComment = false
				}
				continue
			}
			if i := strings.Index(line, "<!--"); i >= 0 {
				if !strings.Contains(line[i:], "-->") {
					inHTMLComment = true
					continue
				}
				continue
			}
		}
		if strings.HasPrefix(trimmed, "```") {
			if !inFence {
				inFence = true
				indent = len(line) - len(strings.TrimLeft(line, " "))
				lang = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(trimmed, "```")), " ")))
				if i := strings.IndexAny(lang, " \t"); i >= 0 {
					lang = lang[:i]
				}
				body = body[:0]
				continue
			}
			inFence = false
			if isShellFence(lang) {
				if cmd := strings.TrimSpace(strings.Join(body, "\n")); cmd != "" {
					shell := "sh"
					if lang == "bash" {
						shell = "bash"
					}
					acc.Commands = append(acc.Commands, Command{Shell: shell, Body: cmd})
				}
			}
			body = nil
			continue
		}
		if inFence {
			body = append(body, unindent(line, indent))
			continue
		}
		if item, done, ok := manualItem(trimmed); ok {
			if done {
				acc.ManualDone = append(acc.ManualDone, item)
			} else {
				acc.Manual = append(acc.Manual, item)
			}
		}
	}
	return acc
}

// unindent 去掉围栏所在层级的缩进：列表项里的围栏整体缩进四格，正文跟着缩，
// 不去掉就会把 "    go test ./..." 原样交给 shell（前导空格不影响执行，但证据里
// 的命令长得不对、diff 也不干净）。
func unindent(line string, n int) string {
	for i := 0; i < n && strings.HasPrefix(line, " "); i++ {
		line = line[1:]
	}
	return line
}

func isShellFence(lang string) bool {
	switch lang {
	case "bash", "sh", "shell":
		return true
	}
	return false
}

// manualItem 解析一行人工检查项，返回文本、是否已勾选、是否命中。
// 支持 "- [ ] x" / "-   [x]   x"，也接受 "* [x] x"。
func manualItem(trimmed string) (text string, done bool, ok bool) {
	if !strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(trimmed, "*") {
		return "", false, false
	}
	rest := strings.TrimSpace(trimmed[1:])
	switch {
	case strings.HasPrefix(rest, "[ ]"):
		return strings.TrimSpace(strings.TrimPrefix(rest, "[ ]")), false, true
	case strings.HasPrefix(strings.ToLower(rest), "[x]"):
		return strings.TrimSpace(rest[3:]), true, true
	}
	return "", false, false
}

// Result 是一次 `forge change verify` 的完整结果（也是命令行输出与
// evidence/<ts>/summary.json 的共同来源）。
type Result struct {
	Summary     Summary
	EvidenceDir string
	// Manual 是 acceptance.md 里的人工检查项：只展示、不阻断，也不进封印。
	Manual []string
	// ManualDone 是已勾选的人工项，同样不进封印，只是别让它从报告里消失。
	ManualDone []string
}

// VerifyOptions 控制一次验证。
type VerifyOptions struct {
	// Timeout 是单条命令的超时，<=0 用 DefaultCheckTimeout。
	Timeout time.Duration
	// Now 可注入时间（测试用），零值取 time.Now。
	Now time.Time
}

// Verify 实跑 change 的 acceptance.md 并落一份证据。
//
// 行为约定：
//   - 命令按文档顺序执行，首个非零即停，后面的标记 skipped；
//   - 全部命令退出 0 且至少有一条命令时才 sealed=true——没有可执行判据的
//     change 不能凭"我是对的"通过；
//   - 只要有命令失败，证据照样落盘（失败证据比没有证据更有用）。
func Verify(ctx context.Context, root, id string, opts VerifyOptions) (*Result, error) {
	// id 进路径之前必须校验：`../sibling` 之类会把证据写到别人的目录里。
	// CLI 已经挡了一层，但 Verify 是公开 API，测试和后续调用方都会直接用它。
	if err := ValidID(id); err != nil {
		return nil, err
	}
	// 这里不取指纹：验收命令可能自己产生文件，指纹要等它们跑完再算，
	// 否则 verify 刚打印"已封印"，list 立刻判它 stale（出场即失效）。
	c, err := load(root, id, nil)
	if err != nil {
		return nil, err
	}
	// 模板骨架不是判据：init+new 之后四份文件还是模板，直接 verify 会把
	// 通用示例命令当成这个 change 的验收标准（在 Go 仓库里甚至能封印）。
	if problems := docProblemsFor(c.Dir, c.Fast); len(problems) > 0 {
		return nil, fmt.Errorf("change: %s 的产物还不完整（%s）——四份产物都填成这个 change 自己的内容、并删掉 %s 标记后再 verify",
			id, strings.Join(problems, "、"), scaffoldMarker)
	}
	if !c.HasAcceptance {
		return nil, fmt.Errorf("change: %s 没有 %s（无法验证）", relTo(root, c.Dir), FileAcceptance)
	}
	accPath := filepath.Join(c.Dir, FileAcceptance)
	raw, err := os.ReadFile(accPath)
	if err != nil {
		return nil, fmt.Errorf("change: 读取 %s: %w", relTo(root, accPath), err)
	}
	acc := ParseAcceptance(raw)
	if len(acc.Commands) == 0 {
		return nil, fmt.Errorf("change: %s 里没有 ```bash 命令——人工清单不算可执行判据", relTo(root, accPath))
	}
	for _, cmd := range acc.Commands {
		if !hasExecutableLine(cmd.Body) {
			return nil, fmt.Errorf("change: %s 里有一条围栏只有注释/空行——注释不是判据", relTo(root, accPath))
		}
		if err := lintJudge(cmd.Body); err != nil {
			return nil, fmt.Errorf("change: %s: %w", relTo(root, accPath), err)
		}
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultCheckTimeout
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	evDir, err := uniqueEvidenceDir(filepath.Join(c.Dir, EvidenceDirName), now.UTC().Format("20060102T150405Z"))
	if err != nil {
		return nil, err
	}

	plan := make([]plannedCheck, 0, len(acc.Commands))
	for i, cmd := range acc.Commands {
		plan = append(plan, plannedCheck{Index: i + 1, Cmd: cmd})
	}
	checks, passed, err := runPlan(ctx, root, evDir, plan, timeout)
	if err != nil {
		return nil, err
	}
	sum := Summary{
		Change:           id,
		StartedAt:        now.UTC().Format(time.RFC3339),
		AcceptanceSHA256: hashBytes(raw),
		Checks:           checks,
		Fast:             c.Fast,
	}

	// 命令跑完再取指纹：证据描述的是"这些命令在这个工作区上通过了"，
	// 命令自身的副作用（生成的临时文件）也属于这个工作区。
	sum.Passed = passed
	finishSummary(root, &sum)
	// 只有"全部实跑命令通过"才封印。人工检查项留给 `change verify` 的输出
	// 末尾列出，由人自己确认——它们不进入封印，也就不构成假绿。
	sum.Sealed = passed
	sum.FinishedAt = time.Now().UTC().Format(time.RFC3339)

	if err := writeSummary(evDir, sum); err != nil {
		return nil, err
	}
	return &Result{Summary: sum, EvidenceDir: evDir, Manual: acc.Manual, ManualDone: acc.ManualDone}, nil
}

// 单条命令的输出最多保留 2 MiB：噪声命令（构建日志、测试刷屏）不能把 verify
// 自己撑爆；截断标记会写进证据文件，避免"看起来完整"的假象。
const maxCaptureBytes = 2 << 20

type cappedBuffer struct {
	buf       bytes.Buffer
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := maxCaptureBytes - c.buf.Len(); room > 0 {
		if len(p) <= room {
			c.buf.Write(p)
		} else {
			c.buf.Write(p[:room])
			c.truncated = true
		}
	} else if len(p) > 0 {
		c.truncated = true
	}
	return len(p), nil
}

// runCheck 执行一条判据并落证据。证据写不下去时把 check.Exit 记成 -1：
// 一次无法留下证据的"通过"不可复核，不能算通过。
func runCheck(ctx context.Context, root, shell, evDir string, check Check, timeout time.Duration) Check {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 以 set -e 包一层再执行：围栏里允许多行（`cd x` 换行 `make test`），
	// 但中间一行失败不能因为最后一行成功而被吞掉——否则会静默假绿。
	// bash 再加 pipefail：`go test ./... | tail` 这种写法在 set -e 下仍会
	// 以 tail 的退出码为准，不设 pipefail 就等于允许 agent 把失败管道掉。
	prefix := "set -e\n"
	if base := strings.ToLower(filepath.Base(shell)); base == "bash" || base == "bash.exe" {
		prefix = "set -eo pipefail\n"
	}
	cmd := exec.CommandContext(cctx, shell, "-c", prefix+check.Command)
	cmd.Dir = root
	var stdout, stderr cappedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	runErr := cmd.Run()
	check.DurationMS = time.Since(start).Milliseconds()
	check.TimedOut = errors.Is(cctx.Err(), context.DeadlineExceeded)
	check.Exit = exitCode(runErr)

	base := fmt.Sprintf("%02d", check.Index)
	if werr := writeCapture(filepath.Join(evDir, base+".stdout"), stdout); werr != nil {
		check.WriteError = werr.Error()
	} else if stdout.buf.Len() > 0 {
		check.StdoutFile = base + ".stdout"
	}
	if werr := writeCapture(filepath.Join(evDir, base+".stderr"), stderr); werr != nil {
		check.WriteError = werr.Error()
	} else if stderr.buf.Len() > 0 {
		check.StderrFile = base + ".stderr"
	}
	if check.WriteError != "" {
		check.Exit = -1
	}
	return check
}

func writeCapture(path string, buf cappedBuffer) error {
	if buf.buf.Len() == 0 && !buf.truncated {
		return nil
	}
	data := append([]byte{}, buf.buf.Bytes()...)
	if buf.truncated {
		data = append(data, []byte("\n[forge: output truncated at 2 MiB]\n")...)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("change: 写证据 %s: %w", filepath.Base(path), err)
	}
	return nil
}

// exitCode 把子进程结果归一成退出码：没有退出码（被信号杀、启动失败）记 -1，
// 不把"没跑起来"混同于"跑失败了"。
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

// shellFor 按围栏声明的壳返回可执行路径。
//
// 不把两种壳混着找：写了 ```sh 的判据就该在 sh 里跑，拿 bash 顶替会让
// "在 sh 下会失败"的判据静默通过（Bash 语法、[[ ]]、pipefail 都不一样）。
func shellFor(kind string) (string, string, error) {
	// 走 shellexec 而不是裸 exec.LookPath：Windows 上 PATH 里的 bash 可能是
	// WSL 启动器（C:\Windows\System32\bash.exe），判据会在 WSL 里跑——路径、工具链
	// 全错，却可能"成功"。见 internal/shellexec/bash.go 的解析顺序。
	if kind == "bash" {
		if p, err := shellexec.FindBash(); err == nil {
			return p, "bash", nil
		}
		return "", "", errors.New("change: acceptance 里有 ```bash 围栏，但本机没有可用的 bash（Windows 请安装 Git for Windows 并确保 bash 在 PATH）")
	}
	if p, err := exec.LookPath("sh"); err == nil {
		return p, "sh", nil
	}
	// Windows 的 Git 自带 bash 却通常不带 PATH 里的 sh。与其让 ```sh 围栏恒报
	// "没有 sh"，不如回落 bash 并把实际用的壳记进证据——判据必须跑得起来，
	// 而证据里写清楚"这条在 bash 下跑的"比假装它是 sh 诚实。
	if p, err := shellexec.FindBash(); err == nil {
		return p, "bash", nil
	}
	return "", "", errors.New("change: acceptance 里有 ```sh 围栏，但本机既没有 sh 也没有 bash；把判据改成 ```bash 围栏，或安装一个 sh")
}

// uniqueEvidenceDir 保证每次验证都有独立目录：同一秒内连跑两次是常态
// （失败→修→重跑），共用目录会把上一次的证据悄悄覆盖掉。
func uniqueEvidenceDir(base, stamp string) (string, error) {
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", fmt.Errorf("change: 创建证据目录: %w", err)
	}
	for i := 0; ; i++ {
		name := stamp
		if i > 0 {
			name = fmt.Sprintf("%s-%d", stamp, i+1)
		}
		dir := filepath.Join(base, name)
		err := os.Mkdir(dir, 0o755)
		if err == nil {
			return dir, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("change: 创建证据目录: %w", err)
		}
	}
}

func writeSummary(dir string, sum Summary) error {
	raw, err := json.MarshalIndent(sum, "", "  ")
	if err != nil {
		return fmt.Errorf("change: 序列化摘要: %w", err)
	}
	path := filepath.Join(dir, SummaryFile)
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		return fmt.Errorf("change: 写 %s: %w", relTo(dir, path), err)
	}
	return nil
}

// Fingerprint 是"当前工作区内容"的指纹：HEAD + 未提交改动 + 未跟踪文件内容。
//
// 用它而不是单看 HEAD：同一次验证里 agent 可能还没提交，工作区才是被验证的
// 真实对象。提交之后 HEAD 变了，但改动已经落地——所以判 stale 时只在 HEAD
// 相同的情况下比较内容指纹（见 List）。
type Fingerprint struct {
	Head       string
	DiffSHA256 string
	// Files 是每个改动文件的内容摘要，用于判断证据是否还代表当前代码。
	Files []FileDigest
	OK    bool
}

// FileDigest 是一个被改动文件（改动中或未跟踪）的路径与内容摘要。
type FileDigest struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// WorktreeFingerprint 计算仓库内容指纹；非 git 仓库或 git 不可用时 OK=false，
// 此时证据不绑定 diff（summary 里字段留空），调用方要照实说明。
func WorktreeFingerprint(root string) Fingerprint {
	head, err := gitOutput(root, "rev-parse", "HEAD")
	if err != nil {
		return Fingerprint{}
	}
	files, ok := changedFiles(root)
	if !ok {
		return Fingerprint{}
	}
	return Fingerprint{
		Head:       strings.TrimSpace(head),
		DiffSHA256: hashFiles(files),
		Files:      files,
		OK:         true,
	}
}

// changedFiles 列出相对 HEAD 有改动（含未跟踪）的文件，并逐个算内容摘要。
// 未跟踪文件也进列表：新写的文件同样是"被验证的代码"。
func changedFiles(root string) ([]FileDigest, bool) {
	diff, err := gitOutput(root, "diff", "HEAD", "--name-only", "-z")
	if err != nil {
		return nil, false
	}
	untracked, err := gitOutput(root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, false
	}
	seen := make(map[string]bool)
	var out []FileDigest
	for _, p := range splitNul(diff + untracked) {
		if p == "" || isEvidencePath(p) || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, FileDigest{Path: p, SHA256: fileSHA256(root, p)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, true
}

func splitNul(s string) []string {
	var out []string
	for _, p := range strings.Split(s, "\x00") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// fileSHA256 给单个文件算摘要；读不到时返回标记值而不是空串——
// 空串会让"文件被删掉"与"没记录"混为一谈，漂移就查不出来。
func fileSHA256(root, rel string) string {
	f, err := os.Open(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "<missing>"
		}
		return "<unreadable>"
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "<read-error>"
	}
	return hex.EncodeToString(h.Sum(nil))
}

func hashFiles(files []FileDigest) string {
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.Path))
		h.Write([]byte{0})
		h.Write([]byte(f.SHA256))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// sameDigests 比较两组文件摘要是否完全一致（路径与内容都要对上）。
func sameDigests(a, b []FileDigest) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Path != b[i].Path || a[i].SHA256 != b[i].SHA256 {
			return false
		}
	}
	return true
}

// filesDrifted 判断封印时记录的文件内容是否还与磁盘一致。
func filesDrifted(root string, files []FileDigest) bool {
	for _, f := range files {
		if fileSHA256(root, f.Path) != f.SHA256 {
			return true
		}
	}
	return false
}

// scaffoldMarker 标记尚未填写的模板骨架。填完删掉标记，verify 才会执行。
const scaffoldMarker = "forge:fill"

// docProblemsFor 列出让这个 change 还不算填完的原因：缺文件，或者仍是模板骨架。
// 普通 change 要四份产物，少一份就少了对应的审计线索；fast 通道只要求
// "为什么做"和"怎么算完"——省的是文档，不是判据与证据。
func docProblemsFor(dir string, fast bool) []string {
	required := []string{FileProposal, FileDesign, FileTasks, FileAcceptance}
	if fast {
		required = []string{FileProposal, FileAcceptance}
	}
	var out []string
	for _, name := range required {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			out = append(out, name+"（缺失）")
			continue
		}
		if strings.Contains(string(raw), scaffoldMarker) {
			out = append(out, name+"（仍是模板骨架）")
		}
	}
	return out
}

// hasExecutableLine 判断一段围栏里有没有真正会被执行的语句。
// 只有注释和空行的围栏跑起来必然退出 0，不能算判据。
//
// 注释不会因为行尾的反斜杠续到下一行（实测 `# 说明 \` 之后的 `false` 照跑），
// 所以按行判断即可——早先按续行跳过会把真语句当成注释。
func hasExecutableLine(cmd string) bool {
	for _, line := range strings.Split(cmd, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		return true
	}
	return false
}

// hasSingleAmpersand 判断有没有单个 `&`（放行 `&&`、`>&`、`&>`）。
func hasSingleAmpersand(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '&' {
			continue
		}
		var prev, next byte
		if i > 0 {
			prev = s[i-1]
		}
		if i+1 < len(s) {
			next = s[i+1]
		}
		if next == '&' {
			i++
			continue
		}
		if prev == '>' || next == '>' {
			continue
		}
		return true
	}
	return false
}

// isEvidencePath 判断未跟踪文件是否落在 spec/changes/<id>/evidence/ 下。
//
// 证据是验证的产物，不是被验证的内容：它由 verify 自己在算完指纹之后写入，
// 如果算进指纹，每跑一次 verify 都会把自己的上一次证据判成 stale。
func isEvidencePath(p string) bool {
	parts := strings.Split(filepath.ToSlash(p), "/")
	if len(parts) < 4 || parts[0] != specroot.DirName || parts[1] != DirName {
		return false
	}
	// 归档后的路径是 spec/changes/archive/<YYYYMMDD>-<id>/evidence/...，
	// 不只第 4 段叫 evidence——任何一段都认。
	for _, part := range parts[3:] {
		if part == EvidenceDirName {
			return true
		}
	}
	return false
}

func gitOutput(root string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}

// plannedCheck 是一条要执行的判据。Index 保留它在 acceptance.md 里的围栏
// 序号：节点 gate 只跑子集，但输出里的 #N 必须还能对回文档。
type plannedCheck struct {
	Index int
	Cmd   Command
}

// runPlan 按顺序执行一组判据，首个非零即停，其余标记 skipped。
func runPlan(ctx context.Context, root, evDir string, plan []plannedCheck, timeout time.Duration) ([]Check, bool, error) {
	checks := make([]Check, 0, len(plan))
	passed := true
	for _, p := range plan {
		check := Check{Index: p.Index, Shell: p.Cmd.Shell, Command: p.Cmd.Body}
		if !passed {
			check.Skipped = true
			checks = append(checks, check)
			continue
		}
		shell, shellName, err := shellFor(p.Cmd.Shell)
		if err != nil {
			return nil, false, err
		}
		check.Shell = shellName
		check = runCheck(ctx, root, shell, evDir, check, timeout)
		if check.Exit != 0 {
			passed = false
		}
		checks = append(checks, check)
	}
	return checks, passed, nil
}

// finishSummary 在命令跑完之后补齐指纹与结束时间。
//
// 必须放在命令之后：判据自己可能写文件，先取指纹会让证据一出场就失效。
func finishSummary(root string, sum *Summary) {
	fp := WorktreeFingerprint(root)
	sum.DiffBound = fp.OK
	if fp.OK {
		sum.Head = fp.Head
		sum.DiffSHA256 = fp.DiffSHA256
		sum.Files = fp.Files
	}
	sum.FinishedAt = time.Now().UTC().Format(time.RFC3339)
}
