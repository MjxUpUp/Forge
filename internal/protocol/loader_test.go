package protocol

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// TestSaveDataDirLoadRoundTrip verifies SaveDataDir persists a loadable protocol.yml: it writes the user-level DataDir copy (creating the dir via util.AtomicWrite) and Load reads back an equivalent Protocol.
//
// TestSaveDataDirLoadRoundTrip 验证 SaveDataDir 落盘的 protocol.yml 可加载：
// 写用户级 DataDir 副本（经 util.AtomicWrite 自建目录），Load 读回等价 Protocol。
func TestSaveDataDirLoadRoundTrip(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir := t.TempDir()
	want := DefaultProtocol()

	if err := SaveDataDir(dir, want); err != nil {
		t.Fatalf("SaveDataDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(forgedata.DataDirFor(dir), "protocol.yml")); err != nil {
		t.Fatalf("protocol.yml must exist in DataDir after SaveDataDir: %v", err)
	}
	// 零项目写入：SaveDataDir 不得创建项目级 .forge/。
	if _, err := os.Stat(filepath.Join(dir, ".forge")); !os.IsNotExist(err) {
		t.Fatalf("SaveDataDir must not create project-level .forge/ (zero-project-write)")
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load after SaveDataDir: %v", err)
	}
	if got.Version != want.Version {
		t.Errorf("Version = %q, want %q", got.Version, want.Version)
	}
	if len(got.Standards) != len(want.Standards) {
		t.Fatalf("Standards len = %d, want %d", len(got.Standards), len(want.Standards))
	}
	for i := range want.Standards {
		if got.Standards[i] != want.Standards[i] {
			t.Errorf("Standards[%d] = %+v, want %+v", i, got.Standards[i], want.Standards[i])
		}
	}
	if len(got.SessionRules) != len(want.SessionRules) {
		t.Fatalf("SessionRules len = %d, want %d", len(got.SessionRules), len(want.SessionRules))
	}
	for i := range want.SessionRules {
		if got.SessionRules[i] != want.SessionRules[i] {
			t.Errorf("SessionRules[%d] = %+v, want %+v", i, got.SessionRules[i], want.SessionRules[i])
		}
	}
}

// TestProjectLevelOverride pins the two-layer contract.
//
// TestProjectLevelOverride 钉死双层契约：<dir>/.forge/protocol.yml 存在时
// （团队共享覆盖，`forge init --project`），Load 读它而非 DataDir 副本，
// 更新覆盖层经 SaveProjectLevel（已删除的路由式 Save 曾隐式选层；现在调用方显式选）。
func TestProjectLevelOverride(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir := t.TempDir()

	// DataDir 副本用默认 version；项目级覆盖用自定义 version。
	if err := SaveDataDir(dir, DefaultProtocol()); err != nil {
		t.Fatalf("SaveDataDir copy: %v", err)
	}
	override := DefaultProtocol()
	override.Version = "team-override"
	if err := SaveProjectLevel(dir, override); err != nil {
		t.Fatalf("SaveProjectLevel: %v", err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Version != "team-override" {
		t.Errorf("Load 应读项目级覆盖 version=team-override，实得 %q", got.Version)
	}

	// 更新覆盖层经 SaveProjectLevel；DataDir 副本不动。
	override.Version = "team-override-v2"
	if err := SaveProjectLevel(dir, override); err != nil {
		t.Fatalf("SaveProjectLevel: %v", err)
	}
	got2, err := Load(dir)
	if err != nil {
		t.Fatalf("Load after SaveProjectLevel: %v", err)
	}
	if got2.Version != "team-override-v2" {
		t.Errorf("SaveProjectLevel 应写项目级覆盖，Load version 实得 %q", got2.Version)
	}
}

// TestSaveDataDirLoad_FailFastWhenDataDirUnresolvable pins the silent-mislocation fix.
//
// TestSaveDataDirLoad_FailFastWhenDataDirUnresolvable 钉死静默错位修复：DataDir
// 无法解析（FORGE_DATA_HOME 未设且 os.UserHomeDir 失败）时，SaveDataDir/Load
// 必须显式报错，而不是把裸 "protocol.yml" 写进进程 cwd。
func TestSaveDataDirLoad_FailFastWhenDataDirUnresolvable(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, ``)
	// 全平台破坏 os.UserHomeDir：unix 读 HOME；Windows 读 USERPROFILE，再读
	// HOMEDRIVE+HOMEPATH。
	t.Setenv(`HOME`, ``)
	t.Setenv(`USERPROFILE`, ``)
	t.Setenv(`HOMEDRIVE`, ``)
	t.Setenv(`HOMEPATH`, ``)

	dir := t.TempDir()
	if err := SaveDataDir(dir, DefaultProtocol()); err == nil {
		t.Fatal("SaveDataDir 在 DataDir 不可解析时必须报错（拒绝写相对路径），实得 nil")
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("Load 在 DataDir 不可解析时必须报错，实得 nil")
	}
	if _, err := os.Stat(`protocol.yml`); err == nil {
		t.Fatal("不得在进程 cwd 写入相对路径 protocol.yml")
	}
}

// TestEnsureDefault_CreatesWhenMissing: no protocol.yml anywhere → the DataDir copy is created from defaults and loads cleanly.
//
// TestEnsureDefault_CreatesWhenMissing：两处都无 protocol.yml → 从默认值创建
// DataDir 副本且可加载。
func TestEnsureDefault_CreatesWhenMissing(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir := t.TempDir()

	if err := EnsureDefault(dir); err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}
	if _, err := os.Stat(filepath.Join(forgedata.DataDirFor(dir), "protocol.yml")); err != nil {
		t.Fatalf("EnsureDefault 应创建 DataDir 副本: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load after EnsureDefault: %v", err)
	}
	if got.Scoring == nil {
		t.Error("EnsureDefault 写入的默认 protocol 应含 scoring 配置")
	}
	// 幂等：第二次跑保留文件（不重写、不报错）。
	if err := EnsureDefault(dir); err != nil {
		t.Fatalf("EnsureDefault 第二次: %v", err)
	}
}

// TestEnsureDefault_KeepsValidFile: an existing valid protocol.yml (user-edited) is never touched.
//
// TestEnsureDefault_KeepsValidFile：已存在的合法 protocol.yml（用户改过）绝不动。
func TestEnsureDefault_KeepsValidFile(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir := t.TempDir()

	custom := DefaultProtocol()
	custom.Version = "user-edited"
	if err := SaveDataDir(dir, custom); err != nil {
		t.Fatalf("SaveDataDir: %v", err)
	}
	if err := EnsureDefault(dir); err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Version != "user-edited" {
		t.Errorf("合法用户文件应保留: version = %q, want user-edited", got.Version)
	}
}

// TestEnsureDefault_CorruptBackedAside pins the corrupt-file fix: a protocol.yml the user broke (YAML parse error) is renamed aside to protocol.yml.corrupt-<ts> before defaults are written — never silently overwritten.
//
// TestEnsureDefault_CorruptBackedAside 钉死损坏文件修复：用户改坏的 protocol.yml
// （YAML 解析失败）先改名备份为 protocol.yml.corrupt-<ts> 再写默认值——绝不静默
// 覆盖。
func TestEnsureDefault_CorruptBackedAside(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv(`FORGE_DATA_HOME`, dataHome)
	dir := t.TempDir()

	dd := forgedata.DataDirFor(dir)
	if err := os.MkdirAll(dd, 0755); err != nil {
		t.Fatal(err)
	}
	bad := []byte("version: [unclosed\n  : : :")
	path := filepath.Join(dd, "protocol.yml")
	if err := os.WriteFile(path, bad, 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureDefault(dir); err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}

	// 损坏文件必须带原内容备份到一边。
	entries, err := os.ReadDir(dd)
	if err != nil {
		t.Fatal(err)
	}
	var backup string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), `protocol.yml.corrupt-`) {
			backup = e.Name()
			break
		}
	}
	if backup == "" {
		t.Fatal("损坏的 protocol.yml 应备份为 protocol.yml.corrupt-<ts>，未找到")
	}
	got, err := os.ReadFile(filepath.Join(dd, backup))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(bad) {
		t.Errorf("备份内容 = %q, want 原损坏内容 %q", got, bad)
	}

	// 新默认值就位且可加载。
	p, err := Load(dir)
	if err != nil {
		t.Fatalf("Load after EnsureDefault: %v", err)
	}
	if p.Scoring == nil {
		t.Error("重建的默认 protocol 应含 scoring 配置")
	}
}

// TestSave_AtomicNoTempLeftover verifies Save leaves no temp files behind in the DataDir — AtomicWrite renames its temp file over the target, so a stray .tmp-* file would indicate the write path is not atomic.
//
// TestSaveDataDir_AtomicNoTempLeftover 验证 SaveDataDir 不在 DataDir 留临时文件
// ——AtomicWrite 把临时文件 rename 覆盖目标，残留 .tmp-* 说明写路径不再原子。
func TestSaveDataDir_AtomicNoTempLeftover(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir := t.TempDir()
	if err := SaveDataDir(dir, DefaultProtocol()); err != nil {
		t.Fatalf("SaveDataDir: %v", err)
	}
	entries, err := os.ReadDir(forgedata.DataDirFor(dir))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "protocol.yml" {
			t.Errorf("unexpected file left in DataDir: %s", e.Name())
		}
	}
}

// TestLoad_CrossRepoImpactField pins the new cross_repo_impact knob: it round-trips through YAML, and an absent key decodes to "" (the advisory default — zero behavior change for existing protocol.yml files).
//
// TestLoad_CrossRepoImpactField 钉住新 cross_repo_impact 配置项：YAML 往返
// 无损；缺省键解码为 ""（advisory 默认——存量 protocol.yml 零行为变化）。
func TestLoad_CrossRepoImpactField(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir := t.TempDir()

	// 缺省 → ""（advisory 默认）。
	if err := SaveDataDir(dir, DefaultProtocol()); err != nil {
		t.Fatalf("SaveDataDir: %v", err)
	}
	p, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.CrossRepoImpact != `` {
		t.Errorf("缺省 cross_repo_impact 应为空（advisory 默认）, got %q", p.CrossRepoImpact)
	}

	// 显式 "required" 往返。
	custom := DefaultProtocol()
	custom.CrossRepoImpact = `required`
	if err := SaveDataDir(dir, custom); err != nil {
		t.Fatalf("SaveDataDir required: %v", err)
	}
	p, err = Load(dir)
	if err != nil {
		t.Fatalf("Load required: %v", err)
	}
	if p.CrossRepoImpact != `required` {
		t.Errorf("CrossRepoImpact = %q, want required", p.CrossRepoImpact)
	}
}

// captureStderr 把 os.Stderr 换成管道跑 fn 并返回写出的内容。validateWarn 的契约
// 只能经 stderr 观测——这些测试不得并行（全局 fd 替换）。
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	old := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = old }()
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	return string(data)
}

// writeRawDataDirProtocol 把原始 YAML 字节写成 DataDir 的 protocol.yml 副本
// （SaveDataDir marshal 类型化结构，产不出非法形态）。
func writeRawDataDirProtocol(t *testing.T, dir, content string) {
	t.Helper()
	path, err := DataDirPath(dir)
	if err != nil {
		t.Fatalf("DataDirPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write raw protocol: %v", err)
	}
}

// TestLoad_SeverityNormalizedAndWarned pins the severity post-validation.
//
// TestLoad_SeverityNormalizedAndWarned 钉死 severity 后校验：
//   - "ERROR"（大写）规范化为 "error" 并 stderr 告警提醒改源头——消费方精确比较
//     == "error"，规范化让该 standard 按本意生效而非落进未知处理；
//   - "catastrophic"（集合外）保留原值但响亮告警——render.go 的 label switch 会把
//     未知 severity 映射到 default 分支（当前是最严重的 ERROR/🔴），静默就是错方向。
func TestLoad_SeverityNormalizedAndWarned(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv(`FORGE_DATA_HOME`, dataHome)
	dir := t.TempDir()
	writeRawDataDirProtocol(t, dir, `version: "1.0"
standards:
  - id: upper-sev
    name: Upper
    description: d
    severity: ERROR
    enabled: true
  - id: bogus-sev
    name: Bogus
    description: d
    severity: catastrophic
    enabled: true
session_rules: []
`)

	var p *Protocol
	var loadErr error
	stderr := captureStderr(t, func() {
		p, loadErr = Load(dir)
	})
	if loadErr != nil {
		t.Fatalf("Load（语义告警不得让 Load 失败）: %v", loadErr)
	}
	if len(p.Standards) != 2 {
		t.Fatalf("Standards len = %d, want 2", len(p.Standards))
	}
	if p.Standards[0].Severity != "error" {
		t.Errorf("upper-sev severity 应规范化为 error, got %q（stderr=%q）", p.Standards[0].Severity, stderr)
	}
	if p.Standards[1].Severity != "catastrophic" {
		t.Errorf("bogus-sev severity 应保留原值 catastrophic, got %q", p.Standards[1].Severity)
	}
	if !strings.Contains(stderr, `已规范化为 "error"`) {
		t.Errorf("大写 severity 应告警「已规范化」, stderr=%q", stderr)
	}
	if !strings.Contains(stderr, `bogus-sev`) || !strings.Contains(stderr, `不在 info/warning/error 集合内`) {
		t.Errorf("集合外 severity 应点名告警, stderr=%q", stderr)
	}
}

// TestLoad_MissingEnabledWarnsOnce pins the enabled post-validation: YAML bools
// cannot distinguish "key absent" from "explicit false", so the raw bytes are
// shadow-re-read with *bool. Any number of standards lacking the key produces
// exactly ONE aggregated stderr hint (缺省告警一次) — a missing enabled decodes
// to false and the standard silently never applies.
//
// TestLoad_MissingEnabledWarnsOnce 钉死 enabled 后校验：YAML bool 无法区分
// 「键缺失」与「显式 false」，原始字节经 *bool 影子重读。任意多个漏写该键的
// standard 只触发一次聚合 stderr 提示（缺省告警一次）——漏写的 enabled 解码为
// false，standard 静默不生效。
func TestLoad_MissingEnabledWarnsOnce(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv(`FORGE_DATA_HOME`, dataHome)
	dir := t.TempDir()
	writeRawDataDirProtocol(t, dir, `version: "1.0"
standards:
  - id: no-enabled-a
    name: A
    description: d
    severity: warning
  - id: no-enabled-b
    name: B
    description: d
    severity: warning
  - id: explicit-false
    name: C
    description: d
    severity: warning
    enabled: false
session_rules: []
`)

	var p *Protocol
	var loadErr error
	stderr := captureStderr(t, func() {
		p, loadErr = Load(dir)
	})
	if loadErr != nil {
		t.Fatalf("Load: %v", loadErr)
	}
	if n := strings.Count(stderr, `漏写 enabled`); n != 1 {
		t.Errorf("2 个漏写 enabled 的 standard 应恰好触发一次聚合提示, got %d 次, stderr=%q", n, stderr)
	}
	if !strings.Contains(stderr, `2 个 standard 漏写 enabled`) {
		t.Errorf("提示应报出漏写数量, stderr=%q", stderr)
	}
	if !strings.Contains(stderr, `漏写将不生效`) {
		t.Errorf("提示应说明漏写将不生效, stderr=%q", stderr)
	}
	// 解码值：键缺失与显式 false 都是 false（解码无异——正是这条提示存在的理由）。
	for i, id := range []string{"no-enabled-a", "no-enabled-b", "explicit-false"} {
		if p.Standards[i].ID != id || p.Standards[i].Enabled {
			t.Errorf("Standards[%d] = %+v, want id=%q enabled=false", i, p.Standards[i], id)
		}
	}
}

// TestLoad_ValidProtocolSilent pins the no-noise half: a fully well-formed protocol (lowercase severities, explicit enabled) loads without a single stderr warning — the validation must not cry wolf on healthy files.
//
// TestLoad_ValidProtocolSilent 钉死无噪声的一半：完全合规的 protocol（小写
// severity、显式 enabled）加载时 stderr 零告警——校验不得对健康文件狼来了。
func TestLoad_ValidProtocolSilent(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir := t.TempDir()

	var loadErr error
	stderr := captureStderr(t, func() {
		_, loadErr = Load(dir)
	})
	// 完全没有 protocol.yml → Load 在校验前就报错；预期静默。
	if loadErr == nil {
		t.Fatal("缺 protocol.yml 应报错")
	}
	if stderr != "" {
		t.Errorf("校验前失败不应产生 stderr 告警, got %q", stderr)
	}

	writeRawDataDirProtocol(t, dir, `version: "1.0"
standards:
  - id: good-sev
    name: Good
    description: d
    severity: warning
    enabled: true
session_rules: []
`)
	stderr = captureStderr(t, func() {
		_, loadErr = Load(dir)
	})
	if loadErr != nil {
		t.Fatalf("Load 合规文件: %v", loadErr)
	}
	if stderr != "" {
		t.Errorf("合规 protocol 不得产生任何 stderr 告警, got %q", stderr)
	}
}
