package taskpipeline

// heldoutpool.go — oracle-pipeline L3：held-out 池（独立出题的项目级沉淀）。
// 既有 per-task `task start --heldout <file>` 是一次性保留集；池把它变成资产：
// 业务方/验收人把已知的业务坏天气（金额边界/并发/退款链）按集沉淀进
// <DataDir>/heldout-pool/，任务侧 `forge task heldout-pool --draw N --apply`
// 确定性抽取并入任务侧车——出题成本一次付、处处收。轮换语义沿用 W4：并入
// 【保留】侧车既有 RunCount/PinnedAt（RunCount 语义是 VerifyHeldout 实跑
// 计数，apply 不增不减），换锚事实落审计行；达阈值提醒重出（新集必须人出
// ——「测试的测试」是人工边界，forge 不自造测试）。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/util"
)

// HeldoutSet 是池里的一套保留题。
type HeldoutSet struct {
	Name     string // 池内唯一名（文件名去扩展）
	Tag      string // 分组标签（如 money/concurrency/refund；空=未分组）
	Criteria []AcceptanceCriterion
}

// heldoutPoolDir 返回池目录 <DataDir>/heldout-pool/。
func heldoutPoolDir(root string) string {
	return filepath.Join(dataHome(root), "heldout-pool")
}

// heldoutSetPath 是单套池集的落盘位置（每套一个 json）。
func heldoutSetPath(root, name string) string {
	safe := strings.ReplaceAll(name, "/", "__")
	return filepath.Join(heldoutPoolDir(root), safe+".json")
}

// AddHeldoutSet 把一套保留题落进池（同名校验：拒绝静默覆盖——换锚必须显式）。
func AddHeldoutSet(root string, s HeldoutSet) error {
	if s.Name == "" {
		return fmt.Errorf("池集名必填（--name；将作为 heldout-pool/<name>.json）")
	}
	if len(s.Criteria) == 0 {
		return fmt.Errorf("池集 %s 无条目——空集不入池", s.Name)
	}
	if _, err := os.Stat(heldoutSetPath(root, s.Name)); err == nil {
		return fmt.Errorf("池集 %s 已存在——换内容请先删 %s（拒绝静默换锚）", s.Name, heldoutSetPath(root, s.Name))
	}
	if err := os.MkdirAll(heldoutPoolDir(root), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	// 原子写（审查 P2-6）：撕裂写产出的损坏文件会被 ListHeldoutSets 跳过告警
	// ——池是项目资产，崩溃后集合无声消失不可接受。
	return util.AtomicWrite(heldoutSetPath(root, s.Name), append(body, '\n'), 0o644)
}

// ListHeldoutSets 列出池内全部集（名序确定性）。[tag] 前缀过滤，空 tag 全列。
func ListHeldoutSets(root, tag string) []HeldoutSet {
	entries, err := os.ReadDir(heldoutPoolDir(root))
	if err != nil {
		return nil
	}
	var sets []HeldoutSet
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(heldoutPoolDir(root), e.Name()))
		if err != nil {
			continue
		}
		var s HeldoutSet
		if err := json.Unmarshal(body, &s); err != nil || len(s.Criteria) == 0 {
			fmt.Fprintf(os.Stderr, "⚠ held-out 池集 %s 损坏或为空，已跳过（修复：删除后重新 --add）\n", e.Name())
			continue
		}
		if s.Name == "" {
			s.Name = strings.TrimSuffix(e.Name(), ".json") // 文件名是权威名（内部 Name 可被手改重排序）
		}
		if tag != "" && s.Tag != tag {
			continue
		}
		sets = append(sets, s)
	}
	sort.Slice(sets, func(i, j int) bool { return sets[i].Name < sets[j].Name })
	return sets
}

// DrawHeldoutSets 从池里确定性抽 n 套（名序取前 n——无随机；[tag] 过滤）。
// 纯只读抽取；轮换计数的载体是任务侧车的 RunCount（VerifyHeldout 实跑计数
// ，apply 不动它——审查 P1-1 修复后的口径）。
func DrawHeldoutSets(root, tag string, n int) ([]HeldoutSet, error) {
	sets := ListHeldoutSets(root, tag)
	if len(sets) == 0 {
		return nil, fmt.Errorf("held-out 池为空（tag=%q）——先 forge task heldout-pool --add <file> --name <名> 沉淀业务坏天气集", tag)
	}
	if n <= 0 || n > len(sets) {
		n = len(sets)
	}
	return sets[:n], nil
}

// CheckNameHeldoutPoolApply 是一次池并入的审计行（换锚有据——VerifyHeldout
// 的前后哈希对比把刻意 apply 与手改区分开靠的就是这行）。
const CheckNameHeldoutPoolApply checklog.CheckName = "heldout-pool-apply"

// ApplyHeldoutToTask 把抽出的集合并进任务的 held-out 侧车（与既有内容按 Run
// 去重合并）。保留侧车的既有使用计数与锚定时间（W4 记忆化暴露的对抗面：
// 合并清零计数 = 接近轮换阈值时跑一次 apply 即可推迟提醒——审查 P1-1），换锚
// 事实落 checklog 审计行（before/after 内容哈希）。返回合并后的总条数。
func ApplyHeldoutToTask(root, taskRef string, sets []HeldoutSet) (int, error) {
	var merged []AcceptanceCriterion
	for _, s := range sets {
		for _, c := range s.Criteria {
			c.Source = AcceptanceSourceManual // 池题出处是人——按最弱层披露（出题独立性由 heldout 侧车本身保障）
			merged = append(merged, c)
		}
	}
	sc, exists, err := loadHeldoutSidecar(root, taskRef)
	if err != nil {
		return 0, err
	}
	existing := []AcceptanceCriterion{}
	beforeHash := ""
	runCount := 0
	var pinnedAt time.Time
	if sc != nil && exists {
		existing = sc.Criteria
		beforeHash = sc.Hash
		runCount = sc.RunCount
		pinnedAt = sc.PinnedAt
	}
	all := existing
	seen := map[string]bool{}
	for _, c := range existing {
		seen[c.Run] = true
	}
	added := 0
	for _, c := range merged {
		if seen[c.Run] {
			continue
		}
		all = append(all, c)
		seen[c.Run] = true
		added++
	}
	afterHash := heldoutContentHash(all)
	if pinnedAt.IsZero() {
		pinnedAt = time.Now() // 首次登记侧车：锚定时间即现在
	}
	if err := saveHeldoutSidecar(root, taskRef, &heldoutSidecar{
		Criteria: all,
		RunCount: runCount,
		PinnedAt: pinnedAt,
		Hash:     afterHash,
	}); err != nil {
		return 0, err
	}
	recordAudit(root, &checklog.Entry{
		Check:   CheckNameHeldoutPoolApply,
		Passed:  true,
		Checked: true,
		TaskRef: taskRef,
		Level:   checklog.LevelPass,
		Detail: fmt.Sprintf("heldout-pool apply：%d 套并入（新增 %d 条，共 %d 条），内容哈希 %s → %s（使用计数 %d 保留）",
			len(sets), added, len(all), beforeHash, afterHash, runCount),
		Meta: map[string]string{
			"hash_before": beforeHash,
			"hash_after":  afterHash,
			"added":       fmt.Sprintf("%d", added),
		},
	})
	return len(all), nil
}
