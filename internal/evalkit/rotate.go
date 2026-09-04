package evalkit

// rotate.go — golden 私有子集与轮换（Track B · P3，docs/design/
// forge-evaluation-system.md §六 P3）：私有标注子集永不进 VCS（0600 用户级），
// 季度轮换 1/3；轮换前对存续用例做 oracle 复验（门禁演化会使旧用例失效——
// 不可复验的用例淘汰）。
//
// rotate.go — golden private subset & rotation: the private labeled subset
// never enters VCS (0600 user-level), rotates 1/3 quarterly; before rotation,
// surviving cases get oracle revalidation (gate evolution invalidates old
// cases — non-revalidatable cases retire).

import (
	"runtime"

	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// PrivateGoldenDirName is the user-level private golden directory (under
// <evalDir>/forge/).
//
// PrivateGoldenDirName 是用户级私有 golden 目录（<evalDir>/forge/ 下）。
const PrivateGoldenDirName = "golden-private"

// PrivateGoldenDir resolves the private golden directory.
//
// PrivateGoldenDir 解析私有 golden 目录。
func PrivateGoldenDir(evalDir string) string {
	return filepath.Join(evalDataDir(evalDir), PrivateGoldenDirName)
}

// CheckPrivatePermissions enforces 0700/0600 on the private dir and its case
// files (POSIX). Mode mismatch is a hard rejection — a leaky private set
// poisons both the anti-overfitting signal and any public claim built on it.
// Windows has no POSIX permission bits (modes read back as 0777) — the check
// degrades to existence-only there; ACL hardening is the Windows-side
// follow-up, disclosed in gates-card blind spots.
//
// CheckPrivatePermissions 强制私有目录与用例文件 0700/0600（POSIX）。权限不符即
// 硬拒绝——泄漏的私有集同时毒化防过拟合信号与公开声明。Windows 无 POSIX 权限位
// （模式读回恒 0777）——在该平台降级为仅存在性检查；ACL 加固是 Windows 侧后续，
// 已披露进 gates-card 盲区。
func CheckPrivatePermissions(dir string) error {
	if runtime.GOOS == "windows" {
		if _, err := os.Stat(dir); err != nil {
			return fmt.Errorf("evalkit: 私有 golden 目录不存在（先 init）： %s", dir)
		}
		return nil
	}
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("evalkit: 私有 golden 目录不存在（先 init）： %s", dir)
		}
		return err
	}
	if info.Mode().Perm() != 0o700 {
		return fmt.Errorf("evalkit: 私有 golden 目录权限 %o 非 0700——拒绝使用（泄漏的私有集毒化防过拟合信号）", info.Mode().Perm())
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return err
	}
	for _, p := range matches {
		fi, err := os.Stat(p)
		if err != nil {
			return err
		}
		if fi.Mode().Perm() != 0o600 {
			return fmt.Errorf("evalkit: 私有用例 %s 权限 %o 非 0600——拒绝使用", filepath.Base(p), fi.Mode().Perm())
		}
	}
	return nil
}

// InitPrivateGolden creates the private directory with 0700.
//
// InitPrivateGolden 以 0700 建私有目录。
func InitPrivateGolden(evalDir string) (string, error) {
	dir := PrivateGoldenDir(evalDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// RotationRecord is one rotation's audit payload.
//
// RotationRecord 是一次轮换的审计载荷。
type RotationRecord struct {
	RotatedAt time.Time     `json:"rotated_at"`
	Retired   []string      `json:"retired"`
	Invalid   []InvalidCase `json:"invalid"`
	Kept      int           `json:"kept"`
}

// InvalidCase is a case that failed oracle revalidation.
//
// InvalidCase 是未通过 oracle 复验的用例。
type InvalidCase struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// RotatePrivateGolden runs one rotation: revalidate every surviving case
// (oracle = the case still parses, still declares valid expectations, and its
// probe argv is runnable — the cheap structural oracle; live replay belongs to
// the next golden run), retire invalid ones plus the oldest until at most
// maxCases remain, and write the rotation audit file. The caller decides how
// many fresh cases to add (authoring is human work).
//
// RotatePrivateGolden 执行一次轮换：对全部存续用例做 oracle 复验（结构级 oracle
// ——用例仍可解析、期望合法、探测命令可运行；live 重放属于下一次 golden run），
// 淘汰失效用例并按最老优先淘汰到 maxCases 以内，写轮换审计文件。补充多少新用例
// 由调用方决定（命题是人的工作）。
func RotatePrivateGolden(evalDir string, maxCases int, repoRoot string) (*RotationRecord, string, error) {
	dir := PrivateGoldenDir(evalDir)
	if err := CheckPrivatePermissions(dir); err != nil {
		return nil, "", err
	}
	cases, err := LoadGoldenDir(dir)
	if err != nil {
		return nil, "", fmt.Errorf("evalkit: 私有 golden 复验失败: %w", err)
	}
	rec := &RotationRecord{RotatedAt: time.Now().UTC()}
	kept := cases[:0]
	for _, c := range cases {
		if reason := oracleRevalidate(c); reason != "" {
			rec.Invalid = append(rec.Invalid, InvalidCase{ID: c.ID, Reason: reason})
			if err := retireCase(dir, c.ID); err != nil {
				return nil, "", err
			}
			rec.Retired = append(rec.Retired, c.ID)
			continue
		}
		kept = append(kept, c)
	}
	if excess := len(kept) - maxCases; excess > 0 {
		// 最老优先淘汰：按 ID 字典序近似（用例 ID 带序号约定）； retirement 与
		// 失效淘汰同一出口。
		sort.Slice(kept, func(i, j int) bool { return kept[i].ID < kept[j].ID })
		for _, c := range kept[:excess] {
			if err := retireCase(dir, c.ID); err != nil {
				return nil, "", err
			}
			rec.Retired = append(rec.Retired, c.ID)
		}
		kept = kept[excess:]
	}
	rec.Kept = len(kept)
	data, err := jsonMarshal(rec)
	if err != nil {
		return nil, "", err
	}
	auditPath := filepathJoin(dir, fmt.Sprintf("rotation-%s.json", rec.RotatedAt.UTC().Format("20060102-150405")))
	if err := atomicWriteFile(auditPath, data); err != nil {
		return nil, "", err
	}
	_ = checklog.Record(repoRoot, &checklog.Entry{
		Check:   checklog.CheckEvalGoldenRotate,
		Passed:  true,
		Checked: true,
		Detail: fmt.Sprintf(`golden rotate: kept %d retired %v invalid %d`,
			rec.Kept, rec.Retired, len(rec.Invalid)),
	})
	return rec, auditPath, nil
}

// oracleRevalidate is the structural oracle: the case must still load cleanly
// under current validation rules and its probe must not reference unknown
// placeholders. Semantic validity (does the gate still behave as labeled) is
// the next golden run's job — this only retires cases the current system can
// no longer even express.
//
// retireCase 把用例移入 retired/ 子目录——留在同目录改名会被 LoadGoldenDir 的
// *.yaml glob 捞回活跃集（对抗审查 I4：退役即复活，私有集治理失效）。
//
// retireCase moves a case into the retired/ subdirectory — renaming in place
// gets globbed back into the active set by LoadGoldenDir's *.yaml pattern.
func retireCase(dir, id string) error {
	retiredDir := filepath.Join(dir, "retired")
	if err := os.MkdirAll(retiredDir, 0o700); err != nil {
		return err
	}
	return os.Rename(filepath.Join(dir, id+".yaml"), filepath.Join(retiredDir, id+".yaml"))
}

// oracleRevalidate 是结构级 oracle：用例必须在当前校验规则下仍可干净加载，且
// 探测命令不引用未知占位符。语义有效性（门禁是否仍按标注行为）是下一次
// golden run 的事——这里只淘汰当前系统已经无法表达的用例。
func oracleRevalidate(c GoldenCase) string {
	if err := validateGoldenCase(&c); err != nil {
		return "结构校验失败: " + err.Error()
	}
	for _, a := range c.ProbeArgv {
		if strings.Contains(a, "{unknown}") {
			return "探测命令引用未知占位符"
		}
	}
	return ""
}
