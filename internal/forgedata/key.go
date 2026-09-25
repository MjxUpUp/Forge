// Package forgedata 提供 forge 项目数据的统一路径 / key 推导。
//
// 设计目标：把当前散落的 `filepath.Join(dir, ".forge", ...)` 集中到一个真相源，
// 让项目数据 home（tasks / gates / ...）从项目级 `.forge/` 平滑迁移
// 到用户级 `~/.forge/projects/<hash12>/`。ConfigDir（protocol/CLAUDE.md
// /hooks）仍留项目级（git tracked，user-editable）。
//
// 详见 docs/plans/refactor-data-home.md。
//
// 中文字符串用 raw string（反引号）规避 Windows 输入引号腐蚀。
package forgedata

import (
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// 预定义错误
var (
	// ErrNotInGitRepo reports that cwd is not inside any git repository.
	//
	// ErrNotInGitRepo: cwd 不在任何 git repo 内
	ErrNotInGitRepo = errors.New(`forgedata: cwd is not in a git repository`)

	// ErrInvalidGitFile reports that the `.git` file is corrupted (empty / missing 'gitdir:' / NUL / looped / beyond fs root).
	//
	// ErrInvalidGitFile: `.git` file 损坏（empty / missing 'gitdir:' / NUL / looped / beyond fs root）
	ErrInvalidGitFile = errors.New(`forgedata: invalid .git file (worktree/submodule)`)

	// ErrInvalidProjectID: `.forge-project-id` present but malformed (must be fpid_ + 32 lowercase hex).
	//
	// ErrInvalidProjectID：`.forge-project-id` 存在但畸形（须为 fpid_ + 32 位小写
	// hex）。Key() 将其视同无 ID 回落路径 hash（fail-open）——错误经 ReadProjectID
	// 暴露给报告层（doctor / adopt）供严格校验。
	ErrInvalidProjectID = errors.New(`forgedata: invalid .forge-project-id (want fpid_<32 lowercase hex>)`)
)

// ProjectIDFileName 是 repo-born 身份文件（project-sync 设计 §A）：committed 在主
// worktree 根，随仓库旅行（clone/fork/其他机器），使推导 key 与机器无关。刻意不放
// `.forge/` 下——项目级 `.forge/` 目录的存在会把 ConfigDir 翻进 team/legacy 模式
// （见 paths.go），身份文件绝不能触发该副作用。
const ProjectIDFileName = `.forge-project-id`

// idPrefix / idHexLen 钉死 ID 格式：`fpid_` + 32 位小写 hex（生成时 crypto/rand 的
// 16 字节）。校验是紧 allowlist——ID 是攻击者可控输入（任意 clone 的仓库都可能带
// 一个），宽松格式会让奇异内容流进身份 hash。
const (
	idPrefix = `fpid_`
	idHexLen = 32
)

// ReadProjectID reads and strictly validates the project ID file under repoRoot.
//
// ReadProjectID 读取并严格校验 repoRoot 下的项目 ID 文件。返回完整 token
// （fpid_<32hex>）。文件缺失、不可读、内容畸形都返回错误（缺失为 os 错误，畸形为
// ErrInvalidProjectID）——只需要回落路径的调用方把任何错误都当「无 ID」。
func ReadProjectID(repoRoot string) (string, error) {
	data, err := os.ReadFile(filepath.Join(repoRoot, ProjectIDFileName))
	if err != nil {
		return ``, err
	}
	id := strings.TrimSpace(string(data))
	if len(id) != len(idPrefix)+idHexLen || !strings.HasPrefix(id, idPrefix) {
		return ``, fmt.Errorf(`%w: %q`, ErrInvalidProjectID, truncateRunesForErr(id))
	}
	for _, c := range id[len(idPrefix):] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return ``, fmt.Errorf(`%w: non-hex char %q`, ErrInvalidProjectID, truncateRunesForErr(id))
		}
	}
	return id, nil
}

// truncateRunesForErr 限制错误信息长度（文件可能是任意大的垃圾内容）；24 rune
// 足以定位问题。
func truncateRunesForErr(s string) string {
	r := []rune(s)
	if len(r) <= 24 {
		return s
	}
	return string(r[:24]) + `...`
}

// IDKey derives the identity key from a validated project ID: hash12 of FNV-64a("fpid:" + id).
//
// IDKey 从已校验的项目 ID 推导身份 key：FNV-64a("fpid:"+id) 的 hash12。"fpid:" 域
// 前缀使 hash 输入与任何文件系统路径不相交（路径 key hash 的是绝对路径串），
// 按构造 ID 永不与遗留路径身份相撞。纯内容 hash → 任意 OS/文件系统同 key
// （输入无路径、无大小写折叠）。
func IDKey(id string) string {
	h := fnv.New64a()
	h.Write([]byte(idPrefix + `:` + id))
	return hash12(h.Sum64())
}

// keyFromCommonDir 是 ID 前的身份推导：解析后 `.git` common dir 路径的 hash12。
// 留作具名函数是因为 adopt 需要同时拿到旧 key（路径体系）与新 key（ID 体系）
// 在两者之间迁移数据。
func keyFromCommonDir(resolvedGitDir string) string {
	h := fnv.New64a()
	h.Write([]byte(filepath.Clean(resolvedGitDir)))
	return hash12(h.Sum64())
}

// ResolvedGitDir returns cwd's fully-resolved `.git` common dir — the same resolution Key() performs (worktree/submodule .git file → main repo .git, EvalSymlinks, CanonicalCase).
//
// ResolvedGitDir 返回 cwd 完全解析后的 `.git` common dir——与 Key() 同一解析
// （worktree/submodule 的 .git file → 主 repo .git、EvalSymlinks、CanonicalCase）。
// 导出给 `forge project adopt`：它需要主 worktree 根（该目录的父目录）来写 ID
// 文件，与 cwd 落在哪个 worktree 无关。错误契约与 Key 相同
// （ErrNotInGitRepo / ErrInvalidGitFile）。
func ResolvedGitDir(cwd string) (string, error) {
	return resolveGitDir(cwd)
}

// KeyFromPath is the path-regime derivation: the key this repo had BEFORE any project ID existed (and would still have if the ID file were removed). adopt uses it as the migration source key.
//
// KeyFromPath 是路径体系推导：本项目在任何 project ID 存在之前的 key（也是 ID
// 文件被删后将回到的 key）。adopt 用它作迁移源 key。错误契约与 Key 相同。
func KeyFromPath(cwd string) (string, error) {
	gitDir, err := resolveGitDir(cwd)
	if err != nil {
		return ``, err
	}
	return keyFromCommonDir(gitDir), nil
}

// resolveGitDir 是 Key/ResolvedGitDir/KeyFromPath 共享的解析主干：
// FindGitRoot → .git 目录或文件解析 → EvalSymlinks → CanonicalCase。
func resolveGitDir(cwd string) (string, error) {
	gitRoot := FindGitRoot(cwd)
	if gitRoot == `` {
		return ``, ErrNotInGitRepo
	}
	absGit := filepath.Clean(filepath.Join(gitRoot, ".git"))

	info, err := os.Stat(absGit)
	if err != nil {
		return ``, ErrNotInGitRepo // .git 不存在（与"非 git"等效）
	}

	var resolvedGitDir string
	if info.IsDir() {
		// 主 worktree —— .git 自身已稳定
		resolvedGitDir = absGit
	} else {
		// .git 是 file（worktree / submodule）
		resolvedGitDir, err = resolveGitFile(absGit, gitRoot)
		if err != nil {
			return ``, err
		}
	}
	if eval, evalErr := filepath.EvalSymlinks(resolvedGitDir); evalErr == nil {
		resolvedGitDir = eval
	}
	return CanonicalCase(resolvedGitDir), nil
}

// FindGitRoot walks up from dir to the nearest ancestor containing `.git` (dir or file).
//
// cli 侧（cli/hook.go 的 suggestTagFor、cli/suggest.go）已复用本函数——单一真相源在此，
// cli 不再保留私有复制品。
//
// FindGitRoot walks up from dir 到最近的含 `.git`（dir 或 file）祖先。找不到返 ""。
// 不依赖 forge 项目存在——只是 git repo 探测。
//
// cli 侧已复用本函数，不再保留私有复制品。
func FindGitRoot(dir string) string {
	d := filepath.Clean(dir)
	for {
		git := filepath.Join(d, ".git")
		if _, err := os.Stat(git); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "" // Unix "/" 或 Windows 盘根
		}
		d = parent
	}
}

// Key derives hash12: the first 12 hex chars of FNV-64a of the `.git` common dir of the git repo containing cwd.
//
// Key 推导 hash12：cwd 所在 git repo 的 `.git` common dir 的 FNV-64a hex 前 12 字符。
// 同仓库多 worktree（agent / detached / submodule）共享同一 key。
//
// 算法：
//  1. findGitRoot(cwd) —  失败 → ErrNotInGitRepo
//  2. .git 是 dir（主 worktree）→ 直接用
//     .git 是 file（worktree/submodule）→ 读 gitdir: 行 → 沿 parent 链找 .git 祖先
//  3. EvalSymlinks 后置归一（symlinked repo 同 key）
//  4. repo-born ID 优先：主 worktree 根（解析后 common .git 目录的父目录）存在合法
//     `.forge-project-id` 时覆盖路径 hash → IDKey。缺失/畸形 ID 静默回落路径 hash
//     （fail-open：存量项目身份不变；坏文件绝不 brick hook 热路径）。
//  5. fnv-64a(... )[:12]
//
// ErrInvalidGitFile 在 .git file 损坏时返（empty / 缺 'gitdir:' / 含 NUL / 循环 / 越过 fs 根）。
func Key(cwd string) (string, error) {
	resolvedGitDir, err := resolveGitDir(cwd)
	if err != nil {
		return ``, err
	}

	// CanonicalCase 回写磁盘真实拼写：大小写不敏感 APFS 上同一 repo 的任意拼写
	// 都能 stat 成功，而 EvalSymlinks 不归一大小写——没有这步，拼写变体的 cwd
	// 会给同一项目衍生第二个身份（Forge/forge key 分裂）。大小写敏感文件系统上
	// 恒等（精确匹配优先规则）。既有 canonical-case 登记 key 不变：canonical
	// 形态就是磁盘拼写，只有变体拼写向它收敛——存量数据零迁移。
	resolvedGitDir = CanonicalCase(resolvedGitDir)

	// repo-born ID 优先（project-sync §A）：从主 worktree 根读 ID——解析后 common
	// .git 目录的父目录（linked worktree 解析到主 repo 的 .git，故所有 worktree
	// 看到同一个 ID 文件，维持一 repo 一 key 契约；主 worktree 未 commit 的 ID
	// 也已生效）。任何读取/校验失败 = 无 ID → 路径 hash 回落。额外成本是 hook
	// 热路径上一次 stat+read（µs 级）；不做 memoize——缓存会在 adopt 写入/删除
	// 文件的瞬间失真。
	idRoot := filepath.Dir(resolvedGitDir)
	if id, ierr := ReadProjectID(idRoot); ierr == nil {
		return IDKey(id), nil
	}

	return keyFromCommonDir(resolvedGitDir), nil
}

// hash12 返回 sum 的 hex 前 12 字符（零填充）。strconv.FormatUint 不零填充，
// hex 不足 12 位的 sum（概率约 16^-12）会在 s[:12] 上 slice 越界 panic；%012x 保证 >= 12 位。
func hash12(sum uint64) string {
	return fmt.Sprintf("%012x", sum)[:12]
}

// PathKey derives a stable key for a NON-git project root: "p" + hash12 of the FNV-64a of the normalized absolute path.
//
// PathKey 为非 git 项目根推导稳定 key：归一化绝对路径的 FNV-64a hash12 加 "p" 前缀。
// git 项目用 Key（git common dir hash）；非 git 项目没有 .git 可 hash，
// 绝对路径本身就是身份。"p" 前缀让两个 key 命名空间互不相撞
// （纯 hex 的 git key 永远不会与 path key 冲突）。
//
// Windows 路径 hash 前转小写（大小写不敏感文件系统——C:\Proj 与 c:\proj 必须
// 共享同一 DataDir）。可能时解析 symlink，让 symlink 根得到相同 key。
func PathKey(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	abs = filepath.Clean(abs)
	if eval, err := filepath.EvalSymlinks(abs); err == nil {
		abs = eval
	}
	if runtime.GOOS == "windows" {
		abs = strings.ToLower(abs)
	} else {
		// 与 Key() 相同的 canonical-case 收敛：大小写不敏感文件系统上同一目录
		// 的拼写变体必须共享同一 PathKey。
		abs = CanonicalCase(abs)
	}
	h := fnv.New64a()
	h.Write([]byte(abs))
	return "p" + hash12(h.Sum64())
}

// resolveGitFile 解析 worktree/submodule `.git` file 的 gitdir: 行，沿 parent 链找 `.git` 祖先。
// 容错：empty / 缺 prefix / NUL / 循环 / fs 根外 全部返 ErrInvalidGitFile。
func resolveGitFile(absGitFile, gitRoot string) (string, error) {
	data, err := os.ReadFile(absGitFile)
	if err != nil {
		return "", fmt.Errorf(`%w: %s`, ErrInvalidGitFile, err.Error())
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return "", fmt.Errorf(`%w: empty .git file`, ErrInvalidGitFile)
	}
	// 第一行 "gitdir: /path/..." 或 "gitdir: ../relative"
	line := strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
	if !strings.HasPrefix(line, "gitdir: ") {
		return "", fmt.Errorf(`%w: missing 'gitdir:' prefix`, ErrInvalidGitFile)
	}
	relRaw := strings.TrimSpace(strings.TrimPrefix(line, "gitdir: "))
	if relRaw == `` || strings.ContainsRune(relRaw, 0) {
		return "", fmt.Errorf(`%w: invalid gitdir value`, ErrInvalidGitFile)
	}

	var target string
	if filepath.IsAbs(relRaw) {
		target = relRaw
	} else {
		target = filepath.Join(gitRoot, relRaw)
	}
	target = filepath.Clean(target)

	// 沿 parent 链找含名 `.git` 的祖先
	const safetyMax = 64
	candidate := target
	for safety := safetyMax; filepath.Base(candidate) != ".git"; safety-- {
		// 终止条件：候选已退化到空/点/根
		if candidate == `` || candidate == "." || candidate == string(filepath.Separator) || safety <= 0 {
			return "", fmt.Errorf(`%w: gitdir did not resolve to a .git ancestor: %s`, ErrInvalidGitFile, target)
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return "", fmt.Errorf(`%w: gitdir resolved beyond filesystem root: %s`, ErrInvalidGitFile, target)
		}
		candidate = parent
	}
	return filepath.Clean(candidate), nil
}

// RootDir returns the path to `~/.forge/projects/<key>/`.
//
// RootDir 返回 `~/.forge/projects/<key>/` 路径。
//
// `FORGE_DATA_HOME` env 覆盖全局 home（test 隔离 + 高级用户覆盖）。
//
// 空 key 返 ""（caller 决定 fallback，不强默认）。
// ValidKeyFormat reports whether key has one of the two legitimate key shapes —
// hash12 (`[0-9a-f]{12}`, git common-dir hash or IDKey) or PathKey
// (`p` + hash12). Callers that join a key into a filesystem path (RootDir
// consumers addressing a project by key: workspace status, cross-repo
// DependsOn resolution) must check this first — the key can be attacker
// controlled (a DependsOn entry travels inside tasks/*.json bundles), and an
// unchecked `..`/`/` in it would steer the read-only scan outside the data
// home. Tight-allowlist, same tradition as the `.forge-project-id` format
// check: anything not matching is rejected, never sanitized into validity.
//
// ValidKeyFormat 报告 key 是否为两种合法形态之一——hash12（`[0-9a-f]{12}`，
// git common dir hash 或 IDKey）或 PathKey（`p` + hash12）。凡把 key 拼进
// 文件系统路径的调用方（按 key 寻址项目的 RootDir 消费方：workspace status、
// 跨仓 DependsOn 解析）必须先过本检查——key 可以是攻击者可控输入（DependsOn
// 条目随 tasks/*.json bundle 旅行），未校验的 `..`/`/` 会把只读扫描引到数据
// home 之外。收紧 allowlist，与 `.forge-project-id` 格式校验同一传统：不匹配
// 即拒绝，绝不清洗成合法。
func ValidKeyFormat(key string) bool {
	hex := key
	if strings.HasPrefix(key, `p`) {
		hex = key[1:]
	}
	if len(hex) != 12 {
		return false
	}
	for _, c := range hex {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func RootDir(key string) string {
	if key == "" {
		return ""
	}
	home, err := GlobalHome()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "projects", key)
}

// GlobalHome returns the global home.
//
// GlobalHome 返全局 home。FORGE_DATA_HOME 优先（覆盖 home root），否则回落 UserHomeDir。
//
// 设计：FORGE_DATA_HOME 既管控全局 home（如 ~/.forge），所有 sub-store
// （registry/projects.json、init-suggest marker、knowledge、projects/<key>/）都用它。
// 导出供 registry/suggest 等全局 store 复用同一真相源（refactor-data-home commit E：
// 统一 FORGE_DATA_HOME，废弃 registry 旧的 FORGE_HOME env）。
func GlobalHome() (string, error) {
	if h := os.Getenv("FORGE_DATA_HOME"); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".forge"), nil
}
