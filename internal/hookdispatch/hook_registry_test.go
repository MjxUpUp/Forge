package hookdispatch

// hook_registry_test.go — 宿主行为注册表的完整性守卫（hostcap-behavior-registry.md）：
// 数据（hostcap 注册表）与行为（hookdispatch 的 stdinNormalizers / outputEmitters）
// 分离的执法面。新增宿主漏配任一行为列，第一道守卫测试即红——不再依赖包文档
// 注释里"键必须与 StdinDialect 一致"的自觉。

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hostcap"
)

// repoRoot 定位仓根。不用 os.Getwd：包内既有测试会 os.Chdir 且不复位，全包
// 串行跑时 cwd 不可信——改从本测试文件的编译期路径（runtime.Caller）向上找。
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller 失败——无法定位仓根")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("向上未找到 go.mod——请在仓内运行")
		}
		dir = parent
	}
}

// TestHostcapDialectRegistry 钉住 stdinNormalizers 键集合 ↔ hostcap StdinDialect
// 声明的双向一致：宿主声明了方言就必须有同名 normalizer（漏配 = 拦截类 hook
// 静默 fail-open 的归因断点）；normalizer 的死键同样是漂移信号。
func TestHostcapDialectRegistry(t *testing.T) {
	declared := map[string]string{} // dialect → 宿主名
	for _, h := range hostcap.Hosts {
		if h.StdinDialect == "" {
			continue
		}
		if prev, dup := declared[h.StdinDialect]; dup {
			t.Errorf("StdinDialect %q 被宿主 %q 与 %q 重复声明", h.StdinDialect, prev, h.Name)
		}
		declared[h.StdinDialect] = h.Name
		if _, ok := stdinNormalizers[h.StdinDialect]; !ok {
			t.Errorf("宿主 %q 声明 StdinDialect %q，但 stdinNormalizers 无同名 normalizer（新增宿主漏配行为列）", h.Name, h.StdinDialect)
		}
	}
	for key := range stdinNormalizers {
		if _, ok := declared[key]; !ok {
			t.Errorf("stdinNormalizers 死键 %q：没有任何 hostcap 宿主声明该 StdinDialect", key)
		}
	}
}

// claudeDefaultEmitters 显式声明走 emitClaudeOutput 默认（无 outputEmitters 键）
// 的宿主及理由——名单之外的新宿主必须写专属 emitter，或显式入列并给理由。
// 语义与 EmitAgentOutput 的 map-miss 默认一致：这里把"隐式默认"升为"显式声明"。
var claudeDefaultEmitters = map[string]string{
	"claude-code": "Claude 本体——emitClaudeOutput 即其专属 emitter（协议定义方）",
	"opencode":    "code-based：TS 扩展 spawn forge 前直接构造 Claude-shape stdin，stdout 也是 Claude 形",
	"codebuddy":   "Claude-JSON 兼容宿主（不带 --agent flag），走默认",
	"reasonix":    "native manifest 接线；stdout 解析 Claude 形 dialect（camelCase 只在 stdin，由 normalizer 归一）",
	"dsh":         "forge-dsh 包装层在进程内读 forge stdout 的 Claude dialect 并折进 DSH decision",
	"zcode":       "协议层刻意 Claude 兼容（hookSpecificOutput.additionalContext + exit-2 阻断捷径）",
}

// TestHostcapEmitterRegistry 钉住 outputEmitters 键集合 ↔ hostcap 宿主清单的
// 双向一致：每个宿主要么有专属 emitter，要么在 claudeDefaultEmitters 显式声明
// 走默认（带理由）；emitter 键必须是已知宿主（死键检查）。
func TestHostcapEmitterRegistry(t *testing.T) {
	hosts := map[string]bool{}
	for _, h := range hostcap.Hosts {
		hosts[h.Name] = true
		if _, ok := outputEmitters[h.Name]; !ok {
			if _, declared := claudeDefaultEmitters[h.Name]; !declared {
				t.Errorf("宿主 %q 无 outputEmitters 条目也未声明走 Claude 默认（claudeDefaultEmitters）——新增宿主的输出协议漏配", h.Name)
			}
		}
	}
	for key := range outputEmitters {
		if !hosts[key] {
			t.Errorf("outputEmitters 死键 %q：hostcap 注册表无此宿主", key)
		}
	}
	for key := range claudeDefaultEmitters {
		if !hosts[key] {
			t.Errorf("claudeDefaultEmitters 死键 %q：hostcap 注册表无此宿主（宿主已删除/改名，名单未同步）", key)
		}
	}
}

// noHostLiteralAllowlist 是字面量棘轮的显式豁免清单（文件 → 预期命中数 + 理由）。
// 命中数超出登记（或出现未登记文件）即红——新增宿主字面量门控必须先在此登记
// 理由并通过评审；这正是棘轮的执法方式：让"加一个 if agent == ..."从顺手变成
// 显式决定。豁免清零时（重构消灭了字面量）测试会反向提醒移除豁免。
var noHostLiteralAllowlist = map[string]struct {
	want   int
	reason string
}{
	"internal/hookdispatch/hook_emitters.go": {1, "advisoryPromotionDisabled：FORGE_KIMI_ADVISORY 的 env 名即已发布契约，soft 语义刻意仅限 kimi、不得静默波及其他提升宿主（见该函数注释）"},
	"internal/hookdispatch/hook_kimi_advisory.go": {2, "kimi 专精 advisory 队列：EmitAdvisoryRouted 的 kimi 分支（UserPromptSubmit 攒发/其余事件入队静默）与 AdvisoryEmissionChannel 的通道标注——队列机制本体就是 per-host 专精件"},
}

// TestNoHostLiteralGates 字面量棘轮：internal/ 生产代码（非测试、非纯注释行）
// 禁止出现按宿主名的 ==/!= 字面量门控——新宿主差异必须走 hostcap 注册表，
// 而非散落 if。宿主名词表直接来自 hostcap.Hosts（注册表即词表）。
// 已知盲区（诚实边界，2026-09-16 审查）：switch case "kimi":、跨行比较、
// strings.Contains/EqualFold(agent, ...)、map 键取用 m["kimi"] 与 cmd/ 目录
// 不在本正则扫描面——审查时对高频宿主分支点补 grep。
func TestNoHostLiteralGates(t *testing.T) {
	root := repoRoot(t)
	names := make([]string, 0, len(hostcap.Hosts))
	for _, h := range hostcap.Hosts {
		names = append(names, regexp.QuoteMeta(h.Name))
	}
	sort.Strings(names)
	alts := strings.Join(names, "|")
	// 两个方向都查：agent == "kimi" / "kimi" == agent，== 与 != 同罪。
	// (?:...) 分组必须有——否则 alternation 会把引号锚只留在首尾分支上，
	// 裸名（opencode/cline/...）会命中 .opencode/.clinerules 这类路径子串。
	re := regexp.MustCompile(`(==|!=)\s*"(?:` + alts + `")|"(?:` + alts + `")\s*(==|!=)`)

	type hit struct {
		line int
		text string
	}
	found := map[string][]hit{}
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		for i, line := range strings.Split(string(body), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue // 注释里的示例/史料不算门控
			}
			if re.MatchString(line) {
				found[rel] = append(found[rel], hit{line: i + 1, text: trimmed})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for file, hits := range found {
		allow, ok := noHostLiteralAllowlist[file]
		if !ok {
			t.Errorf("%s 出现未登记的宿主字面量门控（应改走 hostcap 注册表，或在此登记理由）:", file)
			for _, h := range hits {
				t.Errorf("  L%d: %s", h.line, h.text)
			}
			continue
		}
		if len(hits) > allow.want {
			t.Errorf("%s 的字面量门控数 %d 超过豁免登记的 %d 处（豁免理由：%s）——新增位点请改走 hostcap 注册表或更新豁免登记", file, len(hits), allow.want, allow.reason)
		}
	}
	for file, allow := range noHostLiteralAllowlist {
		if _, ok := found[file]; !ok {
			t.Errorf("豁免清单里的 %q 已无字面量门控命中——请移除豁免并删掉理由（理由：%s）", file, allow.reason)
		}
	}
}
