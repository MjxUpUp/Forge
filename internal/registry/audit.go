package registry

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// audit.go — read-only consistency audit over the registry + project data dirs
// (project-sync). Four finding kinds, all advisory (surfaced by
// `forge registry audit`):
//
//	key-drift      entry's stored key ≠ currently-derived key AND the old-key
//	               DataDir holds data → the project adopted an ID (or moved) and
//	               needs `forge project adopt` to migrate; otherwise its history
//	               silently sits under an unreachable key.
//	orphan-datadir a projects/<key>/ dir with real payload but no registry entry
//	               carrying that key (backup shells excluded).
//	id-collision   two DIFFERENT registered paths derive the SAME key — the
//	               copy-paste-shared-ID / same-repo-two-clones detector.
//	invalid-id     a repo has a .forge-project-id that fails strict validation —
//	               Key() silently fell back to the path hash; this is the only
//              	 surface where the fail-open fallback becomes visible.
//
// audit.go —— 注册表 + 项目数据目录的只读一致性审计（project-sync）。四类发现，
// 全部 advisory（经 `forge registry audit` 呈现），语义见上。

// Finding is one audit result.
//
// Finding 是一条审计发现。
type Finding struct {
	Kind   string `json:"kind"`
	Path   string `json:"path,omitempty"`
	Key    string `json:"key,omitempty"`
	Detail string `json:"detail"`
}

// AuditKind constants.
//
// AuditKind 常量。
const (
	AuditKeyDrift      = `key-drift`
	AuditOrphanDataDir = `orphan-datadir`
	AuditIDCollision   = `id-collision`
	AuditInvalidID     = `invalid-id`
)

// Audit walks the registry and the projects/ home, returning every finding.
// Read-only; a missing registry file yields the orphan scan only (same contract
// as List — empty registry is not an error).
//
// Audit 遍历注册表与 projects/ home，返回全部发现。只读；注册表文件缺失时仅做
// orphan 扫描（与 List 同契约——空注册表不是错误）。
func Audit() []Finding {
	var findings []Finding
	f, _ := readFile()
	entries := f.Projects

	// key-drift / invalid-id / id-collision: per-entry pass.
	//
	// key-drift / invalid-id / id-collision：逐条目扫描。
	derived := make(map[string]string, len(entries)) // key → first path (collision probe)
	keysHeld := make(map[string]bool, len(entries))  // keys carried by any entry
	for _, e := range entries {
		keysHeld[e.Key] = true

		if _, err := os.Stat(e.Path); err != nil {
			continue // 死路径归 prune 管，不在 audit 重复报
		}
		der, err := forgedata.Key(e.Path)
		if err != nil {
			continue // 非 git / .git 损坏：推导失败不作 finding（prune/doctor 域）
		}
		if der != e.Key && e.Key != `` && dataDirHasPayload(forgedata.RootDir(e.Key)) {
			findings = append(findings, Finding{
				Kind: AuditKeyDrift,
				Path: e.Path,
				Key:  e.Key,
				Detail: fmt.Sprintf(`注册表 key=%s 但当前派生 key=%s（旧 key 数据目录仍有数据）——跑 forge project adopt 迁移对齐，否则历史数据在新身份下不可见`,
					e.Key, der),
			})
		}
		// invalid-id：文件存在但校验失败（Key() 的 fail-open 回落在此暴露）。
		if gitDir, gerr := forgedata.ResolvedGitDir(e.Path); gerr == nil {
			mainRoot := filepath.Dir(gitDir)
			if _, rerr := os.Stat(filepath.Join(mainRoot, forgedata.ProjectIDFileName)); rerr == nil {
				if _, verr := forgedata.ReadProjectID(mainRoot); verr != nil {
					findings = append(findings, Finding{
						Kind: AuditInvalidID,
						Path: e.Path,
						Detail: fmt.Sprintf(`%s 存在但不合法（要求 fpid_<32 位小写 hex）——身份已回落路径 hash；修正或删除该文件后重跑 forge project adopt`,
							forgedata.ProjectIDFileName),
					})
				}
			}
		}
		if first, dup := derived[der]; dup && first != e.Path {
			findings = append(findings, Finding{
				Kind: AuditIDCollision,
				Key:  der,
				Path: e.Path,
				Detail: fmt.Sprintf(`两个不同路径派生同一 key=%s（%s 与 %s）——同仓库两 clone 属预期；若两项目本不相同，是复制粘贴共享 .forge-project-id，用 forge project adopt --regenerate 换新`,
					der, first, e.Path),
			})
		} else {
			derived[der] = e.Path
		}
	}

	// orphan-datadir：projects/ 下有实质载荷但无任何注册条目携带该 key。
	home, err := forgedata.GlobalHome()
	if err != nil {
		return findings
	}
	projectsDir := filepath.Join(home, `projects`)
	bins, err := os.ReadDir(projectsDir)
	if err != nil {
		return findings
	}
	for _, b := range bins {
		if !b.IsDir() || strings.HasPrefix(b.Name(), `.`) {
			continue
		}
		if keysHeld[b.Name()] {
			continue
		}
		dir := filepath.Join(projectsDir, b.Name())
		if !dataDirHasPayload(dir) {
			continue
		}
		findings = append(findings, Finding{
			Kind:   AuditOrphanDataDir,
			Key:    b.Name(),
			Path:   dir,
			Detail: `数据目录有实质载荷但注册表无对应条目——可能从未 init、或身份已翻转到新 key（旧目录待 forge project adopt 迁移或确认后清理）`,
		})
	}
	return findings
}

// dataDirHasPayload reports whether dir contains a regular file at any depth,
// EXCLUDING .rekey-backup-* shells (their payload is a backup copy, not live
// data — an emptied-from dir riding a backup must not read as live).
//
// dataDirHasPayload 报告 dir 任意深度是否含普通文件，排除 .rekey-backup-* 壳
// （其载荷是备份副本不是活数据——只剩备份壳的目录不得读作有活数据）。
func dataDirHasPayload(dir string) bool {
	if dir == `` {
		return false
	}
	found := false
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() != filepath.Base(dir) && strings.HasPrefix(d.Name(), `.rekey-backup-`) {
			return filepath.SkipDir
		}
		if !found && d.Type().IsRegular() {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}
