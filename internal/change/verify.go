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
	Index      int    `json:"index"`
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

// Acceptance 是 acceptance.md 解析出来的两类判据：
// 可执行命令（```bash 围栏）与人工检查项（- [ ] 清单）。
type Acceptance struct {
	Commands []string
	Manual   []string
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
	lang := ""
	var body []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if !inFence {
				inFence = true
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
					acc.Commands = append(acc.Commands, cmd)
				}
			}
			body = nil
			continue
		}
		if inFence {
			body = append(body, line)
			continue
		}
		if item, ok := manualItem(trimmed); ok {
			acc.Manual = append(acc.Manual, item)
		}
	}
	return acc
}

func isShellFence(lang string) bool {
	switch lang {
	case "bash", "sh", "shell":
		return true
	}
	return false
}

func manualItem(trimmed string) (string, bool) {
	// 支持 "- [ ] x" / "-   [ ]   x"，不认已勾选的 "- [x]"。
	if !strings.HasPrefix(trimmed, "-") {
		return "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
	if !strings.HasPrefix(rest, "[ ]") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(rest, "[ ]")), true
}

// Result 是一次 `forge change verify` 的完整结果（也是命令行输出与
// evidence/<ts>/summary.json 的共同来源）。
type Result struct {
	Summary     Summary
	EvidenceDir string
	// Manual 是 acceptance.md 里的人工检查项：只展示、不阻断，也不进封印。
	Manual []string
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
	c, err := Load(root, id)
	if err != nil {
		return nil, err
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

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultCheckTimeout
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	shell, err := findShell()
	if err != nil {
		return nil, err
	}

	evDir, err := uniqueEvidenceDir(filepath.Join(c.Dir, EvidenceDirName), now.UTC().Format("20060102T150405Z"))
	if err != nil {
		return nil, err
	}

	sum := Summary{
		Change:           id,
		StartedAt:        now.UTC().Format(time.RFC3339),
		AcceptanceSHA256: hashBytes(raw),
	}
	fp := WorktreeFingerprint(root)
	sum.DiffBound = fp.OK
	if fp.OK {
		sum.Head = fp.Head
		sum.DiffSHA256 = fp.DiffSHA256
	}

	passed := true
	for i, cmd := range acc.Commands {
		check := Check{Index: i + 1, Command: cmd}
		if !passed {
			check.Skipped = true
			sum.Checks = append(sum.Checks, check)
			continue
		}
		check = runCheck(ctx, root, shell, evDir, check, timeout)
		if check.Exit != 0 {
			passed = false
		}
		sum.Checks = append(sum.Checks, check)
	}

	sum.Passed = passed
	// 只有"全部实跑命令通过"才封印。人工检查项留给 `change verify` 的输出
	// 末尾列出，由人自己确认——它们不进入封印，也就不构成假绿。
	sum.Sealed = passed
	sum.FinishedAt = time.Now().UTC().Format(time.RFC3339)

	if err := writeSummary(evDir, sum); err != nil {
		return nil, err
	}
	return &Result{Summary: sum, EvidenceDir: evDir, Manual: acc.Manual}, nil
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
	cmd := exec.CommandContext(cctx, shell, "-c", "set -e\n"+check.Command)
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

// findShell 找 acceptance 的执行壳：优先 bash（与 forge hook 的执行环境一致），
// 再退 sh。Windows 上没有 bash 时给一条能照做的错误，而不是含糊的 exec 错误。
func findShell() (string, error) {
	// 走 shellexec 而不是裸 exec.LookPath：Windows 上 PATH 里的 bash 可能是
	// WSL 启动器（C:\Windows\System32\bash.exe），判据会在 WSL 里跑——路径、工具链
	// 全错，却可能"成功"。见 internal/shellexec/bash.go 的解析顺序。
	if p, err := shellexec.FindBash(); err == nil {
		return p, nil
	}
	if p, err := exec.LookPath("sh"); err == nil {
		return p, nil
	}
	return "", errors.New("change: 找不到 bash/sh，acceptance 命令无法执行（Windows 请安装 Git for Windows 并确保 bash 在 PATH）")
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
	OK         bool
}

// WorktreeFingerprint 计算仓库内容指纹；非 git 仓库或 git 不可用时 OK=false，
// 此时证据不绑定 diff（summary 里字段留空），调用方要照实说明。
func WorktreeFingerprint(root string) Fingerprint {
	head, err := gitOutput(root, "rev-parse", "HEAD")
	if err != nil {
		return Fingerprint{}
	}
	diff, err := gitOutput(root, "diff", "HEAD", "--binary")
	if err != nil {
		return Fingerprint{}
	}
	untracked, err := gitOutput(root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return Fingerprint{}
	}

	h := sha256.New()
	h.Write([]byte(diff))
	h.Write([]byte{0})
	paths := strings.Split(strings.TrimSuffix(untracked, "\x00"), "\x00")
	sort.Strings(paths)
	for _, p := range paths {
		if p == "" || isEvidencePath(p) {
			continue
		}
		h.Write([]byte(p))
		h.Write([]byte{0})
		full := filepath.Join(root, filepath.FromSlash(p))
		if _, ierr := os.Stat(full); ierr != nil {
			h.Write([]byte("<missing>"))
			continue
		}
		f, oerr := os.Open(full)
		if oerr != nil {
			h.Write([]byte("<unreadable>"))
			h.Write([]byte{0})
			continue
		}
		if _, cerr := io.Copy(h, f); cerr != nil {
			h.Write([]byte("<read-error>"))
		}
		f.Close()
		h.Write([]byte{0})
	}
	return Fingerprint{Head: strings.TrimSpace(head), DiffSHA256: hex.EncodeToString(h.Sum(nil)), OK: true}
}

// isEvidencePath 判断未跟踪文件是否落在 spec/changes/<id>/evidence/ 下。
//
// 证据是验证的产物，不是被验证的内容：它由 verify 自己在算完指纹之后写入，
// 如果算进指纹，每跑一次 verify 都会把自己的上一次证据判成 stale。
func isEvidencePath(p string) bool {
	parts := strings.Split(filepath.ToSlash(p), "/")
	return len(parts) >= 4 && parts[0] == specroot.DirName &&
		parts[1] == DirName && parts[3] == EvidenceDirName
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
